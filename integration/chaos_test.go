package integration

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"github.com/Bldg-7/hal-o-swarm/internal/supervisor"
	"go.uber.org/zap"
)

func TestNetworkPartitionRecovery(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-partition", []string{"proj-partition"}, nil)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	created := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-partition"},
		Args: map[string]interface{}{
			"prompt": "state to keep",
		},
	})
	if created.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("create failed: %s", created.Error)
	}
	sessionID := created.Output

	beforeSnapshotCalls := ag.snapshotCall.Load()
	h.closeNodeConnection(ag.nodeID)

	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOffline, 3*time.Second)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 5*time.Second)
	if ag.snapshotCall.Load() <= beforeSnapshotCalls {
		t.Fatalf("expected snapshot provider to run on reconnect (before=%d after=%d)", beforeSnapshotCalls, ag.snapshotCall.Load())
	}

	status := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeSessionStatus,
		Target: supervisor.CommandTarget{Project: "proj-partition"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if status.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("session state not recovered after partition: %s", status.Error)
	}
}

func TestSupervisorCrashRecovery(t *testing.T) {
	db := setupIntegrationDB(t)
	h1 := newSupervisorHarnessWithDB(t, db, false)
	ag1 := newAgentHarness(t, h1, "agent-supervisor-crash", []string{"proj-crash"}, nil)
	h1.waitForNodeStatus(t, ag1.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	created := h1.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-crash"},
		Args: map[string]interface{}{
			"prompt": "persist me",
		},
	})
	if created.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("create failed: %s", created.Error)
	}
	sessionID := created.Output

	ag1.stop()
	h1.stop()

	h2 := newSupervisorHarnessWithDB(t, db, true)
	t.Cleanup(h2.stop)
	ag2 := newAgentHarness(t, h2, "agent-supervisor-crash", []string{"proj-crash"}, ag1.adapter)
	h2.waitForNodeStatus(t, ag2.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	h2.waitForSession(t, sessionID, 3*time.Second)
	status := h2.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeSessionStatus,
		Target: supervisor.CommandTarget{Project: "proj-crash"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if status.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("expected recovered session status success, got %s (%s)", status.Status, status.Error)
	}
}

func TestAgentCrashRecovery(t *testing.T) {
	h := newSupervisorHarness(t)
	ag1 := newAgentHarness(t, h, "agent-crash", []string{"proj-agent-crash"}, nil)
	h.waitForNodeStatus(t, ag1.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	created := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-agent-crash"},
		Args: map[string]interface{}{
			"prompt": "survive restart",
		},
	})
	sessionID := created.Output

	ag1.stop()
	h.waitForNodeStatus(t, ag1.nodeID, supervisor.NodeStatusOffline, 3*time.Second)

	ag2 := newAgentHarness(t, h, "agent-crash", []string{"proj-agent-crash"}, ag1.adapter)
	h.waitForNodeStatus(t, ag2.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	status := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeSessionStatus,
		Target: supervisor.CommandTarget{Project: "proj-agent-crash"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if status.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("expected status success after agent restart, got %s (%s)", status.Status, status.Error)
	}
}

func TestHeartbeatTimeout(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-heartbeat", []string{"proj-heartbeat"}, nil)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	ag.setHeartbeatPaused(true)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOffline, 3*time.Second)
}

func TestEventOrderingUnderLoad(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-order", []string{"proj-order"}, nil)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	created := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-order"},
		Args: map[string]interface{}{
			"prompt": "load-test",
		},
	})
	if created.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("create failed: %s", created.Error)
	}
	if err := h.tracker.AddSession(supervisor.TrackedSession{
		SessionID:    created.Output,
		NodeID:       ag.nodeID,
		Project:      "proj-order",
		Status:       supervisor.SessionStatusRunning,
		LastActivity: time.Now().UTC(),
		StartedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("track session: %v", err)
	}

	ag.emitEvents(1000, created.Output)

	waitFor(t, 12*time.Second, func() bool {
		ids := h.allEventIDs()
		count := 0
		for _, id := range ids {
			if strings.HasPrefix(id, ag.nodeID+"-") {
				count++
			}
		}
		return count >= 1000
	}, "1000 persisted events")

	ids := h.allEventIDs()
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !strings.HasPrefix(id, ag.nodeID+"-") {
			continue
		}
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate event id detected: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestPolicyEngineIntegration(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-policy", []string{"proj-policy"}, nil)
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	idle := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-policy"},
	})
	running := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-policy"},
		Args: map[string]interface{}{
			"prompt": "work",
		},
	})

	now := time.Now().UTC()
	if err := h.tracker.AddSession(supervisor.TrackedSession{
		SessionID:    idle.Output,
		NodeID:       ag.nodeID,
		Project:      "proj-policy",
		Status:       supervisor.SessionStatusIdle,
		LastActivity: now.Add(-10 * time.Second),
		StartedAt:    now,
	}); err != nil {
		t.Fatalf("track idle session: %v", err)
	}
	if err := h.tracker.AddSession(supervisor.TrackedSession{
		SessionID:    running.Output,
		NodeID:       ag.nodeID,
		Project:      "proj-policy",
		Status:       supervisor.SessionStatusRunning,
		TokenUsage:   supervisor.TokenUsage{Total: 9999},
		LastActivity: now,
		StartedAt:    now,
	}); err != nil {
		t.Fatalf("track running session: %v", err)
	}

	policy := supervisor.NewPolicyEngine(config.PolicyConfig{
		CheckIntervalSec: 1,
		ResumeOnIdle: config.IdlePolicyConfig{
			Enabled:           true,
			IdleThresholdSec:  1,
			MaxRetries:        2,
			RetryResetSeconds: 60,
		},
		RestartOnCompaction: config.CompactionPolicyConfig{
			Enabled:           true,
			TokenThreshold:    100,
			MaxRetries:        2,
			RetryResetSeconds: 60,
		},
	}, h.tracker, h.dispatcher, h.pipeline)
	policy.Start()
	defer policy.Stop()

	waitFor(t, 3*time.Second, func() bool {
		status := h.dispatch(t, supervisor.Command{
			Type:   supervisor.CommandTypeSessionStatus,
			Target: supervisor.CommandTarget{Project: "proj-policy"},
			Args: map[string]interface{}{
				"session_id": idle.Output,
			},
		})
		return status.Status == supervisor.CommandStatusSuccess && status.Output == "running"
	}, "policy resume-on-idle")

	waitFor(t, 3*time.Second, func() bool {
		var count int
		err := h.db.QueryRow(`SELECT COUNT(*) FROM events WHERE type = 'policy.action'`).Scan(&count)
		return err == nil && count > 0
	}, "policy action events persisted")
}

func TestDatabaseCorruptionRecovery(t *testing.T) {
	db := setupIntegrationDB(t)
	seedCorruptedRows(t, db)

	registry := supervisor.NewNodeRegistry(db, zap.NewNop())
	tracker := supervisor.NewSessionTracker(db, zap.NewNop())

	if err := registry.LoadNodesFromDB(); err != nil {
		t.Fatalf("load nodes with corruption: %v", err)
	}
	if err := tracker.LoadSessionsFromDB(); err != nil {
		t.Fatalf("load sessions with corruption: %v", err)
	}
	if registry.RecoveryErrorCount() == 0 {
		t.Fatal("expected node recovery errors for corrupted rows")
	}
	if tracker.RecoveryErrorCount() == 0 {
		t.Fatal("expected session recovery errors for corrupted rows")
	}
}

func seedCorruptedRows(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO nodes (id, hostname, status, last_heartbeat, connected_at) VALUES
		('node-valid', 'host-valid', 'online', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'),
		('node-bad', 'host-bad', 'online', 'not-a-timestamp', '2026-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed nodes: %v", err)
	}

	_, err = db.Exec(`INSERT INTO sessions (id, node_id, project, status, tokens, cost, started_at) VALUES
		('sess-valid', 'node-valid', 'proj', 'running', 1, 0.1, '2026-01-01T00:00:00Z'),
		('sess-bad', 'node-valid', 'proj', 'running', 2, 0.2, 'bad-time')`)
	if err != nil {
		t.Fatalf("seed sessions: %v", err)
	}
}
