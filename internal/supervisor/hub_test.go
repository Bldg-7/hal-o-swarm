package supervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func newTestHub(ctx context.Context, heartbeatInterval time.Duration, heartbeatTimeout int) *Hub {
	return NewHub(ctx, "test-token", nil, heartbeatInterval, heartbeatTimeout, zap.NewNop())
}

func startTestServer(hub *Hub) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/agent", hub.ServeWS)
	return httptest.NewServer(mux)
}

func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"
}

func sendHeartbeat(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{"status": "ok"})
	env := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeHeartbeat),
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}
	data, err := shared.MarshalEnvelope(env)
	if err != nil {
		t.Fatalf("failed to marshal heartbeat: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send heartbeat: %v", err)
	}
}

func TestHubAgentConnectHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 100*time.Millisecond, 3)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online event")
	}

	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", hub.ClientCount())
	}

	for i := 0; i < 5; i++ {
		sendHeartbeat(t, conn)
		time.Sleep(50 * time.Millisecond)
	}

	if hub.ClientCount() != 1 {
		t.Errorf("expected 1 client after heartbeats, got %d", hub.ClientCount())
	}

	select {
	case ev := <-hub.Events():
		t.Errorf("unexpected event: %s", ev.Type)
	default:
	}
}

func TestHubInvalidToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 30*time.Second, 3)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer wrong-token")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err == nil {
		t.Fatal("expected dial to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}

	_, resp, err = websocket.DefaultDialer.Dial(wsURL(server)+"?token=wrong", nil)
	if err == nil {
		t.Fatal("expected dial to fail with invalid query token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for query param, got %d", resp.StatusCode)
	}

	_, resp, err = websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err == nil {
		t.Fatal("expected dial to fail with no token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for no token, got %d", resp.StatusCode)
	}
}

func TestHubHeartbeatTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 50*time.Millisecond, 3)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server)+"?token=test-token", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online")
	}

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.offline" {
			t.Errorf("expected node.offline, got %s", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for node.offline event")
	}

	time.Sleep(100 * time.Millisecond)
	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients after timeout, got %d", hub.ClientCount())
	}
}

func TestHubOriginCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test-token", []string{"http://allowed.example.com"}, 30*time.Second, 3, zap.NewNop())
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	header.Set("Origin", "http://allowed.example.com")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("dial with allowed origin failed: %v", err)
	}

	select {
	case <-hub.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for online event")
	}
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	header2 := http.Header{}
	header2.Set("Authorization", "Bearer test-token")
	header2.Set("Origin", "http://evil.example.com")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(server), header2)
	if err == nil {
		t.Fatal("expected dial to fail with disallowed origin")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestHubQueryParamAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 30*time.Second, 3)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server)+"?token=test-token", nil)
	if err != nil {
		t.Fatalf("dial with query token failed: %v", err)
	}
	defer conn.Close()

	select {
	case ev := <-hub.Events():
		if ev.Type != "node.online" {
			t.Errorf("expected node.online, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for node.online")
	}
}

func TestHubGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	hub := newTestHub(ctx, 30*time.Second, 3)
	done := make(chan struct{})
	go func() {
		hub.Run()
		close(done)
	}()

	server := startTestServer(hub)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server)+"?token=test-token", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	select {
	case <-hub.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for registration")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Run did not exit after context cancellation")
	}
}

func TestHubReconnectSameNodeIDKeepsLatestConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 100*time.Millisecond, 3)
	go hub.Run()

	server := startTestServer(hub)
	defer server.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer test-token")
	header.Set("X-Node-ID", "node-reconnect")

	conn1, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("first dial failed: %v", err)
	}
	defer conn1.Close()

	select {
	case <-hub.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first online event")
	}

	conn2, _, err := websocket.DefaultDialer.Dial(wsURL(server), header)
	if err != nil {
		t.Fatalf("second dial failed: %v", err)
	}
	defer conn2.Close()

	select {
	case <-hub.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for second online event")
	}

	time.Sleep(150 * time.Millisecond)

	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("expected 1 active client after reconnect, got %d", got)
	}

	sendHeartbeat(t, conn2)

	time.Sleep(100 * time.Millisecond)
	if got := hub.ClientCount(); got != 1 {
		t.Fatalf("expected reconnected client to remain active, got %d", got)
	}
}

func TestAgentConnRoutesCommandResultToDispatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := newTestHub(ctx, 30*time.Second, 3)
	dispatcher := NewCommandDispatcherWithTransport(nil, nil, nil, nil, zap.NewNop())
	hub.ConfigureCommandResultDispatcher(dispatcher)

	commandID := "cmd-route-1"
	resultCh := make(chan *CommandResult, 1)
	dispatcher.pendingMu.Lock()
	dispatcher.pending[commandID] = resultCh
	dispatcher.pendingMu.Unlock()

	payload, err := json.Marshal(CommandResult{
		CommandID: commandID,
		Status:    CommandStatusSuccess,
		Output:    "ok",
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("marshal command result payload: %v", err)
	}

	env := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeCommandResult),
		RequestID: commandID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}

	conn := &AgentConn{hub: hub, agentID: "node-test"}
	conn.handleEnvelope(env)

	select {
	case result := <-resultCh:
		if result == nil {
			t.Fatal("expected non-nil command result")
		}
		if result.CommandID != commandID {
			t.Fatalf("expected command id %q, got %q", commandID, result.CommandID)
		}
		if result.Status != CommandStatusSuccess {
			t.Fatalf("expected success status, got %q", result.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for command result routing")
	}
}
