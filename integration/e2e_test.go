package integration

import (
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/supervisor"
)

func TestSingleNodeLifecycle(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-single", []string{"proj-single"}, nil)

	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	create := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-single"},
		Args: map[string]interface{}{
			"prompt": "start lifecycle",
		},
	})
	if create.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("create session failed: %s", create.Error)
	}
	sessionID := create.Output
	if sessionID == "" {
		t.Fatal("expected session ID from create_session")
	}

	if err := h.tracker.AddSession(supervisor.TrackedSession{
		SessionID:    sessionID,
		NodeID:       ag.nodeID,
		Project:      "proj-single",
		Status:       supervisor.SessionStatusRunning,
		LastActivity: time.Now().UTC(),
		StartedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("track session: %v", err)
	}

	prompt := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypePromptSession,
		Target: supervisor.CommandTarget{Project: "proj-single"},
		Args: map[string]interface{}{
			"session_id": sessionID,
			"message":    "continue",
		},
	})
	if prompt.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("prompt failed: %s", prompt.Error)
	}

	status := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeSessionStatus,
		Target: supervisor.CommandTarget{Project: "proj-single"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if status.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("status failed: %s", status.Error)
	}
	if status.Output != "running" {
		t.Fatalf("expected running status, got %q", status.Output)
	}

	kill := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeKillSession,
		Target: supervisor.CommandTarget{Project: "proj-single"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if kill.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("kill failed: %s", kill.Error)
	}

	ag.stop()
	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOffline, 3*time.Second)
}

func TestMultiNodeLifecycle(t *testing.T) {
	h := newSupervisorHarness(t)
	agents := []*agentHarness{
		newAgentHarness(t, h, "agent-a", []string{"proj-a"}, nil),
		newAgentHarness(t, h, "agent-b", []string{"proj-b"}, nil),
		newAgentHarness(t, h, "agent-c", []string{"proj-c"}, nil),
	}

	h.waitForNodeCount(t, 3, 3*time.Second)
	sessionIDs := map[string]string{}

	for idx, project := range []string{"proj-a", "proj-b", "proj-c"} {
		result := h.dispatch(t, supervisor.Command{
			Type:   supervisor.CommandTypeCreateSession,
			Target: supervisor.CommandTarget{Project: project},
			Args: map[string]interface{}{
				"prompt": "fanout",
			},
		})
		if result.Status != supervisor.CommandStatusSuccess {
			t.Fatalf("create on %s failed: %s", project, result.Error)
		}
		sessionIDs[project] = result.Output

		if err := h.tracker.AddSession(supervisor.TrackedSession{
			SessionID:    result.Output,
			NodeID:       agents[idx].nodeID,
			Project:      project,
			Status:       supervisor.SessionStatusRunning,
			LastActivity: time.Now().UTC(),
			StartedAt:    time.Now().UTC(),
		}); err != nil {
			t.Fatalf("track session %s: %v", result.Output, err)
		}
	}

	for idx, project := range []string{"proj-a", "proj-b", "proj-c"} {
		status := h.dispatch(t, supervisor.Command{
			Type:   supervisor.CommandTypeSessionStatus,
			Target: supervisor.CommandTarget{Project: project},
			Args: map[string]interface{}{
				"session_id": sessionIDs[project],
			},
		})
		if status.Status != supervisor.CommandStatusSuccess {
			t.Fatalf("status on %s failed: %s", project, status.Error)
		}
		agents[idx].stop()
	}

	for _, ag := range agents {
		h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOffline, 3*time.Second)
	}
}

func TestSessionLifecycle(t *testing.T) {
	h := newSupervisorHarness(t)
	ag := newAgentHarness(t, h, "agent-session", []string{"proj-session"}, nil)

	h.waitForNodeStatus(t, ag.nodeID, supervisor.NodeStatusOnline, 3*time.Second)

	create := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeCreateSession,
		Target: supervisor.CommandTarget{Project: "proj-session"},
		Args: map[string]interface{}{
			"prompt": "boot",
		},
	})
	if create.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("create failed: %s", create.Error)
	}

	sessionID := create.Output
	if err := h.tracker.AddSession(supervisor.TrackedSession{
		SessionID:    sessionID,
		NodeID:       ag.nodeID,
		Project:      "proj-session",
		Status:       supervisor.SessionStatusRunning,
		LastActivity: time.Now().UTC().Add(-2 * time.Second),
		StartedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("track session: %v", err)
	}

	prompt := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypePromptSession,
		Target: supervisor.CommandTarget{Project: "proj-session"},
		Args: map[string]interface{}{
			"session_id": sessionID,
			"message":    "work on task",
		},
	})
	if prompt.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("prompt failed: %s", prompt.Error)
	}

	kill := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeKillSession,
		Target: supervisor.CommandTarget{Project: "proj-session"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if kill.Status != supervisor.CommandStatusSuccess {
		t.Fatalf("kill failed: %s", kill.Error)
	}

	status := h.dispatch(t, supervisor.Command{
		Type:   supervisor.CommandTypeSessionStatus,
		Target: supervisor.CommandTarget{Project: "proj-session"},
		Args: map[string]interface{}{
			"session_id": sessionID,
		},
	})
	if status.Status != supervisor.CommandStatusFailure {
		t.Fatalf("expected failure for deleted session, got %s", status.Status)
	}
}
