package halctl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetNodeAuthSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes/node-1/auth" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or invalid auth header")
		}

		resp := APIResponse{
			Data: NodeAuthStatus{
				NodeID:            "node-1",
				CredentialSync:    "in_sync",
				CredentialVersion: 1,
				AuthStates: map[string]AuthState{
					"opencode": {
						Tool:      "opencode",
						Status:    "authenticated",
						Reason:    "API key configured",
						CheckedAt: "2026-02-17T10:00:00Z",
					},
					"claude_code": {
						Tool:      "claude_code",
						Status:    "unauthenticated",
						Reason:    "No credentials found",
						CheckedAt: "2026-02-17T10:00:00Z",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	status, err := GetNodeAuth(client, "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", status.NodeID)
	}
	if status.CredentialSync != "in_sync" {
		t.Errorf("expected in_sync, got %s", status.CredentialSync)
	}
	if status.CredentialVersion != 1 {
		t.Errorf("expected version 1, got %d", status.CredentialVersion)
	}
	if len(status.AuthStates) != 2 {
		t.Errorf("expected 2 auth states, got %d", len(status.AuthStates))
	}
}

func TestGetNodeAuthNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(APIError{
			Error: "node not found",
			Code:  "NOT_FOUND",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	_, err := GetNodeAuth(client, "nonexistent")
	if err == nil {
		t.Fatalf("expected error for not found")
	}
}

func TestGetNodeAuthUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(APIError{
			Error: "unauthorized",
			Code:  "AUTH_REQUIRED",
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "invalid-token")
	_, err := GetNodeAuth(client, "node-1")
	if err == nil {
		t.Fatalf("expected error for unauthorized")
	}
}

func TestGetAuthDriftSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/drift" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := APIResponse{
			Data: []DriftNode{
				{
					NodeID:            "node-1",
					CredentialSync:    "drift_detected",
					CredentialVersion: 0,
				},
				{
					NodeID:            "node-3",
					CredentialSync:    "drift_detected",
					CredentialVersion: 1,
				},
			},
			Meta: &APIMeta{Total: 2},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	drifted, err := GetAuthDrift(client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(drifted) != 2 {
		t.Errorf("expected 2 drifted nodes, got %d", len(drifted))
	}
	if drifted[0].NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", drifted[0].NodeID)
	}
	if drifted[1].NodeID != "node-3" {
		t.Errorf("expected node-3, got %s", drifted[1].NodeID)
	}
}

func TestGetAuthDriftEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			Data: []DriftNode{},
			Meta: &APIMeta{Total: 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	drifted, err := GetAuthDrift(client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(drifted) != 0 {
		t.Errorf("expected 0 drifted nodes, got %d", len(drifted))
	}
}

func TestFormatAuthStatusTable(t *testing.T) {
	status := &NodeAuthStatus{
		NodeID:            "node-1",
		CredentialSync:    "in_sync",
		CredentialVersion: 1,
		AuthStates: map[string]AuthState{
			"opencode": {
				Tool:      "opencode",
				Status:    "authenticated",
				Reason:    "API key configured",
				CheckedAt: "2026-02-17T10:00:00Z",
			},
			"claude_code": {
				Tool:      "claude_code",
				Status:    "unauthenticated",
				Reason:    "No credentials found",
				CheckedAt: "2026-02-17T10:00:00Z",
			},
		},
	}

	output := FormatAuthStatusTable(status)

	if output == "" {
		t.Fatalf("expected non-empty output")
	}
	if !contains(output, "node-1") {
		t.Errorf("output missing node-1")
	}
	if !contains(output, "in_sync") {
		t.Errorf("output missing in_sync")
	}
	if !contains(output, "opencode") {
		t.Errorf("output missing opencode")
	}
	if !contains(output, "authenticated") {
		t.Errorf("output missing authenticated")
	}
}

func TestFormatAuthStatusTableEmpty(t *testing.T) {
	status := &NodeAuthStatus{
		NodeID:            "node-1",
		CredentialSync:    "unknown",
		CredentialVersion: 0,
		AuthStates:        map[string]AuthState{},
	}

	output := FormatAuthStatusTable(status)

	if output == "" {
		t.Fatalf("expected non-empty output")
	}
	if !contains(output, "node-1") {
		t.Errorf("output missing node-1")
	}
	if !contains(output, "no auth states") {
		t.Errorf("output missing 'no auth states' message")
	}
}

func TestFormatDriftTable(t *testing.T) {
	drifted := []DriftNode{
		{
			NodeID:            "node-1",
			CredentialSync:    "drift_detected",
			CredentialVersion: 0,
		},
		{
			NodeID:            "node-3",
			CredentialSync:    "drift_detected",
			CredentialVersion: 1,
		},
	}

	output := FormatDriftTable(drifted)

	if output == "" {
		t.Fatalf("expected non-empty output")
	}
	if !contains(output, "node-1") {
		t.Errorf("output missing node-1")
	}
	if !contains(output, "node-3") {
		t.Errorf("output missing node-3")
	}
	if !contains(output, "drift_detected") {
		t.Errorf("output missing drift_detected")
	}
	if !contains(output, "Total: 2") {
		t.Errorf("output missing total count")
	}
}

func TestFormatDriftTableEmpty(t *testing.T) {
	drifted := []DriftNode{}

	output := FormatDriftTable(drifted)

	if output == "" {
		t.Fatalf("expected non-empty output")
	}
	if !contains(output, "no nodes with credential drift") {
		t.Errorf("output missing 'no nodes' message")
	}
	if !contains(output, "Total: 0") {
		t.Errorf("output missing total count")
	}
}

func TestGetAuthStatusWrapper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			Data: NodeAuthStatus{
				NodeID:            "node-1",
				CredentialSync:    "in_sync",
				CredentialVersion: 1,
				AuthStates: map[string]AuthState{
					"opencode": {
						Tool:   "opencode",
						Status: "authenticated",
						Reason: "API key configured",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	status, err := GetAuthStatus(client, "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.NodeID != "node-1" {
		t.Errorf("expected node-1, got %s", status.NodeID)
	}
}

func TestGetDriftWrapper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := APIResponse{
			Data: []DriftNode{
				{
					NodeID:            "node-1",
					CredentialSync:    "drift_detected",
					CredentialVersion: 0,
				},
			},
			Meta: &APIMeta{Total: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	drifted, err := GetDrift(client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(drifted) != 1 {
		t.Errorf("expected 1 drifted node, got %d", len(drifted))
	}
}

func TestAuthStateWithoutReason(t *testing.T) {
	status := &NodeAuthStatus{
		NodeID:            "node-1",
		CredentialSync:    "in_sync",
		CredentialVersion: 1,
		AuthStates: map[string]AuthState{
			"opencode": {
				Tool:   "opencode",
				Status: "authenticated",
				Reason: "",
			},
		},
	}

	output := FormatAuthStatusTable(status)

	if !contains(output, "opencode") {
		t.Errorf("output missing opencode")
	}
	if !contains(output, "authenticated") {
		t.Errorf("output missing authenticated")
	}
}

func TestAuthStatusWithDriftDetected(t *testing.T) {
	status := &NodeAuthStatus{
		NodeID:            "node-2",
		CredentialSync:    "drift_detected",
		CredentialVersion: 1,
		AuthStates: map[string]AuthState{
			"opencode": {
				Tool:   "opencode",
				Status: "authenticated",
				Reason: "API key configured",
			},
		},
	}

	output := FormatAuthStatusTable(status)

	if !contains(output, "node-2") {
		t.Errorf("output missing node-2")
	}
	if !contains(output, "drift_detected") {
		t.Errorf("output missing drift_detected")
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAuthStateCheckedAt(t *testing.T) {
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	status := &NodeAuthStatus{
		NodeID:            "node-1",
		CredentialSync:    "in_sync",
		CredentialVersion: 1,
		AuthStates: map[string]AuthState{
			"opencode": {
				Tool:      "opencode",
				Status:    "authenticated",
				Reason:    "API key configured",
				CheckedAt: checkedAt,
			},
		},
	}

	output := FormatAuthStatusTable(status)

	if output == "" {
		t.Fatalf("expected non-empty output")
	}
	if !contains(output, "opencode") {
		t.Errorf("output missing opencode")
	}
}

func TestGetNodeAuthConnectionError(t *testing.T) {
	client := NewHTTPClient("http://invalid-host-that-does-not-exist:9999", "test-token")
	_, err := GetNodeAuth(client, "node-1")
	if err == nil {
		t.Fatalf("expected error for connection failure")
	}
}

func TestGetAuthDriftConnectionError(t *testing.T) {
	client := NewHTTPClient("http://invalid-host-that-does-not-exist:9999", "test-token")
	_, err := GetAuthDrift(client)
	if err == nil {
		t.Fatalf("expected error for connection failure")
	}
}
