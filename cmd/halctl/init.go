package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func handleInit(args []string) {
	if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "Error: init takes no subcommand\n")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)

	defaultSupervisorPath := "/etc/hal-o-swarm/supervisor.config.json"
	defaultAgentPath := "/etc/hal-o-swarm/agent.config.json"
	defaultSupervisorHost := "127.0.0.1"
	defaultWSPort := 8420
	defaultHTTPPort := 8421
	token, err := randomTokenHex(32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to generate token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("HAL-O-SWARM unified init wizard")
	fmt.Println("This will create/update supervisor and agent config files.")
	fmt.Printf("- Supervisor config: %s\n", defaultSupervisorPath)
	fmt.Printf("- Agent config: %s\n", defaultAgentPath)
	fmt.Println()

	supervisorPath := defaultSupervisorPath
	agentPath := defaultAgentPath
	supervisorHost := promptString(reader, "Supervisor host/IP for agent connection", defaultSupervisorHost, true)
	wsPort := promptInt(reader, "Supervisor WS port", defaultWSPort)
	httpPort := promptInt(reader, "Supervisor HTTP port", defaultHTTPPort)
	authToken := promptString(reader, "Shared auth token", token, true)

	supervisorCfg := defaultSupervisorConfig()
	supervisorCfg.Server.Port = wsPort
	supervisorCfg.Server.HTTPPort = httpPort
	supervisorCfg.Server.AuthToken = authToken

	if promptYesNo(reader, "Configure Discord integration now?", false) {
		supervisorCfg.Channels.Discord.BotToken = promptString(reader, "Discord bot token", "", true)
		supervisorCfg.Channels.Discord.GuildID = promptString(reader, "Discord guild id", "", true)
		supervisorCfg.Channels.Discord.Channels.Alerts = promptString(reader, "Discord alerts channel id", "", true)
		supervisorCfg.Channels.Discord.Channels.DevLog = promptString(reader, "Discord dev-log channel id", "", true)
		supervisorCfg.Channels.Discord.Channels.BuildLog = promptString(reader, "Discord build-log channel id", "", true)
	}

	if promptYesNo(reader, "Configure Anthropic cost provider now?", false) {
		supervisorCfg.Cost.Providers.Anthropic.AdminAPIKey = promptString(reader, "Anthropic admin api key", "", true)
	}
	if promptYesNo(reader, "Configure OpenAI cost provider now?", false) {
		supervisorCfg.Cost.Providers.OpenAI.OrgAPIKey = promptString(reader, "OpenAI org api key", "", true)
	}

	if promptYesNo(reader, "Set default ANTHROPIC_API_KEY for agent credential sync?", false) {
		key := promptString(reader, "Default ANTHROPIC_API_KEY", "", true)
		supervisorCfg.Credentials.Defaults.Env["ANTHROPIC_API_KEY"] = key
	}

	agentCfg := defaultAgentConfig()
	agentCfg.SupervisorURL = fmt.Sprintf("ws://%s:%d/ws/agent", supervisorHost, wsPort)
	agentCfg.AuthToken = authToken

	agentCfg.OpencodePort = promptInt(reader, "Agent opencode port", agentCfg.OpencodePort)
	agentCfg.AuthReportIntervalSec = promptInt(reader, "Agent auth report interval sec", agentCfg.AuthReportIntervalSec)

	for promptYesNo(reader, "Add a project to this agent now?", false) {
		name := promptString(reader, "Project name", "", true)
		dir := promptString(reader, "Project directory", "", true)
		agentCfg.Projects = append(agentCfg.Projects, projectConfig{Name: name, Directory: dir})
	}

	if err := writeJSONFile(supervisorPath, supervisorCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing supervisor config: %v\n", err)
		os.Exit(1)
	}
	if err := writeJSONFile(agentPath, agentCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing agent config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Done.")
	fmt.Printf("- Supervisor config: %s\n", supervisorPath)
	fmt.Printf("- Agent config: %s\n", agentPath)
	fmt.Printf("- Agent supervisor_url: %s\n", agentCfg.SupervisorURL)
	fmt.Println("- Next: start services with systemd (or run binaries directly)")
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

func writeJSONFile(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
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
