package halctl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Data: []string{"item1", "item2"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	body, err := client.Get("/test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
}

func TestHTTPClientGetUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized","code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "wrong-token")
	_, err := client.Get("/test")
	if err == nil {
		t.Fatal("expected error for unauthorized")
	}
	if err.Error() != "authentication failed. Check your auth token" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClientGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found","code":"NOT_FOUND"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	_, err := client.Get("/test")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if err.Error() != "resource not found: not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClientPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(APIResponse{
			Data: map[string]string{"status": "ok"},
		})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	body, err := client.Post("/test", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("Post failed: %v", err)
	}

	var resp APIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
}

func TestParseResponse(t *testing.T) {
	data := []SessionJSON{
		{ID: "s1", Project: "proj1", Status: "running"},
		{ID: "s2", Project: "proj2", Status: "stopped"},
	}
	resp := APIResponse{Data: data}
	body, _ := json.Marshal(resp)

	var sessions []SessionJSON
	if err := ParseResponse(body, &sessions); err != nil {
		t.Fatalf("ParseResponse failed: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "s1" {
		t.Fatalf("expected s1, got %s", sessions[0].ID)
	}
}

func TestListSessions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		sessions := []SessionJSON{
			{ID: "s1", Project: "proj1", Status: "running"},
		}
		json.NewEncoder(w).Encode(APIResponse{Data: sessions})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	sessions, err := ListSessions(client, "", "", "", 100)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "s1" {
		t.Fatalf("expected s1, got %s", sessions[0].ID)
	}
}

func TestGetSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		session := SessionJSON{ID: "s1", Project: "proj1", Status: "running"}
		json.NewEncoder(w).Encode(APIResponse{Data: session})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	session, err := GetSession(client, "s1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if session.ID != "s1" {
		t.Fatalf("expected s1, got %s", session.ID)
	}
}

func TestGetSessionEmptyID(t *testing.T) {
	client := NewHTTPClient("http://localhost", "test-token")
	_, err := GetSession(client, "")
	if err == nil {
		t.Fatal("expected error for empty session id")
	}
}

func TestListNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		nodes := []NodeJSON{
			{ID: "n1", Hostname: "host1", Status: "online"},
		}
		json.NewEncoder(w).Encode(APIResponse{Data: nodes})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	nodes, err := ListNodes(client)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ID != "n1" {
		t.Fatalf("expected n1, got %s", nodes[0].ID)
	}
}

func TestGetNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		node := NodeJSON{ID: "n1", Hostname: "host1", Status: "online"}
		json.NewEncoder(w).Encode(APIResponse{Data: node})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	node, err := GetNode(client, "n1")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}

	if node.ID != "n1" {
		t.Fatalf("expected n1, got %s", node.ID)
	}
}

func TestGetCostToday(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		cost := CostSummary{Period: "today", TotalCost: 10.5, SessionCount: 2}
		json.NewEncoder(w).Encode(APIResponse{Data: cost})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	cost, err := GetCostToday(client)
	if err != nil {
		t.Fatalf("GetCostToday failed: %v", err)
	}

	if cost.Period != "today" {
		t.Fatalf("expected today, got %s", cost.Period)
	}
	if cost.TotalCost != 10.5 {
		t.Fatalf("expected 10.5, got %f", cost.TotalCost)
	}
}

func TestGetEnvStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := EnvCheckResult{Project: "proj1", Status: "ok"}
		json.NewEncoder(w).Encode(APIResponse{Data: result})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	result, err := GetEnvStatus(client, "proj1")
	if err != nil {
		t.Fatalf("GetEnvStatus failed: %v", err)
	}

	if result.Project != "proj1" {
		t.Fatalf("expected proj1, got %s", result.Project)
	}
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %s", result.Status)
	}
}

func TestGetEnvStatusEmptyProject(t *testing.T) {
	client := NewHTTPClient("http://localhost", "test-token")
	_, err := GetEnvStatus(client, "")
	if err == nil {
		t.Fatal("expected error for empty project")
	}
}

func TestCheckEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := EnvCheckResult{Project: "proj1", Status: "ok"}
		json.NewEncoder(w).Encode(APIResponse{Data: result})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	result, err := CheckEnv(client, "proj1")
	if err != nil {
		t.Fatalf("CheckEnv failed: %v", err)
	}

	if result.Project != "proj1" {
		t.Fatalf("expected proj1, got %s", result.Project)
	}
}

func TestGetAgentMdDiff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		diff := AgentMdDiff{Project: "proj1", Diff: "--- local\n+++ template"}
		json.NewEncoder(w).Encode(APIResponse{Data: diff})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	diff, err := GetAgentMdDiff(client, "proj1")
	if err != nil {
		t.Fatalf("GetAgentMdDiff failed: %v", err)
	}

	if diff.Project != "proj1" {
		t.Fatalf("expected proj1, got %s", diff.Project)
	}
}

func TestSyncAgentMd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		result := AgentMdSyncResult{Project: "proj1", Status: "synced"}
		json.NewEncoder(w).Encode(APIResponse{Data: result})
	}))
	defer server.Close()

	client := NewHTTPClient(server.URL, "test-token")
	result, err := SyncAgentMd(client, "proj1")
	if err != nil {
		t.Fatalf("SyncAgentMd failed: %v", err)
	}

	if result.Project != "proj1" {
		t.Fatalf("expected proj1, got %s", result.Project)
	}
	if result.Status != "synced" {
		t.Fatalf("expected synced, got %s", result.Status)
	}
}
