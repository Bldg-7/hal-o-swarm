package supervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleLiveness(t *testing.T) {
	api := NewHTTPAPI(nil, nil, nil, nil, "test_token", nil)
	hc := NewHealthChecker(nil, nil, nil, nil)
	api.SetHealthChecker(hc)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	api.handleLiveness(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result HealthCheckResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status != HealthHealthy {
		t.Errorf("expected status 'healthy', got %v", result.Status)
	}
}

func TestHandleReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test_token", []string{}, 30*time.Second, 3, nil)
	api := NewHTTPAPI(nil, nil, nil, nil, "test_token", nil)
	hc := NewHealthChecker(nil, hub, nil, nil)
	api.SetHealthChecker(hc)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	api.handleReadiness(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 200 or 503, got %d", w.Code)
	}

	var result HealthCheckResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.Status == "" {
		t.Errorf("expected status to be set")
	}
}

func TestMetricsEndpoint(t *testing.T) {
	api := NewHTTPAPI(nil, nil, nil, nil, "test_token", nil)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if w.Header().Get("Content-Type") == "" {
		t.Errorf("expected Content-Type header to be set")
	}
}

func TestHealthzEndpoint(t *testing.T) {
	api := NewHTTPAPI(nil, nil, nil, nil, "test_token", nil)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestReadyzEndpoint(t *testing.T) {
	api := NewHTTPAPI(nil, nil, nil, nil, "test_token", nil)
	handler := api.Handler()

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 200 or 503, got %d", w.Code)
	}
}

func TestHealthCheckResultJSON(t *testing.T) {
	result := HealthCheckResult{
		Status: HealthHealthy,
		Components: map[string]ComponentHealth{
			"database": {Status: StatusOK},
			"hub":      {Status: StatusOK},
		},
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal result: %v", err)
	}

	var decoded HealthCheckResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if decoded.Status != HealthHealthy {
		t.Errorf("expected status %v, got %v", HealthHealthy, decoded.Status)
	}
}
