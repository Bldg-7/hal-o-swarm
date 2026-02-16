package supervisor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

const testAuthToken = "test-secret-token"

func setupHTTPAPI(t *testing.T) (*HTTPAPI, *NodeRegistry, *SessionTracker) {
	t.Helper()
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	api := NewHTTPAPI(registry, tracker, nil, db, testAuthToken, logger)
	return api, registry, tracker
}

func setupHTTPAPIWithDispatcher(t *testing.T) (*HTTPAPI, *NodeRegistry, *SessionTracker, *CommandDispatcher) {
	t.Helper()
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, &mockCommandTransport{}, logger)

	api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
	return api, registry, tracker, dispatcher
}

func seedNode(t *testing.T, registry *NodeRegistry, id, hostname string) {
	t.Helper()
	if err := registry.Register(NodeEntry{ID: id, Hostname: hostname}); err != nil {
		t.Fatalf("register node %s: %v", id, err)
	}
}

func seedSession(t *testing.T, registry *NodeRegistry, tracker *SessionTracker, session TrackedSession) {
	t.Helper()
	if _, err := registry.GetNode(session.NodeID); err != nil {
		seedNode(t, registry, session.NodeID, session.NodeID+"-host")
	}
	if err := tracker.AddSession(session); err != nil {
		t.Fatalf("add session %s: %v", session.SessionID, err)
	}
}

func seedEvent(t *testing.T, api *HTTPAPI, registry *NodeRegistry, tracker *SessionTracker, id, sessionID, eventType, data string, ts time.Time) {
	t.Helper()
	if _, err := tracker.GetSession(sessionID); err != nil {
		seedSession(t, registry, tracker, TrackedSession{
			SessionID: sessionID, NodeID: sessionID + "-node", Project: "test-proj", Status: SessionStatusRunning,
		})
	}
	_, err := api.db.Exec(
		`INSERT INTO events (id, session_id, type, data, timestamp) VALUES (?, ?, ?, ?, ?)`,
		id, sessionID, eventType, data, ts.Format(time.RFC3339Nano),
	)
	if err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

func authRequest(method, path string, body string) *http.Request {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	return req
}

func TestHTTPAPIHealthNoAuth(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Components["database"] != "ok" {
		t.Errorf("expected database ok, got %s", resp.Components["database"])
	}
	if resp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestHTTPAPIHealthWithDispatcher(t *testing.T) {
	api, _, _, _ := setupHTTPAPIWithDispatcher(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp healthResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Status != "healthy" {
		t.Errorf("expected healthy, got %s", resp.Status)
	}
	if resp.Components["command_dispatcher"] != "ok" {
		t.Errorf("expected dispatcher ok, got %s", resp.Components["command_dispatcher"])
	}
}

func TestHTTPAPIUnauthorized(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/sessions"},
		{"GET", "/api/v1/sessions/test-id"},
		{"GET", "/api/v1/nodes"},
		{"GET", "/api/v1/nodes/test-id"},
		{"GET", "/api/v1/events"},
		{"POST", "/api/v1/commands"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", w.Code)
			}

			var errResp apiError
			if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if errResp.Code != "AUTH_REQUIRED" {
				t.Errorf("expected AUTH_REQUIRED, got %s", errResp.Code)
			}
		})
	}
}

func TestHTTPAPIWrongToken(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHTTPAPIListSessions(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC().Truncate(time.Second)
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-1", NodeID: "node-a", Project: "proj-alpha",
		Status: SessionStatusRunning, TokenUsage: TokenUsage{Total: 100},
		SessionCost: 0.50, StartedAt: now,
	})
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-2", NodeID: "node-b", Project: "proj-beta",
		Status: SessionStatusIdle, TokenUsage: TokenUsage{Total: 200},
		SessionCost: 1.00, StartedAt: now,
	})

	req := authRequest("GET", "/api/v1/sessions", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	data, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatal("expected data to be array")
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(data))
	}
	if resp.Meta == nil {
		t.Fatal("expected meta to be present")
	}
	if resp.Meta.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Meta.Total)
	}
}

func TestHTTPAPIListSessionsFilterProject(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-1", NodeID: "node-a", Project: "proj-alpha", Status: SessionStatusRunning,
	})
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-2", NodeID: "node-b", Project: "proj-beta", Status: SessionStatusIdle,
	})

	req := authRequest("GET", "/api/v1/sessions?project=proj-alpha", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 session for proj-alpha, got %d", len(data))
	}
}

func TestHTTPAPIListSessionsFilterStatus(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-1", NodeID: "node-a", Project: "proj-a", Status: SessionStatusRunning,
	})
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-2", NodeID: "node-b", Project: "proj-b", Status: SessionStatusIdle,
	})

	req := authRequest("GET", "/api/v1/sessions?status=idle", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 idle session, got %d", len(data))
	}
}

func TestHTTPAPIListSessionsFilterNodeID(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-1", NodeID: "node-a", Project: "proj-a", Status: SessionStatusRunning,
	})
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-2", NodeID: "node-b", Project: "proj-b", Status: SessionStatusIdle,
	})

	req := authRequest("GET", "/api/v1/sessions?node_id=node-a", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 session for node-a, got %d", len(data))
	}
}

func TestHTTPAPIGetSession(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC().Truncate(time.Second)
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-get", NodeID: "node-x", Project: "proj-x",
		Status: SessionStatusRunning, TokenUsage: TokenUsage{Total: 42},
		SessionCost: 0.99, StartedAt: now,
	})

	req := authRequest("GET", "/api/v1/sessions/ses-get", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data sessionJSON `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID != "ses-get" {
		t.Errorf("expected ses-get, got %s", resp.Data.ID)
	}
	if resp.Data.Tokens != 42 {
		t.Errorf("expected 42 tokens, got %d", resp.Data.Tokens)
	}
}

func TestHTTPAPIGetSessionNotFound(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := authRequest("GET", "/api/v1/sessions/nonexistent", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var errResp apiError
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %s", errResp.Code)
	}
}

func TestHTTPAPIListNodes(t *testing.T) {
	api, registry, _ := setupHTTPAPI(t)
	handler := api.Handler()

	seedNode(t, registry, "node-1", "host-1")
	seedNode(t, registry, "node-2", "host-2")

	req := authRequest("GET", "/api/v1/nodes", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(data))
	}
	if resp.Meta.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Meta.Total)
	}
}

func TestHTTPAPIGetNode(t *testing.T) {
	api, registry, _ := setupHTTPAPI(t)
	handler := api.Handler()

	seedNode(t, registry, "node-get", "host-get")

	req := authRequest("GET", "/api/v1/nodes/node-get", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Data nodeJSON `json:"data"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Data.ID != "node-get" {
		t.Errorf("expected node-get, got %s", resp.Data.ID)
	}
	if resp.Data.Hostname != "host-get" {
		t.Errorf("expected host-get, got %s", resp.Data.Hostname)
	}
}

func TestHTTPAPIGetNodeNotFound(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := authRequest("GET", "/api/v1/nodes/nonexistent", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHTTPAPIListEvents(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC()
	seedEvent(t, api, registry, tracker, "evt-1", "ses-1", "session.start", `{"action":"start"}`, now)
	seedEvent(t, api, registry, tracker, "evt-2", "ses-2", "session.error", `{"action":"error"}`, now.Add(time.Second))

	req := authRequest("GET", "/api/v1/events", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 events, got %d", len(data))
	}
}

func TestHTTPAPIListEventsFilterSessionID(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC()
	seedEvent(t, api, registry, tracker, "evt-1", "ses-1", "session.start", "{}", now)
	seedEvent(t, api, registry, tracker, "evt-2", "ses-2", "session.start", "{}", now)

	req := authRequest("GET", "/api/v1/events?session_id=ses-1", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 event for ses-1, got %d", len(data))
	}
}

func TestHTTPAPIListEventsFilterType(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC()
	seedEvent(t, api, registry, tracker, "evt-1", "ses-1", "session.start", "{}", now)
	seedEvent(t, api, registry, tracker, "evt-2", "ses-1", "session.error", "{}", now.Add(time.Second))

	req := authRequest("GET", "/api/v1/events?type=session.error", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(data))
	}
}

func TestHTTPAPIListEventsLimit(t *testing.T) {
	api, registry, tracker := setupHTTPAPI(t)
	handler := api.Handler()

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		seedEvent(t, api, registry, tracker,
			fmt.Sprintf("evt-%d", i), "ses-lim", "session.start", "{}",
			now.Add(time.Duration(i)*time.Second))
	}

	req := authRequest("GET", "/api/v1/events?limit=2", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp apiResponse
	json.NewDecoder(w.Body).Decode(&resp)
	data := resp.Data.([]interface{})
	if len(data) != 2 {
		t.Fatalf("expected 2 events with limit, got %d", len(data))
	}
}

func TestHTTPAPICommandMissingType(t *testing.T) {
	api, _, _, _ := setupHTTPAPIWithDispatcher(t)
	handler := api.Handler()

	req := authRequest("POST", "/api/v1/commands", `{"target":"proj-a"}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHTTPAPICommandDispatcherUnavailable(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := authRequest("POST", "/api/v1/commands", `{"type":"session_status","target":"proj-a"}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHTTPAPICommandDispatch(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)

	seedNode(t, registry, "node-cmd", "host-cmd")
	seedSession(t, registry, tracker, TrackedSession{
		SessionID: "ses-cmd", NodeID: "node-cmd", Project: "proj-cmd", Status: SessionStatusRunning,
	})

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		go func(cmdID string) {
			time.Sleep(10 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: cmdID,
				Status:    CommandStatusSuccess,
				Output:    "done",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
	handler := api.Handler()

	req := authRequest("POST", "/api/v1/commands", `{"type":"session_status","target":"proj-cmd"}`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data commandResultJSON `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Status != CommandStatusSuccess {
		t.Errorf("expected success, got %s", resp.Data.Status)
	}
	if resp.Data.Output != "done" {
		t.Errorf("expected 'done', got %s", resp.Data.Output)
	}
}

func TestHTTPAPICommandInvalidBody(t *testing.T) {
	api, _, _, _ := setupHTTPAPIWithDispatcher(t)
	handler := api.Handler()

	req := authRequest("POST", "/api/v1/commands", `{invalid json`)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHTTPAPIResponseContentType(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json, got %s", ct)
	}
}

func TestHTTPAPIErrorResponseContentType(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected application/json for error, got %s", ct)
	}
}
