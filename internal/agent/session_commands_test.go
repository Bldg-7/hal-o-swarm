package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type captureCommandResultSender struct {
	envelopes []*shared.Envelope
}

func (s *captureCommandResultSender) SendEnvelope(env *shared.Envelope) error {
	s.envelopes = append(s.envelopes, env)
	return nil
}

func (s *captureCommandResultSender) lastEnvelope(t *testing.T) *shared.Envelope {
	t.Helper()
	if len(s.envelopes) == 0 {
		t.Fatal("expected command result envelope")
	}
	return s.envelopes[len(s.envelopes)-1]
}

func TestHandleSessionCommandLifecycle(t *testing.T) {
	adapter := NewMockOpencodeAdapter()
	sender := &captureCommandResultSender{}
	handler := HandleSessionCommand(adapter, sender, zap.NewNop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	create := makeCommandEnvelope(t, map[string]interface{}{
		"command_id": "cmd-create",
		"type":       "create_session",
		"target":     map[string]interface{}{"project": "proj-a"},
		"args":       map[string]interface{}{"prompt": "bootstrap"},
	})
	if err := handler(ctx, create); err != nil {
		t.Fatalf("create_session handler returned error: %v", err)
	}
	createResult := decodeCommandResult(t, sender.lastEnvelope(t))
	if createResult.Status != commandStatusSuccess {
		t.Fatalf("expected create_session success, got %s (%s)", createResult.Status, createResult.Error)
	}
	if createResult.Output == "" {
		t.Fatal("expected create_session output session id")
	}

	prompt := makeCommandEnvelope(t, map[string]interface{}{
		"command_id": "cmd-prompt",
		"type":       "prompt_session",
		"target":     map[string]interface{}{"project": "proj-a"},
		"args": map[string]interface{}{
			"session_id": createResult.Output,
			"prompt":     "continue",
		},
	})
	if err := handler(ctx, prompt); err != nil {
		t.Fatalf("prompt_session handler returned error: %v", err)
	}
	promptResult := decodeCommandResult(t, sender.lastEnvelope(t))
	if promptResult.Status != commandStatusSuccess {
		t.Fatalf("expected prompt_session success, got %s (%s)", promptResult.Status, promptResult.Error)
	}

	status := makeCommandEnvelope(t, map[string]interface{}{
		"command_id": "cmd-status",
		"type":       "session_status",
		"target":     map[string]interface{}{"project": "proj-a"},
		"args": map[string]interface{}{
			"session_id": createResult.Output,
		},
	})
	if err := handler(ctx, status); err != nil {
		t.Fatalf("session_status handler returned error: %v", err)
	}
	statusResult := decodeCommandResult(t, sender.lastEnvelope(t))
	if statusResult.Status != commandStatusSuccess {
		t.Fatalf("expected session_status success, got %s (%s)", statusResult.Status, statusResult.Error)
	}
	if statusResult.Output != string(SessionStatusRunning) {
		t.Fatalf("expected status output %q, got %q", SessionStatusRunning, statusResult.Output)
	}

	restart := makeCommandEnvelope(t, map[string]interface{}{
		"command_id": "cmd-restart",
		"type":       "restart_session",
		"target":     map[string]interface{}{"project": "proj-a"},
		"args": map[string]interface{}{
			"session_id": createResult.Output,
		},
	})
	if err := handler(ctx, restart); err != nil {
		t.Fatalf("restart_session handler returned error: %v", err)
	}
	restartResult := decodeCommandResult(t, sender.lastEnvelope(t))
	if restartResult.Status != commandStatusSuccess {
		t.Fatalf("expected restart_session success, got %s (%s)", restartResult.Status, restartResult.Error)
	}
	if restartResult.Output == "" || restartResult.Output == createResult.Output {
		t.Fatalf("expected restart_session to return new session id, got %q", restartResult.Output)
	}

	kill := makeCommandEnvelope(t, map[string]interface{}{
		"command_id": "cmd-kill",
		"type":       "kill_session",
		"target":     map[string]interface{}{"project": "proj-a"},
		"args": map[string]interface{}{
			"session_id": restartResult.Output,
		},
	})
	if err := handler(ctx, kill); err != nil {
		t.Fatalf("kill_session handler returned error: %v", err)
	}
	killResult := decodeCommandResult(t, sender.lastEnvelope(t))
	if killResult.Status != commandStatusSuccess {
		t.Fatalf("expected kill_session success, got %s (%s)", killResult.Status, killResult.Error)
	}
}

func TestHandleSessionCommandFailureIncludesEnvelopeFallbackID(t *testing.T) {
	adapter := NewMockOpencodeAdapter()
	sender := &captureCommandResultSender{}
	handler := HandleSessionCommand(adapter, sender, zap.NewNop())

	env := makeCommandEnvelope(t, map[string]interface{}{
		"type":   "session_status",
		"target": map[string]interface{}{"project": "proj-a"},
		"args":   map[string]interface{}{},
	})
	env.RequestID = "req-fallback-id"

	if err := handler(context.Background(), env); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	resultEnv := sender.lastEnvelope(t)
	if resultEnv.Type != string(shared.MessageTypeCommandResult) {
		t.Fatalf("expected envelope type %q, got %q", shared.MessageTypeCommandResult, resultEnv.Type)
	}
	if resultEnv.RequestID != "req-fallback-id" {
		t.Fatalf("expected fallback request_id req-fallback-id, got %q", resultEnv.RequestID)
	}

	result := decodeCommandResult(t, resultEnv)
	if result.Status != commandStatusFailure {
		t.Fatalf("expected failure status, got %s", result.Status)
	}
	if result.CommandID != "req-fallback-id" {
		t.Fatalf("expected command_id fallback to request_id, got %q", result.CommandID)
	}
}

func makeCommandEnvelope(t *testing.T, command map[string]interface{}) *shared.Envelope {
	t.Helper()
	payload, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("marshal command payload: %v", err)
	}
	return &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeCommand),
		RequestID: "req-test",
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}
}

func decodeCommandResult(t *testing.T, env *shared.Envelope) sessionCommandResult {
	t.Helper()
	var result sessionCommandResult
	if err := json.Unmarshal(env.Payload, &result); err != nil {
		t.Fatalf("unmarshal command result payload: %v", err)
	}
	return result
}
