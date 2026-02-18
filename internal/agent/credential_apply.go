package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type CredentialApplier struct {
	mu                  sync.RWMutex
	envVars             map[string]string
	version             int
	appliedCommandIDs   map[string]struct{}
	appliedCommandOrder []string
	logger              *zap.Logger
}

type CredentialVersionReport struct {
	NodeID            string `json:"node_id"`
	CredentialVersion int    `json:"credential_version"`
}

const maxAppliedCredentialCommandIDs = 1000

func NewCredentialApplier(logger *zap.Logger) *CredentialApplier {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &CredentialApplier{
		envVars:           make(map[string]string),
		appliedCommandIDs: make(map[string]struct{}),
		logger:            logger,
	}
}

func (a *CredentialApplier) Apply(payload shared.CredentialPushPayload) error {
	_, err := a.ApplyIfNew("", payload)
	return err
}

func (a *CredentialApplier) ApplyIfNew(commandID string, payload shared.CredentialPushPayload) (bool, error) {
	if err := payload.Validate(); err != nil {
		return false, err
	}
	if payload.Version < 0 {
		return false, fmt.Errorf("validation error: credential_push.version must be >= 0")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if commandID != "" {
		if _, exists := a.appliedCommandIDs[commandID]; exists {
			return false, nil
		}
	}

	a.applyLocked(payload)
	if commandID != "" {
		a.rememberCommandIDLocked(commandID)
	}

	return true, nil
}

func (a *CredentialApplier) applyLocked(payload shared.CredentialPushPayload) {

	envCopy := make(map[string]string, len(payload.EnvVars))
	for key, value := range payload.EnvVars {
		envCopy[key] = value
	}

	a.envVars = envCopy
	a.version = payload.Version
}

func (a *CredentialApplier) rememberCommandIDLocked(commandID string) {
	a.appliedCommandIDs[commandID] = struct{}{}
	a.appliedCommandOrder = append(a.appliedCommandOrder, commandID)

	if len(a.appliedCommandOrder) <= maxAppliedCredentialCommandIDs {
		return
	}

	evictedID := a.appliedCommandOrder[0]
	a.appliedCommandOrder = a.appliedCommandOrder[1:]
	delete(a.appliedCommandIDs, evictedID)
}

func (a *CredentialApplier) GetEnv() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	envCopy := make(map[string]string, len(a.envVars))
	for key, value := range a.envVars {
		envCopy[key] = value
	}

	return envCopy
}

func (a *CredentialApplier) GetVersion() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.version
}

func (a *CredentialApplier) BuildVersionReport(nodeID string) CredentialVersionReport {
	return CredentialVersionReport{
		NodeID:            nodeID,
		CredentialVersion: a.GetVersion(),
	}
}

func (a *CredentialApplier) MaskValue(value string) string {
	if value == "" {
		return value
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, credentialValue := range a.envVars {
		if value == credentialValue {
			return "[REDACTED]"
		}
	}

	return value
}

func RegisterCredentialPushHandler(client *WSClient, applier *CredentialApplier) error {
	if client == nil {
		return fmt.Errorf("ws client is required")
	}
	if applier == nil {
		return fmt.Errorf("credential applier is required")
	}

	client.RegisterCommandHandler("credential_push", HandleCredentialPush(applier))
	return nil
}

func RegisterCredentialSyncOnReconnect(client *WSClient, applier *CredentialApplier, nodeID string) error {
	if client == nil {
		return fmt.Errorf("ws client is required")
	}
	if applier == nil {
		return fmt.Errorf("credential applier is required")
	}
	if nodeID == "" {
		return fmt.Errorf("node id is required")
	}

	WithOnConnectHook(func() error {
		report := applier.BuildVersionReport(nodeID)
		payload, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("marshal credential version report: %w", err)
		}

		env := &shared.Envelope{
			Version:   shared.ProtocolVersion,
			Type:      string(shared.MessageTypeCredentialSync),
			Timestamp: time.Now().UTC().Unix(),
			Payload:   payload,
		}

		return client.SendEnvelope(env)
	})(client)

	return nil
}

func HandleCredentialPush(applier *CredentialApplier) CommandHandler {
	return func(ctx context.Context, envelope *shared.Envelope) error {
		_ = ctx
		if applier == nil {
			return fmt.Errorf("credential applier is required")
		}
		if envelope == nil {
			return fmt.Errorf("command envelope is required")
		}

		var command struct {
			CommandID string `json:"command_id"`
			Type      string `json:"type"`
			Target    struct {
				NodeID string `json:"node_id"`
			} `json:"target"`
			Args struct {
				EnvVars map[string]string `json:"env_vars"`
				Version int               `json:"version"`
			} `json:"args"`
		}

		if err := json.Unmarshal(envelope.Payload, &command); err != nil {
			return fmt.Errorf("unmarshal credential_push command: %w", err)
		}
		if command.Type != "credential_push" {
			return fmt.Errorf("unexpected command type %q", command.Type)
		}

		commandID := command.CommandID
		if commandID == "" {
			commandID = envelope.RequestID
		}

		payload := shared.CredentialPushPayload{
			TargetNode: command.Target.NodeID,
			EnvVars:    command.Args.EnvVars,
			Version:    command.Args.Version,
		}

		applied, err := applier.ApplyIfNew(commandID, payload)
		if err != nil {
			return fmt.Errorf("apply credential payload: %w", err)
		}
		if !applied {
			applier.logger.Info("ignored duplicate credential push command",
				zap.String("command_id", commandID),
				zap.String("request_id", envelope.RequestID),
			)
			return nil
		}

		maskedEnv := make(map[string]string, len(payload.EnvVars))
		for key, value := range payload.EnvVars {
			maskedEnv[key] = applier.MaskValue(value)
		}

		applier.logger.Info("applied credential push payload",
			zap.String("request_id", envelope.RequestID),
			zap.String("target_node", payload.TargetNode),
			zap.Int("version", applier.GetVersion()),
			zap.Any("env_vars", maskedEnv),
		)

		return nil
	}
}
