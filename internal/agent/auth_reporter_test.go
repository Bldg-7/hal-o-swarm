package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type mockAuthAdapter struct {
	tool         shared.ToolIdentifier
	statuses     []shared.AuthStatus
	reason       string
	panicOnCheck bool

	mu    sync.Mutex
	calls int
}

func (m *mockAuthAdapter) ToolID() shared.ToolIdentifier {
	return m.tool
}

func (m *mockAuthAdapter) CheckAuth(ctx context.Context) shared.AuthStateReport {
	_ = ctx

	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	if m.panicOnCheck {
		panic("adapter failure")
	}

	idx := m.calls - 1
	if idx >= len(m.statuses) {
		idx = len(m.statuses) - 1
	}
	status := shared.AuthStatusAuthenticated
	if idx >= 0 {
		status = m.statuses[idx]
	}

	reason := m.reason
	if reason == "" {
		reason = "mocked"
	}

	return shared.AuthStateReport{
		Tool:      m.tool,
		Status:    status,
		Reason:    reason,
		CheckedAt: time.Now().UTC(),
	}
}

type mockAuthStateSender struct {
	mu      sync.Mutex
	reports [][]shared.AuthStateReport
	ch      chan []shared.AuthStateReport
}

func newMockAuthStateSender() *mockAuthStateSender {
	return &mockAuthStateSender{ch: make(chan []shared.AuthStateReport, 32)}
}

func (m *mockAuthStateSender) SendAuthState(reports []shared.AuthStateReport) error {
	copyReports := make([]shared.AuthStateReport, len(reports))
	copy(copyReports, reports)

	m.mu.Lock()
	m.reports = append(m.reports, copyReports)
	m.mu.Unlock()

	m.ch <- copyReports
	return nil
}

func (m *mockAuthStateSender) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reports)
}

func TestAuthReporterInitialReport(t *testing.T) {
	sender := newMockAuthStateSender()
	reporter := NewAuthReporter(
		[]AuthAdapter{
			&mockAuthAdapter{tool: shared.ToolIdentifierOpenCode, statuses: []shared.AuthStatus{shared.AuthStatusAuthenticated}},
		},
		500*time.Millisecond,
		sender,
		zap.NewNop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reporter.Start(ctx)
	}()

	select {
	case reports := <-sender.ch:
		if len(reports) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reports))
		}
		if reports[0].Tool != shared.ToolIdentifierOpenCode {
			t.Fatalf("expected tool %q, got %q", shared.ToolIdentifierOpenCode, reports[0].Tool)
		}
	case <-time.After(120 * time.Millisecond):
		t.Fatal("timed out waiting for initial auth report")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("reporter did not stop after context cancellation")
	}
}

func TestAuthReporterPeriodicReport(t *testing.T) {
	sender := newMockAuthStateSender()
	reporter := NewAuthReporter(
		[]AuthAdapter{
			&mockAuthAdapter{tool: shared.ToolIdentifierOpenCode, statuses: []shared.AuthStatus{shared.AuthStatusAuthenticated}},
		},
		50*time.Millisecond,
		sender,
		zap.NewNop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reporter.Start(ctx)
	}()

	deadline := time.After(220 * time.Millisecond)
	for sender.count() < 2 {
		select {
		case <-sender.ch:
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("expected at least 2 auth-state reports within deadline, got %d", sender.count())
		}
	}

	cancel()
	<-done
}

func TestAuthReporterSurvivesAdapterFailure(t *testing.T) {
	sender := newMockAuthStateSender()
	reporter := NewAuthReporter(
		[]AuthAdapter{
			&mockAuthAdapter{tool: shared.ToolIdentifierOpenCode, statuses: []shared.AuthStatus{shared.AuthStatusAuthenticated}},
			&mockAuthAdapter{tool: shared.ToolIdentifierClaudeCode, panicOnCheck: true},
			&mockAuthAdapter{tool: shared.ToolIdentifierCodex, statuses: []shared.AuthStatus{shared.AuthStatusUnauthenticated}},
		},
		60*time.Millisecond,
		sender,
		zap.NewNop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reporter.Start(ctx)
	}()

	var first []shared.AuthStateReport
	select {
	case first = <-sender.ch:
	case <-time.After(150 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("timed out waiting for first report")
	}

	if len(first) != 3 {
		cancel()
		<-done
		t.Fatalf("expected 3 tool reports, got %d", len(first))
	}

	statusByTool := make(map[shared.ToolIdentifier]shared.AuthStatus, len(first))
	for _, report := range first {
		statusByTool[report.Tool] = report.Status
	}

	if statusByTool[shared.ToolIdentifierOpenCode] != shared.AuthStatusAuthenticated {
		t.Fatalf("expected opencode authenticated, got %q", statusByTool[shared.ToolIdentifierOpenCode])
	}
	if statusByTool[shared.ToolIdentifierClaudeCode] != shared.AuthStatusError {
		t.Fatalf("expected claude_code error, got %q", statusByTool[shared.ToolIdentifierClaudeCode])
	}
	if statusByTool[shared.ToolIdentifierCodex] != shared.AuthStatusUnauthenticated {
		t.Fatalf("expected codex unauthenticated, got %q", statusByTool[shared.ToolIdentifierCodex])
	}

	select {
	case <-sender.ch:
	case <-time.After(180 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("reporter did not continue reporting after adapter failure")
	}

	cancel()
	<-done
}

func TestAuthReporterChangeDetection(t *testing.T) {
	sender := newMockAuthStateSender()
	reporter := NewAuthReporter(
		[]AuthAdapter{
			&mockAuthAdapter{
				tool:     shared.ToolIdentifierOpenCode,
				statuses: []shared.AuthStatus{shared.AuthStatusUnauthenticated, shared.AuthStatusAuthenticated},
			},
		},
		50*time.Millisecond,
		sender,
		zap.NewNop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		reporter.Start(ctx)
	}()

	var first []shared.AuthStateReport
	select {
	case first = <-sender.ch:
	case <-time.After(120 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("timed out waiting for first report")
	}

	if first[0].Status != shared.AuthStatusUnauthenticated {
		cancel()
		<-done
		t.Fatalf("expected first status unauthenticated, got %q", first[0].Status)
	}

	var second []shared.AuthStateReport
	select {
	case second = <-sender.ch:
	case <-time.After(180 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("timed out waiting for changed-status report")
	}

	if second[0].Status != shared.AuthStatusAuthenticated {
		cancel()
		<-done
		t.Fatalf("expected changed status authenticated, got %q", second[0].Status)
	}

	cancel()
	<-done
}

type captureEnvelopeSender struct {
	env *shared.Envelope
	mu  sync.Mutex
}

func (c *captureEnvelopeSender) SendEnvelope(env *shared.Envelope) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.env = env
	return nil
}

func TestWSAuthStateSenderUsesAuthStateEnvelope(t *testing.T) {
	capture := &captureEnvelopeSender{}
	sender := NewWSAuthStateSender(capture)

	reports := []shared.AuthStateReport{{
		Tool:      shared.ToolIdentifierOpenCode,
		Status:    shared.AuthStatusAuthenticated,
		Reason:    "ok",
		CheckedAt: time.Now().UTC(),
	}}

	if err := sender.SendAuthState(reports); err != nil {
		t.Fatalf("SendAuthState returned error: %v", err)
	}

	if capture.env == nil {
		t.Fatal("expected envelope to be captured")
	}
	if capture.env.Type != string(shared.MessageTypeAuthState) {
		t.Fatalf("expected envelope type %q, got %q", shared.MessageTypeAuthState, capture.env.Type)
	}

	var got []shared.AuthStateReport
	if err := json.Unmarshal(capture.env.Payload, &got); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(got) != 1 || got[0].Tool != shared.ToolIdentifierOpenCode {
		t.Fatalf("unexpected payload reports: %+v", got)
	}
}
