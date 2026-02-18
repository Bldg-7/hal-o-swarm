package supervisor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestAuthStateIngest(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())
	if err := registry.Register(NodeEntry{ID: "node-auth", Hostname: "agent-host-auth"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	hub := newTestHub(context.Background(), 30*time.Second, 3)
	hub.ConfigureCredentialReconciliation(registry, 0)
	conn := &AgentConn{hub: hub, agentID: "node-auth", lastHeartbeat: time.Now()}

	reports := []shared.AuthStateReport{
		{
			Tool:      shared.ToolIdentifierOpenCode,
			Status:    shared.AuthStatusAuthenticated,
			CheckedAt: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
		},
		{
			Tool:      shared.ToolIdentifierClaudeCode,
			Status:    shared.AuthStatusUnauthenticated,
			Reason:    "login required",
			CheckedAt: time.Date(2026, 2, 1, 12, 0, 5, 0, time.UTC),
		},
	}
	payload, err := json.Marshal(reports)
	if err != nil {
		t.Fatalf("marshal auth_state payload: %v", err)
	}

	conn.handleEnvelope(&shared.Envelope{Type: string(shared.MessageTypeAuthState), Payload: payload})

	states := registry.GetAuthState("node-auth")
	if len(states) != 2 {
		t.Fatalf("expected 2 auth states, got %d", len(states))
	}

	if got := states[string(shared.ToolIdentifierOpenCode)]; got.Status != string(shared.AuthStatusAuthenticated) {
		t.Fatalf("expected opencode authenticated, got %s", got.Status)
	}
	claude := states[string(shared.ToolIdentifierClaudeCode)]
	if claude.Status != string(shared.AuthStatusUnauthenticated) {
		t.Fatalf("expected claude_code unauthenticated, got %s", claude.Status)
	}
	if claude.Reason != "login required" {
		t.Fatalf("expected claude reason preserved, got %q", claude.Reason)
	}
}

func TestAuthStateMalformed(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())
	if err := registry.Register(NodeEntry{ID: "node-auth", Hostname: "agent-host-auth"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	core, logs := observer.New(zap.WarnLevel)
	hub := NewHub(context.Background(), "test-token", nil, 30*time.Second, 3, zap.New(core))
	hub.ConfigureCredentialReconciliation(registry, 0)
	conn := &AgentConn{hub: hub, agentID: "node-auth", lastHeartbeat: time.Now()}

	conn.handleEnvelope(&shared.Envelope{Type: string(shared.MessageTypeAuthState), Payload: []byte("{")})

	states := registry.GetAuthState("node-auth")
	if len(states) != 0 {
		t.Fatalf("expected no auth state update on malformed payload, got %d entries", len(states))
	}

	warnLogs := logs.FilterMessage("auth state ingest failed").All()
	if len(warnLogs) == 0 {
		t.Fatal("expected warning log for malformed auth_state payload")
	}
}

func TestAuthStateUnknownNode(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())

	core, logs := observer.New(zap.WarnLevel)
	hub := NewHub(context.Background(), "test-token", nil, 30*time.Second, 3, zap.New(core))
	hub.ConfigureCredentialReconciliation(registry, 0)
	conn := &AgentConn{hub: hub, agentID: "node-missing", lastHeartbeat: time.Now()}

	reports := []shared.AuthStateReport{{
		Tool:      shared.ToolIdentifierOpenCode,
		Status:    shared.AuthStatusAuthenticated,
		CheckedAt: time.Now().UTC(),
	}}
	payload, err := json.Marshal(reports)
	if err != nil {
		t.Fatalf("marshal auth_state payload: %v", err)
	}

	conn.handleEnvelope(&shared.Envelope{Type: string(shared.MessageTypeAuthState), Payload: payload})

	warnLogs := logs.FilterMessage("auth state ingest failed").All()
	if len(warnLogs) == 0 {
		t.Fatal("expected warning log for unknown node auth_state payload")
	}

	if states := registry.GetAuthState("node-missing"); len(states) != 0 {
		t.Fatalf("expected unknown node auth state to remain empty, got %d entries", len(states))
	}
}
