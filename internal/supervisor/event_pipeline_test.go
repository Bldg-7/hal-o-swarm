package supervisor

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/storage"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	_ "modernc.org/sqlite"
)

func TestEventPipelineOrdered(t *testing.T) {
	db := setupPipelineTestDB(t)
	logger := zap.NewNop()

	requests := make([]RequestEventRange, 0)
	pipeline, err := NewEventPipeline(db, logger, func(agentID string, req RequestEventRange) error {
		requests = append(requests, req)
		return nil
	})
	if err != nil {
		t.Fatalf("create event pipeline: %v", err)
	}
	defer pipeline.Close()

	for seq := uint64(1); seq <= 100; seq++ {
		err := pipeline.ProcessEvent("agent-1", Event{
			ID:        eventID(seq),
			SessionID: "session-1",
			Type:      "tool.execute.after",
			Data:      json.RawMessage(`{"ok":true}`),
			Timestamp: time.Now().UTC(),
			Seq:       seq,
		})
		if err != nil {
			t.Fatalf("process event seq=%d: %v", seq, err)
		}
	}

	waitForEventCount(t, db, 100)

	if got := pipeline.LastSequence("agent-1"); got != 100 {
		t.Fatalf("last sequence = %d, want 100", got)
	}
	if len(requests) != 0 {
		t.Fatalf("unexpected replay requests: %d", len(requests))
	}
}

func TestEventPipelineDedup(t *testing.T) {
	db := setupPipelineTestDB(t)
	logger := zap.NewNop()

	pipeline, err := NewEventPipeline(db, logger, nil)
	if err != nil {
		t.Fatalf("create event pipeline: %v", err)
	}
	defer pipeline.Close()

	event := Event{
		ID:        "dup-event",
		SessionID: "session-1",
		Type:      "session.idle",
		Data:      json.RawMessage(`{"idle":false}`),
		Timestamp: time.Now().UTC(),
		Seq:       1,
	}

	if err := pipeline.ProcessEvent("agent-1", event); err != nil {
		t.Fatalf("first process failed: %v", err)
	}
	if err := pipeline.ProcessEvent("agent-1", event); err != nil {
		t.Fatalf("second process failed: %v", err)
	}

	waitForEventCount(t, db, 1)

	if got := pipeline.LastSequence("agent-1"); got != 1 {
		t.Fatalf("last sequence = %d, want 1", got)
	}
}

func TestEventPipelineGap(t *testing.T) {
	db := setupPipelineTestDB(t)
	core, logs := observer.New(zap.WarnLevel)
	logger := zap.New(core)

	requests := make([]RequestEventRange, 0)
	pipeline, err := NewEventPipeline(db, logger, func(agentID string, req RequestEventRange) error {
		requests = append(requests, req)
		return nil
	})
	if err != nil {
		t.Fatalf("create event pipeline: %v", err)
	}
	defer pipeline.Close()

	for _, seq := range []uint64{1, 2, 4} {
		err := pipeline.ProcessEvent("agent-1", Event{
			ID:        eventID(seq),
			SessionID: "session-1",
			Type:      "tool.execute.after",
			Data:      json.RawMessage(`{"seq":true}`),
			Timestamp: time.Now().UTC(),
			Seq:       seq,
		})
		if err != nil {
			t.Fatalf("process seq=%d: %v", seq, err)
		}
	}

	waitForEventCount(t, db, 2)

	if got := pipeline.LastSequence("agent-1"); got != 2 {
		t.Fatalf("last sequence = %d, want 2", got)
	}
	if got := pipeline.PendingCount("agent-1"); got != 1 {
		t.Fatalf("pending count = %d, want 1", got)
	}
	if len(requests) != 1 {
		t.Fatalf("replay request count = %d, want 1", len(requests))
	}
	if requests[0].From != 3 || requests[0].To != 3 {
		t.Fatalf("replay request = %+v, want from=3 to=3", requests[0])
	}

	gapLogs := logs.FilterMessage("event sequence gap detected").All()
	if len(gapLogs) == 0 {
		t.Fatal("expected gap warning log")
	}

	err = pipeline.ProcessEvent("agent-1", Event{
		ID:        eventID(3),
		SessionID: "session-1",
		Type:      "tool.execute.after",
		Data:      json.RawMessage(`{"seq":true}`),
		Timestamp: time.Now().UTC(),
		Seq:       3,
	})
	if err != nil {
		t.Fatalf("process seq=3: %v", err)
	}

	waitForEventCount(t, db, 4)

	if got := pipeline.LastSequence("agent-1"); got != 4 {
		t.Fatalf("last sequence = %d, want 4", got)
	}
	if got := pipeline.PendingCount("agent-1"); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
}

func setupPipelineTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "event-pipeline-*.db")
	if err != nil {
		t.Fatalf("create temp db file: %v", err)
	}
	tmpfile.Close()

	db, err := sql.Open("sqlite", tmpfile.Name())
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	runner := storage.NewMigrationRunner(db)
	if err := runner.Migrate(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO nodes (id, hostname, status) VALUES ('node-1', 'node-1.local', 'connected')`); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, node_id, project, status) VALUES ('session-1', 'node-1', 'demo', 'running')`); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		os.Remove(tmpfile.Name())
	})

	return db
}

func waitForEventCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := countEvents(t, db); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := countEvents(t, db)
	t.Fatalf("event count = %d, want %d", got, want)
}

func countEvents(t *testing.T, db *sql.DB) int {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return count
}

func eventID(seq uint64) string {
	return fmt.Sprintf("event-%d", seq)
}
