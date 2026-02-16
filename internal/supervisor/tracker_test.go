package supervisor

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestTrackerPersistReload(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	if err := registry.Register(NodeEntry{ID: "node-3", Hostname: "agent-host-3"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	startedAt := time.Date(2026, 1, 10, 8, 30, 0, 0, time.UTC)
	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-3",
		NodeID:    "node-3",
		Project:   "gamma",
		Status:    SessionStatusRunning,
		TokenUsage: TokenUsage{
			Total: 101,
		},
		SessionCost: 3.14,
		StartedAt:   startedAt,
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	reloadedTracker := NewSessionTracker(db, logger)
	if err := reloadedTracker.LoadSessionsFromDB(); err != nil {
		t.Fatalf("load sessions failed: %v", err)
	}

	loaded, err := reloadedTracker.GetSession("sess-3")
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if loaded.Status != SessionStatusUnreachable {
		t.Fatalf("expected status unreachable after reload, got %s", loaded.Status)
	}

	if err := reloadedTracker.UpdateSession("sess-3", map[string]interface{}{
		"status":        string(SessionStatusRunning),
		"tokens":        202,
		"cost":          6.28,
		"last_activity": time.Now().UTC(),
	}); err != nil {
		t.Fatalf("update session failed: %v", err)
	}

	updated, err := reloadedTracker.GetSession("sess-3")
	if err != nil {
		t.Fatalf("get updated session failed: %v", err)
	}
	if updated.TokenUsage.Total != 202 {
		t.Fatalf("expected tokens 202, got %d", updated.TokenUsage.Total)
	}
	if updated.SessionCost != 6.28 {
		t.Fatalf("expected cost 6.28, got %f", updated.SessionCost)
	}
}

func TestTrackerCorruptedRow(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-good", Hostname: "host-good"}); err != nil {
		t.Fatalf("register good node failed: %v", err)
	}
	if err := registry.Register(NodeEntry{ID: "node-bad", Hostname: "host-bad"}); err != nil {
		t.Fatalf("register bad node failed: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO sessions (id, node_id, project, status, tokens, cost, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "sess-good", "node-good", "project-a", "running", 10, 1.0, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert good session failed: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO sessions (id, node_id, project, status, tokens, cost, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, "sess-bad", "node-bad", "project-b", "running", 99, 2.0, "{bad-json"); err != nil {
		t.Fatalf("insert corrupted session failed: %v", err)
	}

	tracker := NewSessionTracker(db, logger)
	if err := tracker.LoadSessionsFromDB(); err != nil {
		t.Fatalf("load sessions should not fail with corrupted rows: %v", err)
	}

	if tracker.RecoveryErrorCount() != 1 {
		t.Fatalf("expected 1 recovery error, got %d", tracker.RecoveryErrorCount())
	}

	if _, err := tracker.GetSession("sess-good"); err != nil {
		t.Fatalf("expected valid session loaded, got error: %v", err)
	}

	sessions := tracker.GetAllSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected only valid sessions in memory, got %d", len(sessions))
	}
}

func TestTrackerMarkUnreachableOnDisconnect(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	if err := registry.Register(NodeEntry{ID: "node-disconnect", Hostname: "host-disconnect"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-disconnect",
		NodeID:    "node-disconnect",
		Project:   "delta",
		Status:    SessionStatusIdle,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	if err := registry.MarkOffline("node-disconnect"); err != nil {
		t.Fatalf("mark offline failed: %v", err)
	}
	if err := tracker.MarkUnreachable("node-disconnect"); err != nil {
		t.Fatalf("mark unreachable failed: %v", err)
	}

	session, err := tracker.GetSession("sess-disconnect")
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if session.Status != SessionStatusUnreachable {
		t.Fatalf("expected unreachable status, got %s", session.Status)
	}
}
