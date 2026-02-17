package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type commandResultSender interface {
	SendEnvelope(env *shared.Envelope) error
}

type sessionCommand struct {
	CommandID string                 `json:"command_id"`
	Type      string                 `json:"type"`
	Target    sessionCommandTarget   `json:"target"`
	Args      map[string]interface{} `json:"args"`
}

type sessionCommandTarget struct {
	Project string `json:"project"`
}

type sessionCommandResult struct {
	CommandID string    `json:"command_id"`
	Status    string    `json:"status"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

const (
	commandStatusSuccess = "success"
	commandStatusFailure = "failure"
)

func RegisterSessionCommandHandlers(client *WSClient, adapter OpencodeAdapter, logger *zap.Logger) error {
	if client == nil {
		return fmt.Errorf("ws client is required")
	}
	if adapter == nil {
		return fmt.Errorf("opencode adapter is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	handler := HandleSessionCommand(adapter, client, logger)
	client.RegisterCommandHandler("create_session", handler)
	client.RegisterCommandHandler("prompt_session", handler)
	client.RegisterCommandHandler("kill_session", handler)
	client.RegisterCommandHandler("restart_session", handler)
	client.RegisterCommandHandler("session_status", handler)
	return nil
}

func HandleSessionCommand(adapter OpencodeAdapter, sender commandResultSender, logger *zap.Logger) CommandHandler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(ctx context.Context, envelope *shared.Envelope) error {
		if adapter == nil {
			return fmt.Errorf("opencode adapter is required")
		}
		if sender == nil {
			return fmt.Errorf("command result sender is required")
		}
		if envelope == nil {
			return fmt.Errorf("command envelope is required")
		}

		cmd := sessionCommand{}
		if err := json.Unmarshal(envelope.Payload, &cmd); err != nil {
			result := sessionCommandResult{
				CommandID: resolveCommandID(cmd.CommandID, envelope.RequestID),
				Status:    commandStatusFailure,
				Error:     fmt.Sprintf("unmarshal session command payload: %v", err),
				Timestamp: time.Now().UTC(),
			}
			if sendErr := sendSessionCommandResult(sender, envelope.RequestID, result); sendErr != nil {
				return fmt.Errorf("unmarshal session command payload: %w; send command result: %v", err, sendErr)
			}
			return nil
		}

		result := sessionCommandResult{
			CommandID: resolveCommandID(cmd.CommandID, envelope.RequestID),
			Status:    commandStatusSuccess,
			Timestamp: time.Now().UTC(),
		}

		if err := executeSessionCommand(ctx, adapter, cmd, &result); err != nil {
			result.Status = commandStatusFailure
			result.Error = err.Error()
			logger.Warn("session command execution failed",
				zap.String("command_type", cmd.Type),
				zap.String("command_id", result.CommandID),
				zap.Error(err),
			)
		}

		if err := sendSessionCommandResult(sender, envelope.RequestID, result); err != nil {
			return fmt.Errorf("send session command result: %w", err)
		}

		return nil
	}
}

func executeSessionCommand(ctx context.Context, adapter OpencodeAdapter, cmd sessionCommand, result *sessionCommandResult) error {
	if result == nil {
		return fmt.Errorf("command result is required")
	}

	switch cmd.Type {
	case "create_session":
		prompt := readStringArg(cmd.Args, "prompt")
		sessionID, err := adapter.CreateSession(ctx, cmd.Target.Project, prompt)
		if err != nil {
			return err
		}
		result.Output = string(sessionID)
		return nil
	case "prompt_session":
		sessionID := SessionID(readStringArg(cmd.Args, "session_id"))
		if sessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		message := readStringArg(cmd.Args, "message")
		if message == "" {
			message = readStringArg(cmd.Args, "prompt")
		}
		return adapter.PromptSession(ctx, sessionID, message)
	case "kill_session":
		sessionID := SessionID(readStringArg(cmd.Args, "session_id"))
		if sessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		return adapter.KillSession(ctx, sessionID)
	case "session_status":
		sessionID := SessionID(readStringArg(cmd.Args, "session_id"))
		if sessionID == "" {
			return fmt.Errorf("session_id is required")
		}
		status, err := adapter.SessionStatus(ctx, sessionID)
		if err != nil {
			return err
		}
		result.Output = string(status)
		return nil
	case "restart_session":
		sessionID := SessionID(readStringArg(cmd.Args, "session_id"))
		if sessionID != "" {
			_ = adapter.KillSession(ctx, sessionID)
		}
		newSessionID, err := adapter.CreateSession(ctx, cmd.Target.Project, "restart")
		if err != nil {
			return err
		}
		result.Output = string(newSessionID)
		return nil
	default:
		return fmt.Errorf("unsupported command type %s", cmd.Type)
	}
}

func sendSessionCommandResult(sender commandResultSender, fallbackRequestID string, result sessionCommandResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal command result payload: %w", err)
	}

	requestID := result.CommandID
	if requestID == "" {
		requestID = fallbackRequestID
	}

	response := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeCommandResult),
		RequestID: requestID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}

	return sender.SendEnvelope(response)
}

func resolveCommandID(payloadCommandID, requestID string) string {
	if payloadCommandID != "" {
		return payloadCommandID
	}
	return requestID
}

func readStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
