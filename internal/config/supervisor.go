package config

import (
	"encoding/json"
	"fmt"
	"os"
)

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
	Cost struct {
		PollIntervalMinutes int `json:"poll_interval_minutes"`
		Providers           struct {
			Anthropic struct {
				AdminAPIKey string `json:"admin_api_key"`
			} `json:"anthropic"`
			OpenAI struct {
				OrgAPIKey string `json:"org_api_key"`
			} `json:"openai"`
		} `json:"providers"`
	} `json:"cost"`
	Routes       []interface{} `json:"routes"`
	Policies     PolicyConfig  `json:"policies"`
	Dependencies interface{}   `json:"dependencies"`
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
	if cfg.Server.HeartbeatIntervalSec <= 0 {
		return fmt.Errorf("validation error: server.heartbeat_interval_sec must be positive, got %d", cfg.Server.HeartbeatIntervalSec)
	}
	if cfg.Server.HeartbeatTimeoutCount <= 0 {
		return fmt.Errorf("validation error: server.heartbeat_timeout_count must be positive, got %d", cfg.Server.HeartbeatTimeoutCount)
	}
	if cfg.Cost.PollIntervalMinutes <= 0 {
		return fmt.Errorf("validation error: cost.poll_interval_minutes must be positive, got %d", cfg.Cost.PollIntervalMinutes)
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
