package supervisor

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type CommandType string

const (
	CommandTypeCreateSession  CommandType = "create_session"
	CommandTypePromptSession  CommandType = "prompt_session"
	CommandTypeKillSession    CommandType = "kill_session"
	CommandTypeRestartSession CommandType = "restart_session"
	CommandTypeSessionStatus  CommandType = "session_status"
	CommandTypeHandover       CommandType = "handover"
	CommandTypeCredentialPush CommandType = "credential_push"
	CommandTypeOAuthTrigger   CommandType = "oauth_trigger"
	CommandTypeEnvCheck       CommandType = "env_check"
	CommandTypeEnvProvision   CommandType = "env_provision"
	CommandTypeAgentMDDiff    CommandType = "agentmd_diff"
	CommandTypeAgentMDSync    CommandType = "agentmd_sync"
)

type CommandStatus string

const (
	CommandStatusSuccess CommandStatus = "success"
	CommandStatusFailure CommandStatus = "failure"
	CommandStatusTimeout CommandStatus = "timeout"
)

const (
	DefaultCommandTimeout  = 30 * time.Second
	HandoverCommandTimeout = 60 * time.Second
)

type CommandTarget struct {
	Project string `json:"project,omitempty"`
	NodeID  string `json:"node_id,omitempty"`
}

type Command struct {
	CommandID      string                 `json:"command_id"`
	Type           CommandType            `json:"type"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	Target         CommandTarget          `json:"target"`
	Args           map[string]interface{} `json:"args,omitempty"`
	Timeout        time.Duration          `json:"timeout,omitempty"`
}

type CommandResult struct {
	CommandID string        `json:"command_id"`
	Status    CommandStatus `json:"status"`
	Output    string        `json:"output,omitempty"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

func ParseCommandIntent(intent string) (CommandType, error) {
	normalized := strings.TrimSpace(strings.ToLower(intent))
	switch normalized {
	case string(CommandTypeCreateSession), "start", "/start":
		return CommandTypeCreateSession, nil
	case string(CommandTypePromptSession), "inject", "/inject", "resume", "/resume":
		return CommandTypePromptSession, nil
	case string(CommandTypeKillSession), "kill", "/kill":
		return CommandTypeKillSession, nil
	case string(CommandTypeRestartSession), "restart", "/restart":
		return CommandTypeRestartSession, nil
	case string(CommandTypeSessionStatus), "status", "/status":
		return CommandTypeSessionStatus, nil
	case string(CommandTypeHandover), "/handover":
		return CommandTypeHandover, nil
	case string(CommandTypeCredentialPush), "push_credentials", "/push_credentials":
		return CommandTypeCredentialPush, nil
	case string(CommandTypeOAuthTrigger), "trigger_oauth", "/trigger_oauth":
		return CommandTypeOAuthTrigger, nil
	case string(CommandTypeEnvCheck), "check_env", "/check_env":
		return CommandTypeEnvCheck, nil
	case string(CommandTypeEnvProvision), "provision_env", "/provision_env":
		return CommandTypeEnvProvision, nil
	case string(CommandTypeAgentMDDiff), "diff_agentmd", "/diff_agentmd":
		return CommandTypeAgentMDDiff, nil
	case string(CommandTypeAgentMDSync), "sync_agentmd", "/sync_agentmd":
		return CommandTypeAgentMDSync, nil
	default:
		return "", fmt.Errorf("unsupported command intent %q", intent)
	}
}

func IsSupportedCommandType(commandType CommandType) bool {
	switch commandType {
	case CommandTypeCreateSession,
		CommandTypePromptSession,
		CommandTypeKillSession,
		CommandTypeRestartSession,
		CommandTypeSessionStatus,
		CommandTypeHandover,
		CommandTypeCredentialPush,
		CommandTypeOAuthTrigger,
		CommandTypeEnvCheck,
		CommandTypeEnvProvision,
		CommandTypeAgentMDDiff,
		CommandTypeAgentMDSync:
		return true
	default:
		return false
	}
}

func (c Command) EffectiveTimeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	if c.Type == CommandTypeHandover {
		return HandoverCommandTimeout
	}
	return DefaultCommandTimeout
}

func (c Command) canonicalPayload() ([]byte, error) {
	payload := struct {
		Type    CommandType            `json:"type"`
		Target  CommandTarget          `json:"target"`
		Args    map[string]interface{} `json:"args,omitempty"`
		Timeout int64                  `json:"timeout_ms"`
	}{
		Type:    c.Type,
		Target:  c.Target,
		Args:    c.Args,
		Timeout: c.EffectiveTimeout().Milliseconds(),
	}

	return json.Marshal(payload)
}
