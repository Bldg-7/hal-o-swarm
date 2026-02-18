package agent

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// Agent represents a local hal-agent instance managing projects and opencode processes.
type Agent struct {
	cfg          *config.AgentConfig
	registry     *ProjectRegistry
	envCheckers  map[string]*EnvChecker
	lastEnvCheck map[string]*CheckResult
	mu           sync.RWMutex
	running      bool

	logger             *zap.Logger
	wsClient           *WSClient
	opencodeAdapter    OpencodeAdapter
	credApplier        *CredentialApplier
	authReporter       *AuthReporter
	oauthExecutor      *OAuthTriggerExecutor
	authReporterCancel context.CancelFunc
}

// NewAgent creates a new Agent instance with the given config.
func NewAgent(cfg *config.AgentConfig, logger *zap.Logger) (*Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Load and validate projects
	registry, err := NewProjectRegistry(cfg.Projects)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize project registry: %w", err)
	}

	envCheckers := make(map[string]*EnvChecker)
	for _, proj := range registry.ListProjects() {
		envCheckers[proj.Name] = NewEnvChecker(proj.Directory, nil)
	}

	return &Agent{
		cfg:          cfg,
		registry:     registry,
		envCheckers:  envCheckers,
		lastEnvCheck: make(map[string]*CheckResult),
		running:      false,
		logger:       logger,
	}, nil
}

// Start initializes the agent and begins managing opencode processes.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return fmt.Errorf("agent is already running")
	}

	logger := a.logger
	if logger == nil {
		logger = zap.NewNop()
	}

	a.credApplier = NewCredentialApplier(logger)
	nodeID := nodeIdentifier()
	opencodeURL := fmt.Sprintf("http://127.0.0.1:%d", a.cfg.OpencodePort)
	realAdapter := NewOpencodeAdapter(opencodeURL, "")
	for _, project := range a.cfg.Projects {
		realAdapter.RegisterProjectClient(project.Name, project.Directory, opencodeURL)
	}
	a.opencodeAdapter = realAdapter

	a.wsClient = NewWSClient(
		a.cfg.SupervisorURL,
		a.cfg.AuthToken,
		logger,
		WithNodeID(nodeID),
	)

	if err := RegisterSessionCommandHandlers(a.wsClient, a.opencodeAdapter, logger); err != nil {
		return fmt.Errorf("register session command handlers: %w", err)
	}

	if err := RegisterCredentialPushHandler(a.wsClient, a.credApplier); err != nil {
		return fmt.Errorf("register credential push handler: %w", err)
	}

	if err := RegisterCredentialSyncOnReconnect(a.wsClient, a.credApplier, nodeID); err != nil {
		return fmt.Errorf("register credential sync on reconnect: %w", err)
	}

	authRunner := NewAuthCommandRunner(10*time.Second, logger)
	opencodeStatusCommand := resolveStatusCommand(ToolOpencode, a.cfg.ToolPaths.Opencode, logger)
	claudeStatusCommand := resolveStatusCommand(ToolClaudeCode, a.cfg.ToolPaths.Claude, logger)
	codexStatusCommand := resolveStatusCommand(ToolCodex, a.cfg.ToolPaths.Codex, logger)

	a.oauthExecutor = NewOAuthTriggerExecutor(authRunner, logger)
	if err := RegisterOAuthTriggerHandler(a.wsClient, a.oauthExecutor); err != nil {
		return fmt.Errorf("register oauth trigger handler: %w", err)
	}

	adapters := []AuthAdapter{
		NewOpencodeAuthAdapterWithCommand(authRunner, logger, opencodeStatusCommand),
		NewClaudeAuthAdapterWithCommand(authRunner, logger, claudeStatusCommand),
		NewCodexAuthAdapterWithCommand(authRunner, logger, codexStatusCommand),
	}

	reportInterval := time.Duration(a.cfg.AuthReportIntervalSec) * time.Second
	sender := NewWSAuthStateSender(a.wsClient)
	a.authReporter = NewAuthReporter(adapters, reportInterval, sender, logger)

	reporterCtx, reporterCancel := context.WithCancel(ctx)
	a.authReporterCancel = reporterCancel
	go a.authReporter.Start(reporterCtx)

	a.wsClient.Connect(ctx)

	a.running = true
	logger.Info("agent started",
		zap.String("supervisor_url", a.cfg.SupervisorURL),
		zap.Int("projects", len(a.cfg.Projects)),
	)
	return nil
}

func nodeIdentifier() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown-node"
	}
	return hostname
}

// CheckEnv runs environment checks for a project against the given manifest requirements.
// Returns nil if the project is not found. This is read-only and never mutates the environment.
func (a *Agent) CheckEnv(ctx context.Context, projectName string, reqs *config.ManifestRequirements) *CheckResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	checker, ok := a.envCheckers[projectName]
	if !ok {
		return nil
	}

	result := checker.Check(ctx, reqs)
	a.lastEnvCheck[projectName] = result
	return result
}

// GetLastEnvCheck returns the most recent check result for a project, or nil if none.
func (a *Agent) GetLastEnvCheck(projectName string) *CheckResult {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastEnvCheck[projectName]
}

// ProvisionProject runs the auto-provisioner for a single project.
func (a *Agent) ProvisionProject(projectName string, manifest *config.EnvManifest, templateDir string, emitter EventEmitter) (*shared.ProvisionResult, error) {
	a.mu.RLock()
	reg := a.registry
	a.mu.RUnlock()

	proj := reg.GetProject(projectName)
	if proj == nil {
		return nil, fmt.Errorf("project not found: %s", projectName)
	}

	prov := NewProvisioner(proj.Directory, proj.Name, manifest, templateDir, emitter)
	return prov.Provision()
}

// Stop gracefully shuts down the agent and all managed processes.
func (a *Agent) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return fmt.Errorf("agent is not running")
	}

	if a.authReporterCancel != nil {
		a.authReporterCancel()
	}

	if a.wsClient != nil {
		if err := a.wsClient.Close(); err != nil {
			if a.logger != nil {
				a.logger.Warn("error closing ws client", zap.Error(err))
			}
		}
	}

	a.running = false
	if a.logger != nil {
		a.logger.Info("agent stopped")
	}
	return nil
}

func (a *Agent) GetCredentialEnv() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.credApplier == nil {
		return nil
	}
	return a.credApplier.GetEnv()
}

// IsRunning returns whether the agent is currently running.
func (a *Agent) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}

// GetRegistry returns the project registry.
func (a *Agent) GetRegistry() *ProjectRegistry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.registry
}

// GetConfig returns the agent configuration.
func (a *Agent) GetConfig() *config.AgentConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}
