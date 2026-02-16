package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/hal-o-swarm/hal-o-swarm/internal/config"
)

// Agent represents a local hal-agent instance managing projects and opencode processes.
type Agent struct {
	cfg      *config.AgentConfig
	registry *ProjectRegistry
	mu       sync.RWMutex
	running  bool
}

// NewAgent creates a new Agent instance with the given config.
func NewAgent(cfg *config.AgentConfig) (*Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// Load and validate projects
	registry, err := NewProjectRegistry(cfg.Projects)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize project registry: %w", err)
	}

	return &Agent{
		cfg:      cfg,
		registry: registry,
		running:  false,
	}, nil
}

// Start initializes the agent and begins managing opencode processes.
// This is a skeleton implementation; actual process management is in T10.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return fmt.Errorf("agent is already running")
	}

	// TODO (T10): Start opencode serve processes for each project
	// TODO (T7): Connect to supervisor via WebSocket
	// TODO (T17): Check environment requirements

	a.running = true
	return nil
}

// Stop gracefully shuts down the agent and all managed processes.
func (a *Agent) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return fmt.Errorf("agent is not running")
	}

	// TODO (T10): Stop all opencode serve processes
	// TODO (T7): Close supervisor WebSocket connection

	a.running = false
	return nil
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
