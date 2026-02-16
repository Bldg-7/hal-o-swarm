package supervisor

import (
	"database/sql"
	"os"
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
