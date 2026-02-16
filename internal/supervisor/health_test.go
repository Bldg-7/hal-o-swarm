package supervisor

import (
	"context"
	"testing"
	"time"
)

func TestHealthCheckerLiveness(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, nil)
	result := hc.CheckLiveness(context.Background())

	if result.Status != HealthHealthy {
		t.Errorf("expected healthy status, got %v", result.Status)
	}
	if len(result.Components) != 0 {
		t.Errorf("expected no components in liveness check, got %d", len(result.Components))
	}
}

func TestHealthCheckerReadinessAllUnavailable(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, nil)
	result := hc.CheckReadiness(context.Background())

	if result.Status != HealthDegraded {
		t.Errorf("expected degraded status, got %v", result.Status)
	}

	if len(result.Components) != 4 {
		t.Errorf("expected 4 components, got %d", len(result.Components))
	}

	if result.Components["database"].Status != StatusUnavailable {
		t.Errorf("expected database unavailable, got %v", result.Components["database"].Status)
	}
}

func TestHealthCheckerReadinessWithDB(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	hc := NewHealthChecker(db, nil, nil, nil)
	result := hc.CheckReadiness(context.Background())

	if result.Components["database"].Status != StatusOK {
		t.Errorf("expected database ok, got %v", result.Components["database"].Status)
	}
}

func TestHealthCheckerReadinessWithHub(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test_token", []string{}, 30*time.Second, 3, nil)
	hc := NewHealthChecker(nil, hub, nil, nil)
	result := hc.CheckReadiness(context.Background())

	if result.Components["websocket_hub"].Status != StatusOK {
		t.Errorf("expected hub ok, got %v", result.Components["websocket_hub"].Status)
	}
}

func TestHealthCheckerReadinessWithDispatcher(t *testing.T) {
	dispatcher := &CommandDispatcher{}
	hc := NewHealthChecker(nil, nil, dispatcher, nil)
	result := hc.CheckReadiness(context.Background())

	if result.Components["command_dispatcher"].Status != StatusOK {
		t.Errorf("expected dispatcher ok, got %v", result.Components["command_dispatcher"].Status)
	}
}

func TestHealthCheckerReadinessWithCosts(t *testing.T) {
	costs := &CostAggregator{}
	hc := NewHealthChecker(nil, nil, nil, costs)
	result := hc.CheckReadiness(context.Background())

	if result.Components["cost_aggregator"].Status != StatusOK {
		t.Errorf("expected costs ok, got %v", result.Components["cost_aggregator"].Status)
	}
}

func TestHealthCheckerReadinessAllHealthy(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx, "test_token", []string{}, 30*time.Second, 3, nil)
	dispatcher := &CommandDispatcher{}
	costs := &CostAggregator{}

	hc := NewHealthChecker(db, hub, dispatcher, costs)
	result := hc.CheckReadiness(context.Background())

	if result.Status != HealthHealthy {
		t.Errorf("expected healthy status, got %v", result.Status)
	}

	for name, comp := range result.Components {
		if comp.Status != StatusOK {
			t.Errorf("expected %s to be ok, got %v", name, comp.Status)
		}
	}
}

func TestHealthCheckerTimestamp(t *testing.T) {
	hc := NewHealthChecker(nil, nil, nil, nil)
	before := time.Now().UTC()
	result := hc.CheckReadiness(context.Background())
	after := time.Now().UTC()

	if result.Timestamp.Before(before) || result.Timestamp.After(after.Add(1*time.Second)) {
		t.Errorf("timestamp out of range: %v (expected between %v and %v)", result.Timestamp, before, after)
	}
}

func TestHealthCheckerContextTimeout(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	hc := NewHealthChecker(db, nil, nil, nil)
	result := hc.CheckReadiness(ctx)

	if result.Status == HealthHealthy {
		t.Errorf("expected non-healthy status with timeout context")
	}
}
