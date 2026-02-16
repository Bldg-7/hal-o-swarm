package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOpencodeAdapterMock(t *testing.T) {
	adapter := NewMockOpencodeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := adapter.SubscribeEvents(ctx)
	if err != nil {
		t.Fatalf("SubscribeEvents failed: %v", err)
	}

	sessionID, err := adapter.CreateSession(ctx, "project-a", "hello")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := adapter.PromptSession(ctx, sessionID, "follow-up"); err != nil {
		t.Fatalf("PromptSession failed: %v", err)
	}

	status, err := adapter.SessionStatus(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionStatus failed: %v", err)
	}
	if status != SessionStatusRunning {
		t.Fatalf("unexpected session status: got %q, want %q", status, SessionStatusRunning)
	}

	listed, err := adapter.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("unexpected session count: got %d, want 1", len(listed))
	}

	select {
	case evt := <-events:
		if evt.SessionID == "" {
			t.Fatalf("expected session event with id, got %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if err := adapter.KillSession(ctx, sessionID); err != nil {
		t.Fatalf("KillSession failed: %v", err)
	}
}

func TestOpencodeAdapterSessionNotFound(t *testing.T) {
	adapter := NewMockOpencodeAdapter()
	ctx := context.Background()

	if err := adapter.PromptSession(ctx, "missing", "test"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound from PromptSession, got %v", err)
	}

	if err := adapter.KillSession(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound from KillSession, got %v", err)
	}

	if _, err := adapter.SessionStatus(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound from SessionStatus, got %v", err)
	}
}

func TestOpencodeAdapterSSEDisconnect(t *testing.T) {
	adapter := NewMockOpencodeAdapter()
	adapter.SetStreamError(context.DeadlineExceeded)

	_, err := adapter.SubscribeEvents(context.Background())
	if err == nil {
		t.Fatal("expected error from SubscribeEvents")
	}
	if !errors.Is(err, ErrRecoverable) {
		t.Fatalf("expected ErrRecoverable, got %v", err)
	}
}
