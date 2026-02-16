package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type SupervisorConfig struct {
	Server struct {
		Port                  int    `json:"port"`
		AuthToken             string `json:"auth_token"`
		HeartbeatIntervalSec  int    `json:"heartbeat_interval_sec"`
		HeartbeatTimeoutCount int    `json:"heartbeat_timeout_count"`
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
	Routes           []interface{} `json:"routes"`
	AutoIntervention interface{}   `json:"auto_intervention"`
	Dependencies     interface{}   `json:"dependencies"`
}

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
	return nil
}
