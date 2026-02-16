package supervisor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type mockCommandTransport struct {
	mu     sync.Mutex
	calls  int
	onSend func(nodeID string, cmd Command)
}

func (m *mockCommandTransport) Send(nodeID string, cmd Command) error {
	m.mu.Lock()
	m.calls++
	onSend := m.onSend
	m.mu.Unlock()

	if onSend != nil {
		onSend(nodeID, cmd)
	}

	return nil
}

func (m *mockCommandTransport) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestCommandDispatcherHappyPath(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-happy", Hostname: "node-happy-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-happy",
		NodeID:    "node-happy",
		Project:   "proj-happy",
		Status:    SessionStatusRunning,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		if nodeID != "node-happy" {
			t.Errorf("unexpected nodeID: %s", nodeID)
		}
		go func(commandID string) {
			time.Sleep(10 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    "created",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	result, err := dispatcher.DispatchCommand(context.Background(), Command{
		Type:   CommandTypeCreateSession,
		Target: CommandTarget{Project: "proj-happy"},
		Args: map[string]interface{}{
			"prompt": "start",
		},
	})
	if err != nil {
		t.Fatalf("dispatch command failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected command result")
	}
	if result.Status != CommandStatusSuccess {
		t.Fatalf("expected success status, got %s", result.Status)
	}
	if result.Output != "created" {
		t.Fatalf("expected output 'created', got %q", result.Output)
	}
	if _, err := uuid.Parse(result.CommandID); err != nil {
		t.Fatalf("expected UUID command_id, got %q: %v", result.CommandID, err)
	}
	if transport.CallCount() != 1 {
		t.Fatalf("expected 1 command send, got %d", transport.CallCount())
	}
}

func TestCommandDispatcherIdempotency(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-idempotent", Hostname: "node-idempotent-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-idempotent",
		NodeID:    "node-idempotent",
		Project:   "proj-idempotent",
		Status:    SessionStatusRunning,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		go func(commandID string) {
			time.Sleep(10 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    "ok",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	cmd := Command{
		Type:           CommandTypePromptSession,
		IdempotencyKey: "abc-123",
		Target:         CommandTarget{Project: "proj-idempotent"},
		Args: map[string]interface{}{
			"message": "hello",
		},
	}

	first, err := dispatcher.DispatchCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("first dispatch failed: %v", err)
	}
	second, err := dispatcher.DispatchCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("second dispatch failed: %v", err)
	}

	if transport.CallCount() != 1 {
		t.Fatalf("expected only 1 transport send, got %d", transport.CallCount())
	}
	if first.CommandID != second.CommandID {
		t.Fatalf("expected cached command result with same command_id, got %s vs %s", first.CommandID, second.CommandID)
	}
	if second.Status != CommandStatusSuccess {
		t.Fatalf("expected success cached status, got %s", second.Status)
	}
}

func TestCommandDispatcherOfflineNode(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-offline", Hostname: "node-offline-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}
	if err := registry.MarkOffline("node-offline"); err != nil {
		t.Fatalf("mark node offline failed: %v", err)
	}

	transport := &mockCommandTransport{}
	dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	result, err := dispatcher.DispatchCommand(context.Background(), Command{
		Type:   CommandTypeKillSession,
		Target: CommandTarget{NodeID: "node-offline"},
	})
	if err != nil {
		t.Fatalf("dispatch should return deterministic failure payload, got error: %v", err)
	}
	if result == nil {
		t.Fatal("expected command result")
	}
	if result.Status != CommandStatusFailure {
		t.Fatalf("expected failure status, got %s", result.Status)
	}
	if result.Error != "target node offline: node-offline" {
		t.Fatalf("unexpected offline failure error: %q", result.Error)
	}
	if transport.CallCount() != 0 {
		t.Fatalf("expected no command send for offline node, got %d", transport.CallCount())
	}
}

func TestCommandDispatcherTimeout(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-timeout", Hostname: "node-timeout-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID: "sess-timeout",
		NodeID:    "node-timeout",
		Project:   "proj-timeout",
		Status:    SessionStatusRunning,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("add session failed: %v", err)
	}

	transport := &mockCommandTransport{}
	dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	result, err := dispatcher.DispatchCommand(context.Background(), Command{
		Type:    CommandTypeSessionStatus,
		Target:  CommandTarget{Project: "proj-timeout"},
		Timeout: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("dispatch command failed: %v", err)
	}
	if result.Status != CommandStatusTimeout {
		t.Fatalf("expected timeout status, got %s", result.Status)
	}
	if result.Error == "" {
		t.Fatalf("expected timeout error payload")
	}
	if transport.CallCount() != 1 {
		t.Fatalf("expected one command send before timeout, got %d", transport.CallCount())
	}
}
