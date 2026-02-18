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
	"go.uber.org/zap"
)

func TestOAuthOrchestrationSupported(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-oauth", Hostname: "node-oauth-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		if nodeID != "node-oauth" {
			t.Errorf("expected node-oauth, got %s", nodeID)
		}
		if cmd.Type != CommandTypeOAuthTrigger {
			t.Errorf("expected command type %q, got %q", CommandTypeOAuthTrigger, cmd.Type)
		}
		if cmd.Args["tool"] != string(shared.ToolIdentifierCodex) {
			t.Errorf("expected tool codex, got %v", cmd.Args["tool"])
		}

		go func(commandID string) {
			time.Sleep(5 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    `{"status":"challenge","challenge_url":"https://auth.openai.com/device","user_code":"ABCD-EFGH"}`,
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	orchestrator := NewOAuthOrchestrator(dispatcher, logger)
	result, err := orchestrator.TriggerOAuth(context.Background(), "node-oauth", shared.ToolIdentifierCodex)
	if err != nil {
		t.Fatalf("trigger oauth failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected oauth result")
	}
	if result.Status != "challenge" {
		t.Fatalf("expected challenge status, got %s", result.Status)
	}
	if result.ChallengeURL != "https://auth.openai.com/device" {
		t.Fatalf("unexpected challenge url: %s", result.ChallengeURL)
	}
	if result.UserCode != "ABCD-EFGH" {
		t.Fatalf("unexpected user code: %s", result.UserCode)
	}
	if transport.CallCount() != 1 {
		t.Fatalf("expected one dispatched command, got %d", transport.CallCount())
	}
}

func TestOAuthOrchestrationUnsupported(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	transport := &mockCommandTransport{}
	dispatcher := NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	orchestrator := NewOAuthOrchestrator(dispatcher, logger)
	result, err := orchestrator.TriggerOAuth(context.Background(), "node-oauth", shared.ToolIdentifierClaudeCode)
	if err != nil {
		t.Fatalf("trigger oauth failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected oauth result")
	}
	if result.Status != "manual_required" {
		t.Fatalf("expected manual_required, got %s", result.Status)
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty manual required reason")
	}
	if transport.CallCount() != 0 {
		t.Fatalf("expected no dispatch for unsupported tool, got %d", transport.CallCount())
	}
}

func TestOAuthTriggerEndpoint(t *testing.T) {
	db := setupSupervisorTestDB(t)
	logger := zap.NewNop()

	registry := NewNodeRegistry(db, logger)
	tracker := NewSessionTracker(db, logger)
	if err := registry.Register(NodeEntry{ID: "node-http-oauth", Hostname: "node-http-oauth-host"}); err != nil {
		t.Fatalf("register node failed: %v", err)
	}

	var dispatcher *CommandDispatcher
	transport := &mockCommandTransport{}
	transport.onSend = func(nodeID string, cmd Command) {
		go func(commandID string) {
			time.Sleep(5 * time.Millisecond)
			dispatcher.HandleCommandResult(CommandResult{
				CommandID: commandID,
				Status:    CommandStatusSuccess,
				Output:    `{"status":"challenge","challenge_url":"https://github.com/login/device","user_code":"WXYZ-1234"}`,
				Timestamp: time.Now().UTC(),
			})
		}(cmd.CommandID)
	}
	dispatcher = NewCommandDispatcherWithTransport(db, registry, tracker, transport, logger)

	api := NewHTTPAPI(registry, tracker, dispatcher, db, testAuthToken, logger)
	handler := api.Handler()

	req := httptest.NewRequest("POST", "/api/v1/oauth/trigger", strings.NewReader(`{"node_id":"node-http-oauth","tool":"opencode"}`))
	req.Header.Set("Authorization", "Bearer "+testAuthToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data OAuthResult `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Data.Status != "challenge" {
		t.Fatalf("expected challenge status, got %s", resp.Data.Status)
	}
	if resp.Data.ChallengeURL != "https://github.com/login/device" {
		t.Fatalf("expected challenge URL, got %s", resp.Data.ChallengeURL)
	}
	if resp.Data.UserCode != "WXYZ-1234" {
		t.Fatalf("expected user code WXYZ-1234, got %s", resp.Data.UserCode)
	}
}
