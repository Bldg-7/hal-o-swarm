package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type CredentialDefaults struct {
	Env map[string]string `json:"env"`
}

type CredentialDistributionConfig struct {
	Version  int64                         `json:"version"`
	Defaults CredentialDefaults            `json:"defaults"`
	Agents   map[string]CredentialDefaults `json:"agents"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type SupervisorConfig struct {
	Server struct {
		Port                  int      `json:"port"`
		HTTPPort              int      `json:"http_port"`
		AuthToken             string   `json:"auth_token"`
		HeartbeatIntervalSec  int      `json:"heartbeat_interval_sec"`
		HeartbeatTimeoutCount int      `json:"heartbeat_timeout_count"`
		AllowedOrigins        []string `json:"allowed_origins"`
	} `json:"server"`
	Channels struct {
		Discord struct {
			BotToken string `json:"bot_token"`
			GuildID  string `json:"guild_id"`
			Channels struct {
				Alerts   string `json:"alerts"`
				DevLog   string `json:"dev-log"`
				BuildLog string `json:"build-log"`
			} `json:"channels"`
		} `json:"discord"`
		Slack struct {
			BotToken string `json:"bot_token"`
			Channels struct {
				Alerts string `json:"alerts"`
				DevLog string `json:"dev-log"`
			} `json:"channels"`
		} `json:"slack"`
		N8n struct {
			WebhookURL string `json:"webhook_url"`
		} `json:"n8n"`
	} `json:"channels"`
	Database     DatabaseConfig               `json:"database"`
	Cost         CostConfig                   `json:"cost"`
	Routes       []interface{}                `json:"routes"`
	Policies     PolicyConfig                 `json:"policies"`
	Dependencies interface{}                  `json:"dependencies"`
	Security     SecurityConfig               `json:"security"`
	Credentials  CredentialDistributionConfig `json:"credentials"`
}

type SecurityConfig struct {
	TLS             TLSConfig           `json:"tls"`
	OriginAllowlist []string            `json:"origin_allowlist"`
	TokenRotation   TokenRotationConfig `json:"token_rotation"`
	Audit           AuditConfig         `json:"audit"`
}

type TLSConfig struct {
	Enabled  bool   `json:"enabled"`
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

type TokenRotationConfig struct {
	Enabled              bool `json:"enabled"`
	CheckIntervalSeconds int  `json:"check_interval_seconds"`
}

type AuditConfig struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
}

type CostConfig struct {
	PollIntervalMinutes int           `json:"poll_interval_minutes"`
	Providers           CostProviders `json:"providers"`
	RequestTimeoutSec   int           `json:"request_timeout_seconds"`
	MaxRetries          int           `json:"max_retries"`
	BackoffBaseMS       int           `json:"backoff_base_ms"`
}

type CostProviders struct {
	Anthropic CostProviderConfig `json:"anthropic"`
	OpenAI    CostProviderConfig `json:"openai"`
}

type CostProviderConfig struct {
	APIKey      string               `json:"api_key"`
	Enabled     *bool                `json:"enabled,omitempty"`
	ModelRates  map[string]ModelRate `json:"model_rates"`
	BaseURL     string               `json:"base_url,omitempty"`
	AdminAPIKey string               `json:"admin_api_key,omitempty"`
	OrgAPIKey   string               `json:"org_api_key,omitempty"`
}

type ModelRate struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

func (p CostProviderConfig) EffectiveAPIKey() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	if p.AdminAPIKey != "" {
		return p.AdminAPIKey
	}
	return p.OrgAPIKey
}

func (p CostProviderConfig) IsEnabled() bool {
	if p.Enabled != nil {
		return *p.Enabled
	}
	return p.EffectiveAPIKey() != ""
}

type PolicyConfig struct {
	ResumeOnIdle        IdlePolicyConfig       `json:"resume_on_idle"`
	RestartOnCompaction CompactionPolicyConfig `json:"restart_on_compaction"`
	KillOnCost          CostPolicyConfig       `json:"kill_on_cost"`
	CheckIntervalSec    int                    `json:"check_interval_seconds"`
}

type IdlePolicyConfig struct {
	Enabled           bool `json:"enabled"`
	IdleThresholdSec  int  `json:"idle_threshold_seconds"`
	MaxRetries        int  `json:"max_retries"`
	RetryResetSeconds int  `json:"retry_reset_seconds"`
}

type CompactionPolicyConfig struct {
	Enabled           bool `json:"enabled"`
	TokenThreshold    int  `json:"token_threshold"`
	MaxRetries        int  `json:"max_retries"`
	RetryResetSeconds int  `json:"retry_reset_seconds"`
}

type CostPolicyConfig struct {
	Enabled           bool    `json:"enabled"`
	CostThresholdUSD  float64 `json:"cost_threshold_usd"`
	MaxRetries        int     `json:"max_retries"`
	RetryResetSeconds int     `json:"retry_reset_seconds"`
}

const (
	defaultPolicyCheckIntervalSec   = 30
	defaultResumeIdleThresholdSec   = 300
	defaultResumeMaxRetries         = 3
	defaultResumeRetryResetSec      = 3600
	defaultCompactionTokenThreshold = 180000
	defaultCompactionMaxRetries     = 2
	defaultCompactionRetryResetSec  = 3600
	defaultKillCostThresholdUSD     = 10.0
	defaultKillMaxRetries           = 1
	defaultKillRetryResetSec        = 86400
	defaultCostPollIntervalMinutes  = 60
	defaultCostRequestTimeoutSec    = 15
	defaultCostMaxRetries           = 3
	defaultCostBackoffBaseMS        = 500

	defaultTokenRotationCheckIntervalSec = 300
	defaultAuditRetentionDays            = 90
)

func LoadSupervisorConfig(path string) (*SupervisorConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg SupervisorConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := validateSupervisorConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateSupervisorConfig(cfg *SupervisorConfig) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("validation error: server.port must be between 1 and 65535, got %d", cfg.Server.Port)
	}
	if cfg.Server.HTTPPort < 0 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("validation error: server.http_port must be between 0 and 65535, got %d", cfg.Server.HTTPPort)
	}
	if cfg.Server.AuthToken == "" {
		return fmt.Errorf("validation error: server.auth_token is required")
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = "./supervisor.db"
	}
	if cfg.Server.HeartbeatIntervalSec <= 0 {
		return fmt.Errorf("validation error: server.heartbeat_interval_sec must be positive, got %d", cfg.Server.HeartbeatIntervalSec)
	}
	if cfg.Server.HeartbeatTimeoutCount <= 0 {
		return fmt.Errorf("validation error: server.heartbeat_timeout_count must be positive, got %d", cfg.Server.HeartbeatTimeoutCount)
	}
	if cfg.Cost.PollIntervalMinutes <= 0 {
		cfg.Cost.PollIntervalMinutes = defaultCostPollIntervalMinutes
	}
	if cfg.Cost.RequestTimeoutSec <= 0 {
		cfg.Cost.RequestTimeoutSec = defaultCostRequestTimeoutSec
	}
	if cfg.Cost.MaxRetries <= 0 {
		cfg.Cost.MaxRetries = defaultCostMaxRetries
	}
	if cfg.Cost.BackoffBaseMS <= 0 {
		cfg.Cost.BackoffBaseMS = defaultCostBackoffBaseMS
	}

	if err := validateCostProviderConfig("anthropic", cfg.Cost.Providers.Anthropic); err != nil {
		return err
	}
	if err := validateCostProviderConfig("openai", cfg.Cost.Providers.OpenAI); err != nil {
		return err
	}

	cfg.applyPolicyDefaults()

	if cfg.Policies.CheckIntervalSec <= 0 {
		return fmt.Errorf("validation error: policies.check_interval_seconds must be positive, got %d", cfg.Policies.CheckIntervalSec)
	}
	if cfg.Policies.ResumeOnIdle.IdleThresholdSec <= 0 {
		return fmt.Errorf("validation error: policies.resume_on_idle.idle_threshold_seconds must be positive, got %d", cfg.Policies.ResumeOnIdle.IdleThresholdSec)
	}
	if cfg.Policies.ResumeOnIdle.MaxRetries <= 0 {
		return fmt.Errorf("validation error: policies.resume_on_idle.max_retries must be positive, got %d", cfg.Policies.ResumeOnIdle.MaxRetries)
	}
	if cfg.Policies.ResumeOnIdle.RetryResetSeconds <= 0 {
		return fmt.Errorf("validation error: policies.resume_on_idle.retry_reset_seconds must be positive, got %d", cfg.Policies.ResumeOnIdle.RetryResetSeconds)
	}

	if cfg.Policies.RestartOnCompaction.TokenThreshold <= 0 {
		return fmt.Errorf("validation error: policies.restart_on_compaction.token_threshold must be positive, got %d", cfg.Policies.RestartOnCompaction.TokenThreshold)
	}
	if cfg.Policies.RestartOnCompaction.MaxRetries <= 0 {
		return fmt.Errorf("validation error: policies.restart_on_compaction.max_retries must be positive, got %d", cfg.Policies.RestartOnCompaction.MaxRetries)
	}
	if cfg.Policies.RestartOnCompaction.RetryResetSeconds <= 0 {
		return fmt.Errorf("validation error: policies.restart_on_compaction.retry_reset_seconds must be positive, got %d", cfg.Policies.RestartOnCompaction.RetryResetSeconds)
	}

	if cfg.Policies.KillOnCost.CostThresholdUSD <= 0 {
		return fmt.Errorf("validation error: policies.kill_on_cost.cost_threshold_usd must be positive, got %f", cfg.Policies.KillOnCost.CostThresholdUSD)
	}
	if cfg.Policies.KillOnCost.MaxRetries <= 0 {
		return fmt.Errorf("validation error: policies.kill_on_cost.max_retries must be positive, got %d", cfg.Policies.KillOnCost.MaxRetries)
	}
	if cfg.Policies.KillOnCost.RetryResetSeconds <= 0 {
		return fmt.Errorf("validation error: policies.kill_on_cost.retry_reset_seconds must be positive, got %d", cfg.Policies.KillOnCost.RetryResetSeconds)
	}

	cfg.applySecurityDefaults()

	if cfg.Security.TLS.Enabled {
		if cfg.Security.TLS.CertPath == "" {
			return fmt.Errorf("validation error: security.tls.cert_path is required when TLS is enabled")
		}
		if cfg.Security.TLS.KeyPath == "" {
			return fmt.Errorf("validation error: security.tls.key_path is required when TLS is enabled")
		}
	}

	if err := validateCredentialDistributionConfig(&cfg.Credentials); err != nil {
		return err
	}

	return nil
}

func validateCostProviderConfig(provider string, cfg CostProviderConfig) error {
	if !cfg.IsEnabled() {
		return nil
	}

	if cfg.EffectiveAPIKey() == "" {
		return fmt.Errorf("validation error: cost.providers.%s.api_key is required when provider is enabled", provider)
	}

	for model, rates := range cfg.ModelRates {
		if model == "" {
			return fmt.Errorf("validation error: cost.providers.%s.model_rates keys must not be empty", provider)
		}
		if rates.Input < 0 || rates.Output < 0 {
			return fmt.Errorf("validation error: cost.providers.%s.model_rates.%s rates must be >= 0", provider, model)
		}
	}

	return nil
}

func validateCredentialDistributionConfig(cfg *CredentialDistributionConfig) error {
	// If credentials section is not provided, it's valid (optional)
	if cfg == nil {
		return nil
	}

	// Validate version is non-negative
	if cfg.Version < 0 {
		return fmt.Errorf("validation error: credentials.version must be >= 0, got %d", cfg.Version)
	}

	// Validate defaults env map
	for key, value := range cfg.Defaults.Env {
		if value == "" {
			return fmt.Errorf("validation error: credentials.defaults.env.%s must not be empty", key)
		}
	}

	// Validate per-agent env maps
	for agentName, creds := range cfg.Agents {
		for key, value := range creds.Env {
			if value == "" {
				return fmt.Errorf("validation error: credentials.agents.%s.env.%s must not be empty", agentName, key)
			}
		}
	}

	return nil
}

func (cfg *SupervisorConfig) applyPolicyDefaults() {
	if cfg.Policies.CheckIntervalSec <= 0 {
		cfg.Policies.CheckIntervalSec = defaultPolicyCheckIntervalSec
	}

	if cfg.Policies.ResumeOnIdle.IdleThresholdSec <= 0 {
		cfg.Policies.ResumeOnIdle.IdleThresholdSec = defaultResumeIdleThresholdSec
	}
	if cfg.Policies.ResumeOnIdle.MaxRetries <= 0 {
		cfg.Policies.ResumeOnIdle.MaxRetries = defaultResumeMaxRetries
	}
	if cfg.Policies.ResumeOnIdle.RetryResetSeconds <= 0 {
		cfg.Policies.ResumeOnIdle.RetryResetSeconds = defaultResumeRetryResetSec
	}

	if cfg.Policies.RestartOnCompaction.TokenThreshold <= 0 {
		cfg.Policies.RestartOnCompaction.TokenThreshold = defaultCompactionTokenThreshold
	}
	if cfg.Policies.RestartOnCompaction.MaxRetries <= 0 {
		cfg.Policies.RestartOnCompaction.MaxRetries = defaultCompactionMaxRetries
	}
	if cfg.Policies.RestartOnCompaction.RetryResetSeconds <= 0 {
		cfg.Policies.RestartOnCompaction.RetryResetSeconds = defaultCompactionRetryResetSec
	}

	if cfg.Policies.KillOnCost.CostThresholdUSD <= 0 {
		cfg.Policies.KillOnCost.CostThresholdUSD = defaultKillCostThresholdUSD
	}
	if cfg.Policies.KillOnCost.MaxRetries <= 0 {
		cfg.Policies.KillOnCost.MaxRetries = defaultKillMaxRetries
	}
	if cfg.Policies.KillOnCost.RetryResetSeconds <= 0 {
		cfg.Policies.KillOnCost.RetryResetSeconds = defaultKillRetryResetSec
	}
}

func (cfg *SupervisorConfig) applySecurityDefaults() {
	if cfg.Security.TokenRotation.CheckIntervalSeconds <= 0 {
		cfg.Security.TokenRotation.CheckIntervalSeconds = defaultTokenRotationCheckIntervalSec
	}
	if cfg.Security.Audit.RetentionDays <= 0 {
		cfg.Security.Audit.RetentionDays = defaultAuditRetentionDays
	}
}
