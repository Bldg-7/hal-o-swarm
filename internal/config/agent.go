package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type AgentConfig struct {
	SupervisorURL         string `json:"supervisor_url"`
	AuthToken             string `json:"auth_token"`
	OpencodePort          int    `json:"opencode_port"`
	AuthReportIntervalSec int    `json:"auth_report_interval_sec"`
	Projects              []struct {
		Name      string `json:"name"`
		Directory string `json:"directory"`
	} `json:"projects"`
}

func LoadAgentConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := validateAgentConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validateAgentConfig(cfg *AgentConfig) error {
	if cfg.AuthReportIntervalSec <= 0 {
		cfg.AuthReportIntervalSec = 30
	}

	if cfg.SupervisorURL == "" {
		return fmt.Errorf("validation error: supervisor_url is required")
	}
	if cfg.AuthToken == "" {
		return fmt.Errorf("validation error: auth_token is required")
	}
	if cfg.OpencodePort <= 0 || cfg.OpencodePort > 65535 {
		return fmt.Errorf("validation error: opencode_port must be between 1 and 65535, got %d", cfg.OpencodePort)
	}
	if len(cfg.Projects) == 0 {
		return fmt.Errorf("validation error: projects array must not be empty")
	}
	for i, proj := range cfg.Projects {
		if proj.Name == "" {
			return fmt.Errorf("validation error: projects[%d].name is required", i)
		}
		if proj.Directory == "" {
			return fmt.Errorf("validation error: projects[%d].directory is required", i)
		}
	}
	return nil
}
