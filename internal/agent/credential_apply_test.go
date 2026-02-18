package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCredentialApplySetsRuntimeEnv(t *testing.T) {
	applier := NewCredentialApplier(zap.NewNop())
	handler := HandleCredentialPush(applier)

	env := credentialPushEnvelope(t, "node-1", map[string]string{
		"OPENAI_API_KEY": "key-v1",
		"ANTHROPIC_KEY":  "ant-v1",
	}, 1)

	if err := handler(context.Background(), env); err != nil {
		t.Fatalf("HandleCredentialPush failed: %v", err)
	}

	got := applier.GetEnv()
	if got["OPENAI_API_KEY"] != "key-v1" {
		t.Fatalf("OPENAI_API_KEY mismatch: got %q", got["OPENAI_API_KEY"])
	}
	if got["ANTHROPIC_KEY"] != "ant-v1" {
		t.Fatalf("ANTHROPIC_KEY mismatch: got %q", got["ANTHROPIC_KEY"])
	}
	if applier.GetVersion() != 1 {
		t.Fatalf("version mismatch: got %d want %d", applier.GetVersion(), 1)
	}

	got["OPENAI_API_KEY"] = "mutated"
	if applier.GetEnv()["OPENAI_API_KEY"] != "key-v1" {
		t.Fatal("GetEnv returned aliased map; expected copy")
	}
}

func TestCredentialApplyTracksVersion(t *testing.T) {
	applier := NewCredentialApplier(zap.NewNop())

	if err := applier.Apply(shared.CredentialPushPayload{
		TargetNode: "node-1",
		EnvVars: map[string]string{
			"OPENAI_API_KEY": "key-v1",
		},
		Version: 1,
	}); err != nil {
		t.Fatalf("Apply v1 failed: %v", err)
	}

	if err := applier.Apply(shared.CredentialPushPayload{
		TargetNode: "node-1",
		EnvVars: map[string]string{
			"OPENAI_API_KEY": "key-v2",
		},
		Version: 2,
	}); err != nil {
		t.Fatalf("Apply v2 failed: %v", err)
	}

	if applier.GetVersion() != 2 {
		t.Fatalf("expected version 2, got %d", applier.GetVersion())
	}
	if applier.GetEnv()["OPENAI_API_KEY"] != "key-v2" {
		t.Fatalf("expected latest env value key-v2, got %q", applier.GetEnv()["OPENAI_API_KEY"])
	}
}

func TestCredentialApplyMalformedPayload(t *testing.T) {
	applier := NewCredentialApplier(zap.NewNop())
	handler := HandleCredentialPush(applier)

	tests := []struct {
		name     string
		envelope *shared.Envelope
	}{
		{
			name: "invalid json",
			envelope: &shared.Envelope{
				Payload: []byte("{"),
			},
		},
		{
			name: "missing required fields",
			envelope: envelopeWithPayload(t, map[string]interface{}{
				"type": "credential_push",
				"args": map[string]interface{}{},
			}),
		},
		{
			name: "wrong command type",
			envelope: envelopeWithPayload(t, map[string]interface{}{
				"type": "kill_session",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked: %v", r)
				}
			}()

			err := handler(context.Background(), tt.envelope)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestCredentialApplyMasksSecrets(t *testing.T) {
	core, observedLogs := observer.New(zap.InfoLevel)
	applier := NewCredentialApplier(zap.New(core))
	handler := HandleCredentialPush(applier)

	env := credentialPushEnvelope(t, "node-1", map[string]string{
		"OPENAI_API_KEY": "super-secret-key",
	}, 3)

	if err := handler(context.Background(), env); err != nil {
		t.Fatalf("HandleCredentialPush failed: %v", err)
	}

	if got := applier.MaskValue("super-secret-key"); got != "[REDACTED]" {
		t.Fatalf("MaskValue failed for credential value: got %q", got)
	}
	if got := applier.MaskValue("not-a-secret"); got != "not-a-secret" {
		t.Fatalf("MaskValue should preserve non-secret value: got %q", got)
	}

	entries := observedLogs.All()
	if len(entries) == 0 {
		t.Fatal("expected at least one log entry")
	}

	ctx := entries[0].ContextMap()
	serialized := fmt.Sprintf("%v", ctx["env_vars"])
	if strings.Contains(serialized, "super-secret-key") {
		t.Fatalf("log contains unmasked secret: %s", serialized)
	}
	if !strings.Contains(serialized, "[REDACTED]") {
		t.Fatalf("log does not contain redaction marker: %s", serialized)
	}
}

func TestCredentialApplyConcurrentAccess(t *testing.T) {
	applier := NewCredentialApplier(zap.NewNop())

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				version := i*100 + j
				err := applier.Apply(shared.CredentialPushPayload{
					TargetNode: "node-1",
					EnvVars: map[string]string{
						"TOKEN": fmt.Sprintf("token-%d", version),
					},
					Version: version,
				})
				if err != nil {
					t.Errorf("Apply failed: %v", err)
				}
			}
		}(i)
	}

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = applier.GetEnv()
				_ = applier.GetVersion()
			}
		}()
	}

	wg.Wait()
	if len(applier.GetEnv()) == 0 {
		t.Fatal("expected env vars after concurrent apply")
	}
}

func TestCredentialApplierRejectsDuplicate(t *testing.T) {
	applier := NewCredentialApplier(zap.NewNop())
	handler := HandleCredentialPush(applier)

	first := envelopeWithPayload(t, map[string]interface{}{
		"command_id": "cmd-1",
		"type":       "credential_push",
		"target": map[string]interface{}{
			"node_id": "node-1",
		},
		"args": map[string]interface{}{
			"env_vars": map[string]string{
				"OPENAI_API_KEY": "key-v1",
			},
			"version": 1,
		},
	})

	if err := handler(context.Background(), first); err != nil {
		t.Fatalf("first HandleCredentialPush failed: %v", err)
	}

	duplicate := envelopeWithPayload(t, map[string]interface{}{
		"command_id": "cmd-1",
		"type":       "credential_push",
		"target": map[string]interface{}{
			"node_id": "node-1",
		},
		"args": map[string]interface{}{
			"env_vars": map[string]string{
				"OPENAI_API_KEY": "key-v2",
			},
			"version": 2,
		},
	})

	if err := handler(context.Background(), duplicate); err != nil {
		t.Fatalf("duplicate HandleCredentialPush should be no-op, got error: %v", err)
	}

	if got := applier.GetVersion(); got != 1 {
		t.Fatalf("expected version to remain at first apply, got %d", got)
	}
	if got := applier.GetEnv()["OPENAI_API_KEY"]; got != "key-v1" {
		t.Fatalf("expected env var to remain from first apply, got %q", got)
	}
}

func TestRegisterCredentialPushHandler(t *testing.T) {
	client := NewWSClient("ws://localhost:8420", "token", zap.NewNop())
	applier := NewCredentialApplier(zap.NewNop())

	if err := RegisterCredentialPushHandler(client, applier); err != nil {
		t.Fatalf("RegisterCredentialPushHandler failed: %v", err)
	}

	client.commandMu.RLock()
	_, ok := client.commandHandlers["credential_push"]
	client.commandMu.RUnlock()

	if !ok {
		t.Fatal("credential_push handler was not registered")
	}
}

func TestRegisterCredentialSyncOnReconnect(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	client := NewWSClient(mock.URL(), "token", zap.NewNop(),
		WithBackoff(fastTestBackoff()),
	)
	applier := NewCredentialApplier(zap.NewNop())
	if err := applier.Apply(shared.CredentialPushPayload{
		TargetNode: "node-1",
		EnvVars: map[string]string{
			"OPENAI_API_KEY": "key-v1",
		},
		Version: 1,
	}); err != nil {
		t.Fatalf("seed credential version failed: %v", err)
	}

	if err := RegisterCredentialSyncOnReconnect(client, applier, "node-1"); err != nil {
		t.Fatalf("RegisterCredentialSyncOnReconnect failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	var conn1 interface{ Close() error }
	select {
	case c := <-mock.connCh:
		conn1 = c
	case <-ctx.Done():
		t.Fatal("timed out waiting for first connection")
	}

	time.Sleep(120 * time.Millisecond)
	_ = conn1.Close()

	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect")
	}

	time.Sleep(250 * time.Millisecond)

	msgs := mock.GetMessages()
	credentialSyncCount := 0
	for _, msg := range msgs {
		env, err := shared.UnmarshalEnvelope(msg)
		if err != nil {
			continue
		}
		if env.Type != string(shared.MessageTypeCredentialSync) {
			continue
		}
		credentialSyncCount++

		var payload CredentialVersionReport
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			t.Fatalf("unmarshal credential_sync payload: %v", err)
		}
		if payload.NodeID != "node-1" {
			t.Fatalf("expected node_id node-1, got %q", payload.NodeID)
		}
		if payload.CredentialVersion != 1 {
			t.Fatalf("expected credential_version 1, got %d", payload.CredentialVersion)
		}
	}

	if credentialSyncCount != 2 {
		t.Fatalf("expected credential_sync to be sent on connect and reconnect, got %d", credentialSyncCount)
	}
}

func credentialPushEnvelope(t *testing.T, targetNode string, envVars map[string]string, version int) *shared.Envelope {
	t.Helper()
	return envelopeWithPayload(t, map[string]interface{}{
		"type": "credential_push",
		"target": map[string]interface{}{
			"node_id": targetNode,
		},
		"args": map[string]interface{}{
			"env_vars": envVars,
			"version":  version,
		},
	})
}

func envelopeWithPayload(t *testing.T, payload interface{}) *shared.Envelope {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return &shared.Envelope{Payload: data}
}
