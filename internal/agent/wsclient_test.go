package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

// --- Backoff Tests ---

func TestBackoffDuration(t *testing.T) {
	b := DefaultBackoff()

	prev := time.Duration(0)
	for i := 0; i < 10; i++ {
		d := b.Duration()
		if d < b.Min {
			t.Errorf("attempt %d: duration %v < min %v", i, d, b.Min)
		}
		if d > b.Max {
			t.Errorf("attempt %d: duration %v > max %v", i, d, b.Max)
		}
		// After first few attempts, durations should generally increase
		if i > 2 && d < prev/4 {
			t.Errorf("attempt %d: duration %v is suspiciously small vs prev %v", i, d, prev)
		}
		prev = d
	}
}

func TestBackoffReset(t *testing.T) {
	b := DefaultBackoff()

	// Advance several attempts
	for i := 0; i < 5; i++ {
		b.Duration()
	}
	if b.Attempt() != 5 {
		t.Errorf("expected attempt 5, got %d", b.Attempt())
	}

	b.Reset()
	if b.Attempt() != 0 {
		t.Errorf("expected attempt 0 after reset, got %d", b.Attempt())
	}

	// Post-reset duration should be near Min (within jitter)
	d := b.Duration()
	maxWithJitter := time.Duration(float64(b.Min) * (1 + b.Jitter))
	if d > maxWithJitter {
		t.Errorf("post-reset duration %v > expected max %v", d, maxWithJitter)
	}
}

func TestBackoffCap(t *testing.T) {
	b := DefaultBackoff()

	// Advance well past where exponential would exceed Max
	for i := 0; i < 30; i++ {
		d := b.Duration()
		if d > b.Max {
			t.Fatalf("attempt %d: duration %v exceeds max %v", i, d, b.Max)
		}
	}
}

func TestBackoffJitter(t *testing.T) {
	b := &Backoff{
		Min:    100 * time.Millisecond,
		Max:    60 * time.Second,
		Factor: 2.0,
		Jitter: 0.25,
	}

	// Run many trials to verify jitter bounds for attempt 0
	for trial := 0; trial < 100; trial++ {
		b.Reset()
		d := b.Duration()

		baseMin := float64(b.Min) * (1 - b.Jitter)
		baseMax := float64(b.Min) * (1 + b.Jitter)

		if float64(d) < baseMin || float64(d) > baseMax {
			t.Errorf("trial %d: duration %v outside jitter bounds [%v, %v]",
				trial, d, time.Duration(baseMin), time.Duration(baseMax))
		}
	}
}

func TestBackoffExponentialGrowth(t *testing.T) {
	// No jitter to test pure exponential
	b := &Backoff{
		Min:    100 * time.Millisecond,
		Max:    60 * time.Second,
		Factor: 2.0,
		Jitter: 0, // no jitter for deterministic test
	}

	expected := []time.Duration{
		100 * time.Millisecond,  // 100ms * 2^0
		200 * time.Millisecond,  // 100ms * 2^1
		400 * time.Millisecond,  // 100ms * 2^2
		800 * time.Millisecond,  // 100ms * 2^3
		1600 * time.Millisecond, // 100ms * 2^4
	}

	for i, want := range expected {
		got := b.Duration()
		if got != want {
			t.Errorf("attempt %d: got %v, want %v", i, got, want)
		}
	}
}

// --- WebSocket Test Helpers ---

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// mockWSServer is a test WebSocket server that records connections and messages.
type mockWSServer struct {
	server      *httptest.Server
	messages    [][]byte
	authHeaders []string
	mu          sync.Mutex
	connCh      chan *websocket.Conn
}

func newMockWSServer(t *testing.T) *mockWSServer {
	t.Helper()
	m := &mockWSServer{
		connCh: make(chan *websocket.Conn, 10),
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.authHeaders = append(m.authHeaders, r.Header.Get("Authorization"))
		m.mu.Unlock()

		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		m.connCh <- conn

		// Read messages until connection closes
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			m.mu.Lock()
			m.messages = append(m.messages, msg)
			m.mu.Unlock()
		}
	}))

	return m
}

func (m *mockWSServer) URL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockWSServer) Close() {
	m.server.Close()
}

func (m *mockWSServer) GetMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := make([][]byte, len(m.messages))
	copy(msgs, m.messages)
	return msgs
}

func (m *mockWSServer) GetAuthHeaders() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	headers := make([]string, len(m.authHeaders))
	copy(headers, m.authHeaders)
	return headers
}

func testLogger(t *testing.T) *zap.Logger {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	return logger
}

func fastTestBackoff() *Backoff {
	return &Backoff{
		Min:    10 * time.Millisecond,
		Max:    100 * time.Millisecond,
		Factor: 2.0,
		Jitter: 0.1,
	}
}

// --- WebSocket Client Tests ---

func TestWSClientConnect(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	logger := testLogger(t)
	client := NewWSClient(mock.URL(), "test-token-123", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	// Wait for connection
	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for connection")
	}

	// Verify auth header
	headers := mock.GetAuthHeaders()
	if len(headers) == 0 {
		t.Fatal("no auth headers recorded")
	}
	if headers[0] != "Bearer test-token-123" {
		t.Errorf("expected auth header 'Bearer test-token-123', got %q", headers[0])
	}

	// Verify connected state
	time.Sleep(50 * time.Millisecond)
	if !client.IsConnected() {
		t.Error("client should be connected")
	}
}

func TestWSClientSnapshot(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	logger := testLogger(t)

	snapshotCalled := atomic.Int32{}
	provider := func() *StateSnapshot {
		snapshotCalled.Add(1)
		return &StateSnapshot{
			Sessions: []SessionSnapshot{
				{
					SessionID: "sess-1",
					Project:   "test-project",
					Status:    "running",
					Tokens:    1000,
					Cost:      0.05,
					StartedAt: time.Now().Unix(),
				},
			},
			LastSeq: 42,
		}
	}

	client := NewWSClient(mock.URL(), "token", logger,
		WithSnapshotProvider(provider),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	// Wait for connection
	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for connection")
	}

	// Give time for snapshot to be sent and read by server
	time.Sleep(200 * time.Millisecond)

	if snapshotCalled.Load() != 1 {
		t.Errorf("expected snapshot provider called once, got %d", snapshotCalled.Load())
	}

	// Verify snapshot message received by server
	msgs := mock.GetMessages()
	if len(msgs) == 0 {
		t.Fatal("no messages received by server")
	}

	// Parse the REGISTER envelope
	env, err := shared.UnmarshalEnvelope(msgs[0])
	if err != nil {
		t.Fatalf("failed to unmarshal snapshot envelope: %v", err)
	}

	if env.Type != string(shared.MessageTypeRegister) {
		t.Errorf("expected type %q, got %q", shared.MessageTypeRegister, env.Type)
	}

	var snap StateSnapshot
	if err := json.Unmarshal(env.Payload, &snap); err != nil {
		t.Fatalf("failed to unmarshal snapshot payload: %v", err)
	}

	if len(snap.Sessions) != 1 {
		t.Fatalf("expected 1 session in snapshot, got %d", len(snap.Sessions))
	}
	if snap.Sessions[0].SessionID != "sess-1" {
		t.Errorf("expected session ID 'sess-1', got %q", snap.Sessions[0].SessionID)
	}
	if snap.Sessions[0].Project != "test-project" {
		t.Errorf("expected project 'test-project', got %q", snap.Sessions[0].Project)
	}
	if snap.LastSeq != 42 {
		t.Errorf("expected last_seq 42, got %d", snap.LastSeq)
	}
}

func TestWSClientReconnect(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	logger := testLogger(t)

	connectCount := atomic.Int32{}
	provider := func() *StateSnapshot {
		connectCount.Add(1)
		return &StateSnapshot{
			Sessions: []SessionSnapshot{
				{SessionID: "s1", Project: "p1", Status: "running"},
			},
			LastSeq: 0,
		}
	}

	client := NewWSClient(mock.URL(), "token", logger,
		WithSnapshotProvider(provider),
		WithBackoff(fastTestBackoff()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	// Wait for first connection
	var conn1 *websocket.Conn
	select {
	case conn1 = <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first connection")
	}

	// Give snapshot time to be sent
	time.Sleep(100 * time.Millisecond)

	// Close server-side connection to trigger reconnect
	conn1.Close()

	// Wait for second connection (reconnect)
	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect")
	}

	// Snapshot should have been sent twice (once per connection)
	time.Sleep(200 * time.Millisecond)
	if got := connectCount.Load(); got != 2 {
		t.Errorf("expected 2 snapshot calls (connect + reconnect), got %d", got)
	}

	// Verify 2 REGISTER messages received
	msgs := mock.GetMessages()
	registerCount := 0
	for _, msg := range msgs {
		env, err := shared.UnmarshalEnvelope(msg)
		if err != nil {
			continue
		}
		if env.Type == string(shared.MessageTypeRegister) {
			registerCount++
		}
	}
	if registerCount != 2 {
		t.Errorf("expected 2 register messages, got %d", registerCount)
	}
}

func TestWSClientUnreachable(t *testing.T) {
	logger := testLogger(t)

	fb := fastTestBackoff()
	// Use port 1 which is almost certainly not listening
	client := NewWSClient("ws://127.0.0.1:1", "token", logger,
		WithBackoff(fb),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	client.Connect(ctx)

	// Let it retry — should not crash
	time.Sleep(300 * time.Millisecond)

	// Verify multiple retry attempts occurred
	if fb.Attempt() < 2 {
		t.Errorf("expected at least 2 retry attempts, got %d", fb.Attempt())
	}

	// Clean shutdown
	if err := client.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestWSClientEventResend(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	logger := testLogger(t)
	client := NewWSClient(mock.URL(), "token", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	// Wait for connection
	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for connection")
	}

	time.Sleep(50 * time.Millisecond)

	// Send two events
	env1 := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeEvent),
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{"msg":"event1"}`),
	}
	env2 := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeEvent),
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{"msg":"event2"}`),
	}

	seq1, err := client.SendEvent(env1)
	if err != nil {
		t.Fatalf("SendEvent 1 failed: %v", err)
	}
	seq2, err := client.SendEvent(env2)
	if err != nil {
		t.Fatalf("SendEvent 2 failed: %v", err)
	}

	if seq1 != 1 || seq2 != 2 {
		t.Errorf("expected sequences 1,2 got %d,%d", seq1, seq2)
	}

	// Acknowledge first event only
	client.AcknowledgeSeq(1)

	// Verify only event 2 remains pending
	client.pendingMu.Lock()
	pendingCount := len(client.pendingEvents)
	var pendingSeq int64
	if pendingCount > 0 {
		pendingSeq = client.pendingEvents[0].seq
	}
	client.pendingMu.Unlock()

	if pendingCount != 1 {
		t.Errorf("expected 1 pending event after ack, got %d", pendingCount)
	}
	if pendingSeq != 2 {
		t.Errorf("expected pending event seq=2, got %d", pendingSeq)
	}

	if client.LastAckedSeq() != 1 {
		t.Errorf("expected lastAckedSeq 1, got %d", client.LastAckedSeq())
	}
}

func TestWSClientEventResendOnReconnect(t *testing.T) {
	mock := newMockWSServer(t)
	defer mock.Close()

	logger := testLogger(t)
	client := NewWSClient(mock.URL(), "token", logger,
		WithBackoff(fastTestBackoff()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client.Connect(ctx)
	defer client.Close()

	// First connection
	var conn1 *websocket.Conn
	select {
	case conn1 = <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first connection")
	}

	time.Sleep(50 * time.Millisecond)

	// Send an event (will be buffered as pending)
	ev := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeEvent),
		Timestamp: time.Now().Unix(),
		Payload:   json.RawMessage(`{"msg":"resend-me"}`),
	}
	_, err := client.SendEvent(ev)
	if err != nil {
		t.Fatalf("SendEvent failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Count messages before reconnect
	msgsBefore := len(mock.GetMessages())

	// Close connection to trigger reconnect
	conn1.Close()

	// Wait for reconnect
	select {
	case <-mock.connCh:
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnect")
	}

	time.Sleep(200 * time.Millisecond)

	// After reconnect, the pending event should have been resent
	msgsAfter := mock.GetMessages()
	newMsgCount := len(msgsAfter) - msgsBefore
	if newMsgCount < 1 {
		t.Errorf("expected at least 1 new message after reconnect (resend), got %d", newMsgCount)
	}

	// Verify the resent event is present
	found := false
	for _, msg := range msgsAfter[msgsBefore:] {
		env, err := shared.UnmarshalEnvelope(msg)
		if err != nil {
			continue
		}
		if env.Type == string(shared.MessageTypeEvent) {
			var payload map[string]string
			if err := json.Unmarshal(env.Payload, &payload); err == nil {
				if payload["msg"] == "resend-me" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("resent event not found in messages after reconnect")
	}
}
