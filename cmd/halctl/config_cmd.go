package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	appconfig "github.com/Bldg-7/hal-o-swarm/internal/config"
)

const (
	defaultSupervisorConfigPath = "/etc/hal-o-swarm/supervisor.config.json"
	defaultAgentConfigPath      = "/etc/hal-o-swarm/agent.config.json"
	defaultCLIFormat            = "table"
	defaultCLIURL               = "http://localhost:8421"
)

type projectConfig struct {
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

type agentConfigFile struct {
	SupervisorURL         string          `json:"supervisor_url"`
	AuthToken             string          `json:"auth_token"`
	OpencodePort          int             `json:"opencode_port"`
	AuthReportIntervalSec int             `json:"auth_report_interval_sec"`
	Projects              []projectConfig `json:"projects"`
}

type channelMap struct {
	Alerts   string `json:"alerts"`
	DevLog   string `json:"dev-log"`
	BuildLog string `json:"build-log,omitempty"`
}

type supervisorConfigFile struct {
	Server struct {
		Port                  int    `json:"port"`
		HTTPPort              int    `json:"http_port"`
		AuthToken             string `json:"auth_token"`
		HeartbeatIntervalSec  int    `json:"heartbeat_interval_sec"`
		HeartbeatTimeoutCount int    `json:"heartbeat_timeout_count"`
	} `json:"server"`
	Database struct {
		Path string `json:"path"`
	} `json:"database"`
	Channels struct {
		Discord struct {
			BotToken string     `json:"bot_token"`
			GuildID  string     `json:"guild_id"`
			Channels channelMap `json:"channels"`
		} `json:"discord"`
		Slack struct {
			BotToken string `json:"bot_token"`
			Channels struct {
				Alerts string `json:"alerts"`
				DevLog string `json:"dev-log"`
			} `json:"channels"`
		} `json:"slack"`
		N8N struct {
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
	Routes   []interface{} `json:"routes"`
	Policies struct {
		ResumeOnIdle struct {
			Enabled           bool `json:"enabled"`
			IdleThresholdSec  int  `json:"idle_threshold_seconds"`
			MaxRetries        int  `json:"max_retries"`
			RetryResetSeconds int  `json:"retry_reset_seconds"`
		} `json:"resume_on_idle"`
		RestartOnCompaction struct {
			Enabled           bool `json:"enabled"`
			TokenThreshold    int  `json:"token_threshold"`
			MaxRetries        int  `json:"max_retries"`
			RetryResetSeconds int  `json:"retry_reset_seconds"`
		} `json:"restart_on_compaction"`
		KillOnCost struct {
			Enabled           bool    `json:"enabled"`
			CostThresholdUSD  float64 `json:"cost_threshold_usd"`
			MaxRetries        int     `json:"max_retries"`
			RetryResetSeconds int     `json:"retry_reset_seconds"`
		} `json:"kill_on_cost"`
		CheckIntervalSeconds int `json:"check_interval_seconds"`
	} `json:"policies"`
	Dependencies map[string]interface{} `json:"dependencies"`
	Security     struct {
		TLS struct {
			Enabled  bool   `json:"enabled"`
			CertPath string `json:"cert_path"`
			KeyPath  string `json:"key_path"`
		} `json:"tls"`
		OriginAllowlist []string `json:"origin_allowlist"`
		TokenRotation   struct {
			Enabled              bool `json:"enabled"`
			CheckIntervalSeconds int  `json:"check_interval_seconds"`
		} `json:"token_rotation"`
		Audit struct {
			Enabled       bool `json:"enabled"`
			RetentionDays int  `json:"retention_days"`
		} `json:"audit"`
	} `json:"security"`
	Credentials struct {
		Version  int `json:"version"`
		Defaults struct {
			Env map[string]string `json:"env"`
		} `json:"defaults"`
		Agents map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"agents"`
	} `json:"credentials"`
}

type cliConfigFile struct {
	SupervisorURL string `json:"supervisor_url"`
	AuthToken     string `json:"auth_token"`
	Format        string `json:"format"`
}

func handleConfig(args []string) {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "Error: usage is 'halctl config [supervisor|agent|cli]'")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	target := ""
	if len(args) == 1 {
		target = strings.ToLower(strings.TrimSpace(args[0]))
	}

	if target == "" {
		target = promptTarget(reader)
	}

	var err error
	switch target {
	case "supervisor":
		err = configureSupervisor(reader)
	case "agent":
		err = configureAgent(reader)
	case "cli":
		err = configureCLI(reader)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown config target %q (expected supervisor, agent, or cli)\n", target)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func configureSupervisor(reader *bufio.Reader) error {
	path := defaultSupervisorConfigPath
	cfg := defaultSupervisorConfig()
	_ = readJSONIfExists(path, &cfg)

	fmt.Printf("Supervisor config path: %s\n", path)
	if cfg.Server.AuthToken == "" {
		fmt.Println("Current shared token: <not set>")
	} else {
		fmt.Printf("Current shared token: %s\n", cfg.Server.AuthToken)
	}

	setNew := promptYesNo(reader, "Set a new shared auth token now?", cfg.Server.AuthToken == "")
	if setNew {
		gen := promptYesNo(reader, "Generate a secure random token?", true)
		if gen {
			tok, err := randomTokenHex(32)
			if err != nil {
				return fmt.Errorf("failed to generate token: %w", err)
			}
			cfg.Server.AuthToken = tok
		} else {
			cfg.Server.AuthToken = promptString(reader, "New shared auth token", "", true)
		}
	}

	if err := writeJSONAtomic(path, cfg, 0o600); err != nil {
		return fmt.Errorf("writing supervisor config: %w", err)
	}
	if _, err := appconfig.LoadSupervisorConfig(path); err != nil {
		return fmt.Errorf("supervisor config validation failed after write: %w", err)
	}

	fmt.Println("Saved supervisor config.")
	fmt.Println("Restart hal-supervisor and hal-agent if token changed.")
	return nil
}

func configureAgent(reader *bufio.Reader) error {
	path := defaultAgentConfigPath
	cfg := defaultAgentConfig()
	_ = readJSONIfExists(path, &cfg)

	fmt.Printf("Agent config path: %s\n", path)
	cfg.SupervisorURL = promptString(reader, "Agent supervisor URL", cfg.SupervisorURL, true)
	cfg.AuthToken = promptString(reader, "Shared auth token", cfg.AuthToken, true)
	cfg.OpencodePort = promptInt(reader, "Agent opencode port", cfg.OpencodePort)
	cfg.AuthReportIntervalSec = promptInt(reader, "Agent auth report interval sec", cfg.AuthReportIntervalSec)

	if promptYesNo(reader, "Reconfigure project list now?", len(cfg.Projects) == 0) {
		cfg.Projects = []projectConfig{}
		for promptYesNo(reader, "Add a project?", true) {
			name := promptString(reader, "Project name", "", true)
			dir := promptString(reader, "Project directory", "", true)
			cfg.Projects = append(cfg.Projects, projectConfig{Name: name, Directory: dir})
		}
	}

	if err := writeJSONAtomic(path, cfg, 0o600); err != nil {
		return fmt.Errorf("writing agent config: %w", err)
	}
	if _, err := appconfig.LoadAgentConfig(path); err != nil {
		return fmt.Errorf("agent config validation failed after write: %w", err)
	}

	fmt.Println("Saved agent config.")
	fmt.Println("Restart hal-agent to apply changes.")
	return nil
}

func configureCLI(reader *bufio.Reader) error {
	path, err := defaultCLIConfigPath()
	if err != nil {
		return err
	}

	cfg := cliConfigFile{SupervisorURL: defaultCLIURL, Format: defaultCLIFormat}
	_ = readJSONIfExists(path, &cfg)

	fmt.Printf("CLI config path: %s\n", path)
	cfg.SupervisorURL = promptString(reader, "CLI supervisor URL", cfg.SupervisorURL, true)

	if cfg.AuthToken != "" {
		fmt.Printf("Current CLI token: %s\n", cfg.AuthToken)
		if promptYesNo(reader, "Keep current CLI auth token?", true) {
			// keep
		} else {
			cfg.AuthToken = promptString(reader, "New CLI auth token", "", true)
		}
	} else {
		cfg.AuthToken = promptString(reader, "CLI auth token", "", true)
	}

	for {
		format := strings.ToLower(promptString(reader, "Default output format (table/json)", cfg.Format, true))
		if format == "table" || format == "json" {
			cfg.Format = format
			break
		}
		fmt.Println("Enter either 'table' or 'json'.")
	}

	if err := writeJSONAtomic(path, cfg, 0o600); err != nil {
		return fmt.Errorf("writing cli config: %w", err)
	}

	fmt.Println("Saved CLI config.")
	return nil
}

func defaultCLIConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, ".halctl", "config.json"), nil
}

func loadCLIConfig() (*cliConfigFile, error) {
	path, err := defaultCLIConfigPath()
	if err != nil {
		return nil, err
	}

	var cfg cliConfigFile
	if err := readJSONIfExists(path, &cfg); err != nil {
		return nil, err
	}

	if cfg.SupervisorURL == "" && cfg.AuthToken == "" && cfg.Format == "" {
		return nil, nil
	}
	return &cfg, nil
}

func readJSONIfExists(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	return nil
}

func writeJSONAtomic(path string, value interface{}, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".halctl-config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set temp file mode: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}

func promptTarget(reader *bufio.Reader) string {
	fmt.Println("Select config target:")
	fmt.Println("  1) supervisor")
	fmt.Println("  2) agent")
	fmt.Println("  3) cli")

	for {
		choice := promptString(reader, "Target", "1", true)
		switch strings.TrimSpace(choice) {
		case "1", "supervisor":
			return "supervisor"
		case "2", "agent":
			return "agent"
		case "3", "cli":
			return "cli"
		default:
			fmt.Println("Choose 1, 2, 3, or name (supervisor/agent/cli).")
		}
	}
}

func defaultSupervisorConfig() supervisorConfigFile {
	var cfg supervisorConfigFile

	cfg.Server.Port = 8420
	cfg.Server.HTTPPort = 8421
	cfg.Server.HeartbeatIntervalSec = 30
	cfg.Server.HeartbeatTimeoutCount = 3

	cfg.Database.Path = "/var/lib/hal-o-swarm/supervisor.db"

	cfg.Cost.PollIntervalMinutes = 60
	cfg.Routes = []interface{}{}

	cfg.Policies.ResumeOnIdle.Enabled = true
	cfg.Policies.ResumeOnIdle.IdleThresholdSec = 300
	cfg.Policies.ResumeOnIdle.MaxRetries = 3
	cfg.Policies.ResumeOnIdle.RetryResetSeconds = 3600

	cfg.Policies.RestartOnCompaction.Enabled = true
	cfg.Policies.RestartOnCompaction.TokenThreshold = 180000
	cfg.Policies.RestartOnCompaction.MaxRetries = 2
	cfg.Policies.RestartOnCompaction.RetryResetSeconds = 3600

	cfg.Policies.KillOnCost.Enabled = false
	cfg.Policies.KillOnCost.CostThresholdUSD = 10
	cfg.Policies.KillOnCost.MaxRetries = 1
	cfg.Policies.KillOnCost.RetryResetSeconds = 86400
	cfg.Policies.CheckIntervalSeconds = 30

	cfg.Dependencies = map[string]interface{}{}

	cfg.Security.TLS.Enabled = false
	cfg.Security.TokenRotation.Enabled = false
	cfg.Security.TokenRotation.CheckIntervalSeconds = 300
	cfg.Security.Audit.Enabled = true
	cfg.Security.Audit.RetentionDays = 90

	cfg.Credentials.Version = 1
	cfg.Credentials.Defaults.Env = map[string]string{}
	cfg.Credentials.Agents = map[string]struct {
		Env map[string]string `json:"env"`
	}{}

	return cfg
}

func defaultAgentConfig() agentConfigFile {
	return agentConfigFile{
		SupervisorURL:         "ws://127.0.0.1:8420/ws/agent",
		AuthToken:             "",
		OpencodePort:          4096,
		AuthReportIntervalSec: 30,
		Projects:              []projectConfig{},
	}
}

func randomTokenHex(bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func promptString(reader *bufio.Reader, label, def string, required bool) string {
	for {
		if def != "" {
			fmt.Printf("%s [%s]: ", label, def)
		} else {
			fmt.Printf("%s: ", label)
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, "input error, please try again")
			continue
		}
		input = strings.TrimSpace(input)

		if input == "" {
			input = def
		}

		if required && strings.TrimSpace(input) == "" {
			fmt.Println("Value is required.")
			continue
		}

		return input
	}
}

func promptInt(reader *bufio.Reader, label string, def int) int {
	for {
		raw := promptString(reader, label, strconv.Itoa(def), true)
		val, err := strconv.Atoi(raw)
		if err != nil || val <= 0 || val > 65535 {
			fmt.Println("Enter a valid integer between 1 and 65535.")
			continue
		}
		return val
	}
}

func promptYesNo(reader *bufio.Reader, label string, def bool) bool {
	defLabel := "y/N"
	if def {
		defLabel = "Y/n"
	}

	for {
		fmt.Printf("%s [%s]: ", label, defLabel)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Fprintln(os.Stderr, "input error, please try again")
			continue
		}

		switch strings.ToLower(strings.TrimSpace(input)) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println("Enter y or n.")
		}
	}
}
