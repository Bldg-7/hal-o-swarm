package supervisor

import (
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/storage"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

func TestRegistryPersistReload(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	connectedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := registry.Register(NodeEntry{
		ID:           "node-1",
		Hostname:     "agent-host-1",
		Projects:     []string{"alpha", "beta"},
		Capabilities: []string{"exec", "stream"},
		ConnectedAt:  connectedAt,
	}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-1",
		NodeID:    "node-1",
		Project:   "alpha",
		Status:    SessionStatusRunning,
		TokenUsage: TokenUsage{
			Total: 42,
		},
		SessionCost: 1.25,
		StartedAt:   connectedAt,
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	reloadedRegistry := NewNodeRegistry(db, logger)
	reloadedTracker := NewSessionTracker(db, logger)

	if err := reloadedRegistry.LoadNodesFromDB(); err != nil {
		t.Fatalf("load nodes failed: %v", err)
	}
	if err := reloadedTracker.LoadSessionsFromDB(); err != nil {
		t.Fatalf("load sessions failed: %v", err)
	}

	node, err := reloadedRegistry.GetNode("node-1")
	if err != nil {
		t.Fatalf("get node failed: %v", err)
	}
	if node.Status != NodeStatusOffline {
		t.Fatalf("expected node offline after reload, got %s", node.Status)
	}

	session, err := reloadedTracker.GetSession("sess-1")
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if session.Status != SessionStatusUnreachable {
		t.Fatalf("expected session unreachable after reload, got %s", session.Status)
	}

	if err := reloadedRegistry.Register(NodeEntry{ID: "node-1", Hostname: "agent-host-1"}); err != nil {
		t.Fatalf("re-register node failed: %v", err)
	}
	if err := reloadedTracker.RestoreFromSnapshot("node-1", []TrackedSession{{
		SessionID: "sess-1",
		Project:   "alpha",
		Status:    SessionStatusIdle,
		TokenUsage: TokenUsage{
			Total: 64,
		},
		SessionCost: 2.5,
		StartedAt:   connectedAt,
	}}); err != nil {
		t.Fatalf("restore snapshot failed: %v", err)
	}

	nodeAfterReconnect, err := reloadedRegistry.GetNode("node-1")
	if err != nil {
		t.Fatalf("get reconnected node failed: %v", err)
	}
	if nodeAfterReconnect.Status != NodeStatusOnline {
		t.Fatalf("expected node online after reconnect, got %s", nodeAfterReconnect.Status)
	}

	sessionAfterReconnect, err := reloadedTracker.GetSession("sess-1")
	if err != nil {
		t.Fatalf("get reconnected session failed: %v", err)
	}
	if sessionAfterReconnect.Status != SessionStatusIdle {
		t.Fatalf("expected session idle after reconnect, got %s", sessionAfterReconnect.Status)
	}
}

func TestRegistryHeartbeatTimeoutMarksOffline(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())

	if err := registry.Register(NodeEntry{ID: "node-2", Hostname: "agent-host-2"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	if err := registry.MarkOffline("node-2"); err != nil {
		t.Fatalf("mark offline failed: %v", err)
	}

	node, err := registry.GetNode("node-2")
	if err != nil {
		t.Fatalf("get node failed: %v", err)
	}
	if node.Status != NodeStatusOffline {
		t.Fatalf("expected offline status, got %s", node.Status)
	}
}

func TestRegistryAuthSummary(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())

	if err := registry.Register(NodeEntry{ID: "node-3", Hostname: "agent-host-3"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	authStates := map[string]NodeAuthState{
		"github": {
			Tool:      "github",
			Status:    "authenticated",
			Reason:    "",
			CheckedAt: time.Now().UTC(),
		},
		"anthropic": {
			Tool:      "anthropic",
			Status:    "unauthenticated",
			Reason:    "missing API key",
			CheckedAt: time.Now().UTC(),
		},
		"docker": {
			Tool:      "docker",
			Status:    "not_installed",
			Reason:    "docker binary not found",
			CheckedAt: time.Now().UTC(),
		},
	}

	if err := registry.UpdateAuthState("node-3", authStates); err != nil {
		t.Fatalf("update auth state failed: %v", err)
	}

	retrieved := registry.GetAuthState("node-3")
	if len(retrieved) != 3 {
		t.Fatalf("expected 3 auth states, got %d", len(retrieved))
	}

	if retrieved["github"].Status != "authenticated" {
		t.Fatalf("expected github authenticated, got %s", retrieved["github"].Status)
	}
	if retrieved["anthropic"].Status != "unauthenticated" {
		t.Fatalf("expected anthropic unauthenticated, got %s", retrieved["anthropic"].Status)
	}
	if retrieved["docker"].Status != "not_installed" {
		t.Fatalf("expected docker not_installed, got %s", retrieved["docker"].Status)
	}

	node, err := registry.GetNode("node-3")
	if err != nil {
		t.Fatalf("get node failed: %v", err)
	}
	if node.AuthStates == nil || len(node.AuthStates) != 3 {
		t.Fatalf("expected node to have 3 auth states, got %d", len(node.AuthStates))
	}
	if node.AuthUpdatedAt.IsZero() {
		t.Fatalf("expected AuthUpdatedAt to be set")
	}
}

func TestRegistryNoSecretStore(t *testing.T) {
	forbiddenFields := map[string]bool{
		"key":      true,
		"token":    true,
		"secret":   true,
		"password": true,
		"apikey":   true,
		"api_key":  true,
	}

	authStateType := reflect.TypeOf((*NodeAuthState)(nil)).Elem()
	for i := 0; i < authStateType.NumField(); i++ {
		field := authStateType.Field(i)
		fieldNameLower := strings.ToLower(field.Name)
		if forbiddenFields[fieldNameLower] {
			t.Fatalf("NodeAuthState contains forbidden field: %s", field.Name)
		}
	}

	nodeEntryType := reflect.TypeOf((*NodeEntry)(nil)).Elem()
	for i := 0; i < nodeEntryType.NumField(); i++ {
		field := nodeEntryType.Field(i)
		if field.Name == "AuthStates" {
			authStateMapType := field.Type.Elem()
			for j := 0; j < authStateMapType.NumField(); j++ {
				subField := authStateMapType.Field(j)
				subFieldNameLower := strings.ToLower(subField.Name)
				if forbiddenFields[subFieldNameLower] {
					t.Fatalf("NodeAuthState (in NodeEntry) contains forbidden field: %s", subField.Name)
				}
			}
		}
	}
}

func TestReconnectCredentialReconciliation(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())

	if err := registry.Register(NodeEntry{ID: "node-reconnect", Hostname: "agent-host-reconnect"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}
	if err := registry.MarkOffline("node-reconnect"); err != nil {
		t.Fatalf("mark offline failed: %v", err)
	}
	if err := registry.Register(NodeEntry{ID: "node-reconnect", Hostname: "agent-host-reconnect"}); err != nil {
		t.Fatalf("re-register node failed: %v", err)
	}

	nodeBefore, err := registry.GetNode("node-reconnect")
	if err != nil {
		t.Fatalf("get node before reconcile failed: %v", err)
	}
	if nodeBefore.CredSyncStatus != CredentialSyncStatusUnknown {
		t.Fatalf("expected pre-report status unknown, got %s", nodeBefore.CredSyncStatus)
	}

	payload, err := json.Marshal(CredentialSyncPayload{NodeID: "node-reconnect", CredentialVersion: 1})
	if err != nil {
		t.Fatalf("marshal credential_sync payload: %v", err)
	}
	if err := registry.HandleCredentialSyncMessage(payload, 1); err != nil {
		t.Fatalf("handle credential_sync failed: %v", err)
	}

	nodeAfter, err := registry.GetNode("node-reconnect")
	if err != nil {
		t.Fatalf("get node after reconcile failed: %v", err)
	}
	if nodeAfter.CredSyncStatus != CredentialSyncStatusInSync {
		t.Fatalf("expected in_sync status, got %s", nodeAfter.CredSyncStatus)
	}
	if nodeAfter.CredVersion != 1 {
		t.Fatalf("expected credential version 1, got %d", nodeAfter.CredVersion)
	}
}

func TestReconnectCredentialDrift(t *testing.T) {
	db := setupSupervisorTestDB(t)
	registry := NewNodeRegistry(db, zap.NewNop())

	if err := registry.Register(NodeEntry{ID: "node-drift", Hostname: "agent-host-drift"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	payload, err := json.Marshal(CredentialSyncPayload{NodeID: "node-drift", CredentialVersion: 1})
	if err != nil {
		t.Fatalf("marshal credential_sync payload: %v", err)
	}
	if err := registry.HandleCredentialSyncMessage(payload, 2); err != nil {
		t.Fatalf("handle credential_sync failed: %v", err)
	}

	node, err := registry.GetNode("node-drift")
	if err != nil {
		t.Fatalf("get node failed: %v", err)
	}
	if node.CredSyncStatus != CredentialSyncStatusDriftDetected {
		t.Fatalf("expected drift_detected status, got %s", node.CredSyncStatus)
	}
	if node.CredVersion != 1 {
		t.Fatalf("expected reported credential version 1, got %d", node.CredVersion)
	}
}

func setupSupervisorTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "supervisor-*.db")
	if err != nil {
		t.Fatalf("create temp db failed: %v", err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatalf("close temp db file failed: %v", err)
	}

	db, err := sql.Open("sqlite", tmpfile.Name())
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(tmpfile.Name())
	})

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys failed: %v", err)
	}

	runner := storage.NewMigrationRunner(db)
	if err := runner.Migrate(); err != nil {
		t.Fatalf("migrate test db failed: %v", err)
	}

	return db
}
