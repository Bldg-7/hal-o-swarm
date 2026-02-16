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

func TestHandleCredentialPush(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		db := setupSupervisorTestDB(t)
		logger := zap.NewNop()

		registry := NewNodeRegistry(db, logger)
		tracker := NewSessionTracker(db, logger)

		seedNode(t, registry, "node-cred", "host-cred")

		var dispatcher *CommandDispatcher
		transport := &mockCommandTransport{}
		transport.onSend = func(nodeID string, cmd Command) {
			go func(cmdID string) {
				time.Sleep(10 * time.Millisecond)
				dispatcher.HandleCommandResult(CommandResult{
					CommandID: cmdID,
					Status:    CommandStatusSuccess,
					Output:    "credentials applied",
					Timestamp: time.Now().UTC(),
				})
			}(cmd.CommandID)
		}
		dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

		api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
		handler := api.Handler()

		body := `{"target_node":"node-cred","env_vars":{"API_KEY":"test-value-123"},"version":1}`
		req := authRequest("POST", "/api/v1/commands/credentials/push", body)
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
		if resp.Data.Output != "credentials applied" {
			t.Errorf("expected 'credentials applied', got %s", resp.Data.Output)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		api, _, _, _ := setupHTTPAPIWithDispatcher(t)
		handler := api.Handler()

		body := `{"target_node":"node-1","env_vars":{"KEY":"val"},"version":1}`
		req := httptest.NewRequest("POST", "/api/v1/commands/credentials/push", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}

		var errResp apiError
		json.NewDecoder(w.Body).Decode(&errResp)
		if errResp.Code != "AUTH_REQUIRED" {
			t.Errorf("expected AUTH_REQUIRED, got %s", errResp.Code)
		}
	})

	t.Run("validation error empty target", func(t *testing.T) {
		api, _, _, _ := setupHTTPAPIWithDispatcher(t)
		handler := api.Handler()

		body := `{"target_node":"","env_vars":{"KEY":"val"},"version":1}`
		req := authRequest("POST", "/api/v1/commands/credentials/push", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var errResp apiError
		json.NewDecoder(w.Body).Decode(&errResp)
		if errResp.Code != "VALIDATION_ERROR" {
			t.Errorf("expected VALIDATION_ERROR, got %s", errResp.Code)
		}
		if !strings.Contains(errResp.Error, "target_node") {
			t.Errorf("expected error to mention target_node, got %s", errResp.Error)
		}
	})

	t.Run("validation error empty env_vars", func(t *testing.T) {
		api, _, _, _ := setupHTTPAPIWithDispatcher(t)
		handler := api.Handler()

		body := `{"target_node":"node-1","env_vars":{},"version":1}`
		req := authRequest("POST", "/api/v1/commands/credentials/push", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}

		var errResp apiError
		json.NewDecoder(w.Body).Decode(&errResp)
		if errResp.Code != "VALIDATION_ERROR" {
			t.Errorf("expected VALIDATION_ERROR, got %s", errResp.Code)
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		api, _, _, _ := setupHTTPAPIWithDispatcher(t)
		handler := api.Handler()

		req := authRequest("POST", "/api/v1/commands/credentials/push", `{invalid json`)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}

		var errResp apiError
		json.NewDecoder(w.Body).Decode(&errResp)
		if errResp.Code != "BAD_REQUEST" {
			t.Errorf("expected BAD_REQUEST, got %s", errResp.Code)
		}
	})

	t.Run("dispatcher unavailable", func(t *testing.T) {
		api, _, _ := setupHTTPAPI(t)
		handler := api.Handler()

		body := `{"target_node":"node-1","env_vars":{"KEY":"val"},"version":1}`
		req := authRequest("POST", "/api/v1/commands/credentials/push", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", w.Code)
		}
	})
}

func TestCredentialPushIdempotency(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	seedNode(t, registry, "node-idem", "host-idem")

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		if cmd.IdempotencyKey != "idem-key-1" {
			t.Errorf("expected idempotency key to pass through, got %q", cmd.IdempotencyKey)
		}
		go func(commandID string) {
			time.Sleep(10 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    "credentials applied",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
	handler := api.Handler()

	body := `{"target_node":"node-idem","env_vars":{"API_KEY":"value-1"},"version":1,"idempotency_key":"idem-key-1"}`
	firstReq := authRequest("POST", "/api/v1/commands/credentials/push", body)
	firstReq.Header.Set("Content-Type", "application/json")
	firstW := httptest.NewRecorder()
	handler.ServeHTTP(firstW, firstReq)

	if firstW.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d: %s", firstW.Code, firstW.Body.String())
	}

	var firstResp struct {
		Data commandResultJSON `json:"data"`
	}
	if err := json.NewDecoder(firstW.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondReq := authRequest("POST", "/api/v1/commands/credentials/push", body)
	secondReq.Header.Set("Content-Type", "application/json")
	secondW := httptest.NewRecorder()
	handler.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d: %s", secondW.Code, secondW.Body.String())
	}

	var secondResp struct {
		Data commandResultJSON `json:"data"`
	}
	if err := json.NewDecoder(secondW.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if firstResp.Data.CommandID == "" {
		t.Fatal("expected non-empty command_id")
	}
	if firstResp.Data.CommandID != secondResp.Data.CommandID {
		t.Fatalf("expected cached command_id, got %q vs %q", firstResp.Data.CommandID, secondResp.Data.CommandID)
	}
	if secondResp.Data.Status != CommandStatusSuccess {
		t.Fatalf("expected cached success status, got %s", secondResp.Data.Status)
	}
	if firstResp.Data.Output != secondResp.Data.Output {
		t.Fatalf("expected cached output, got %q vs %q", firstResp.Data.Output, secondResp.Data.Output)
	}
	if transport.CallCount() != 1 {
		t.Fatalf("expected one transport send due idempotency, got %d", transport.CallCount())
	}
}

func TestHandleNodeAuth(t *testing.T) {
	api, registry, _ := setupHTTPAPI(t)
	handler := api.Handler()

	seedNode(t, registry, "node-auth-1", "host-auth-1")

	states := map[string]NodeAuthState{
		"opencode": {
			Tool:      "opencode",
			Status:    "authenticated",
			Reason:    "api key set",
			CheckedAt: time.Now().UTC().Truncate(time.Second),
		},
		"claude_code": {
			Tool:      "claude_code",
			Status:    "unauthenticated",
			Reason:    "no credentials",
			CheckedAt: time.Now().UTC().Truncate(time.Second),
		},
	}
	if err := registry.UpdateAuthState("node-auth-1", states); err != nil {
		t.Fatalf("update auth state: %v", err)
	}

	// Set credential sync status via reconciliation
	if err := registry.ReconcileCredentialVersion("node-auth-1", 3, 3); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	req := authRequest("GET", "/api/v1/nodes/node-auth-1/auth", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data nodeAuthJSON `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data.NodeID != "node-auth-1" {
		t.Errorf("expected node-auth-1, got %s", resp.Data.NodeID)
	}
	if len(resp.Data.AuthStates) != 2 {
		t.Fatalf("expected 2 auth states, got %d", len(resp.Data.AuthStates))
	}
	if resp.Data.AuthStates["opencode"].Status != "authenticated" {
		t.Errorf("expected opencode authenticated, got %s", resp.Data.AuthStates["opencode"].Status)
	}
	if resp.Data.AuthStates["claude_code"].Status != "unauthenticated" {
		t.Errorf("expected claude_code unauthenticated, got %s", resp.Data.AuthStates["claude_code"].Status)
	}
	if resp.Data.CredentialSync != CredentialSyncStatusInSync {
		t.Errorf("expected in_sync, got %s", resp.Data.CredentialSync)
	}
	if resp.Data.CredentialVersion != 3 {
		t.Errorf("expected version 3, got %d", resp.Data.CredentialVersion)
	}
}

func TestHandleNodeAuthNotFound(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := authRequest("GET", "/api/v1/nodes/nonexistent/auth", "")
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

func TestHandleNodeAuthUnauthorized(t *testing.T) {
	api, _, _ := setupHTTPAPI(t)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/api/v1/nodes/some-node/auth", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var errResp apiError
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp.Code != "AUTH_REQUIRED" {
		t.Errorf("expected AUTH_REQUIRED, got %s", errResp.Code)
	}
}

func TestHandleAuthDrift(t *testing.T) {
	api, registry, _ := setupHTTPAPI(t)
	handler := api.Handler()

	// node-drift-1: drift_detected
	seedNode(t, registry, "node-drift-1", "host-drift-1")
	if err := registry.ReconcileCredentialVersion("node-drift-1", 1, 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// node-sync-1: in_sync
	seedNode(t, registry, "node-sync-1", "host-sync-1")
	if err := registry.ReconcileCredentialVersion("node-sync-1", 2, 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// node-drift-2: drift_detected
	seedNode(t, registry, "node-drift-2", "host-drift-2")
	if err := registry.ReconcileCredentialVersion("node-drift-2", 0, 2); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// node-unknown: unknown (default, no reconciliation)
	seedNode(t, registry, "node-unknown", "host-unknown")

	req := authRequest("GET", "/api/v1/auth/drift", "")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []driftNodeJSON `json:"data"`
		Meta *apiMeta        `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Meta == nil {
		t.Fatal("expected meta to be present")
	}
	if resp.Meta.Total != 2 {
		t.Fatalf("expected 2 drifted nodes, got %d", resp.Meta.Total)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 drift entries, got %d", len(resp.Data))
	}

	driftIDs := make(map[string]bool)
	for _, d := range resp.Data {
		driftIDs[d.NodeID] = true
		if d.CredentialSync != CredentialSyncStatusDriftDetected {
			t.Errorf("expected drift_detected for %s, got %s", d.NodeID, d.CredentialSync)
		}
	}
	if !driftIDs["node-drift-1"] {
		t.Error("expected node-drift-1 in drift list")
	}
	if !driftIDs["node-drift-2"] {
		t.Error("expected node-drift-2 in drift list")
	}
	if driftIDs["node-sync-1"] {
		t.Error("node-sync-1 should not be in drift list")
	}
	if driftIDs["node-unknown"] {
		t.Error("node-unknown should not be in drift list")
	}
}

func TestCredentialPushIdempotencyDifferentPayload(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	seedNode(t, registry, "node-idem-diff", "host-idem-diff")

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		go func(commandID string) {
			time.Sleep(10 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    "first-payload-applied",
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
	handler := api.Handler()

	firstBody := `{"target_node":"node-idem-diff","env_vars":{"API_KEY":"value-1"},"version":1,"idempotency_key":"idem-key-diff"}`
	firstReq := authRequest("POST", "/api/v1/commands/credentials/push", firstBody)
	firstReq.Header.Set("Content-Type", "application/json")
	firstW := httptest.NewRecorder()
	handler.ServeHTTP(firstW, firstReq)

	if firstW.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d: %s", firstW.Code, firstW.Body.String())
	}

	var firstResp struct {
		Data commandResultJSON `json:"data"`
	}
	if err := json.NewDecoder(firstW.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondBody := `{"target_node":"node-idem-diff","env_vars":{"API_KEY":"value-2"},"version":2,"idempotency_key":"idem-key-diff"}`
	secondReq := authRequest("POST", "/api/v1/commands/credentials/push", secondBody)
	secondReq.Header.Set("Content-Type", "application/json")
	secondW := httptest.NewRecorder()
	handler.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d: %s", secondW.Code, secondW.Body.String())
	}

	var secondResp struct {
		Data commandResultJSON `json:"data"`
	}
	if err := json.NewDecoder(secondW.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if firstResp.Data.CommandID == "" {
		t.Fatal("expected non-empty command_id")
	}
	if firstResp.Data.CommandID != secondResp.Data.CommandID {
		t.Fatalf("expected original cached command_id, got %q vs %q", firstResp.Data.CommandID, secondResp.Data.CommandID)
	}
	if firstResp.Data.Output != secondResp.Data.Output {
		t.Fatalf("expected original cached output, got %q vs %q", firstResp.Data.Output, secondResp.Data.Output)
	}
	if transport.CallCount() != 1 {
		t.Fatalf("expected one transport send for same idempotency key, got %d", transport.CallCount())
	}
}
