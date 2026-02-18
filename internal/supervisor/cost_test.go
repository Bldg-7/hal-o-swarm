package supervisor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"go.uber.org/zap"
)

func TestCostAggregatorPoll(t *testing.T) {
	db := setupSupervisorTestDB(t)
	tracker := NewSessionTracker(db, zap.NewNop())

	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"usage": []map[string]interface{}{
				{"date": "2026-02-16", "model": "claude-sonnet-4", "tokens": 1234, "cost_usd": 0.45},
			},
		})
	}))
	defer anthropic.Close()

	openai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"date": "2026-02-16", "model": "gpt-4", "tokens": 2300, "cost_usd": 0.87, "project_id": "proj-a"},
			},
		})
	}))
	defer openai.Close()

	cfg := config.CostConfig{
		PollIntervalMinutes: 60,
		RequestTimeoutSec:   3,
		MaxRetries:          1,
		BackoffBaseMS:       1,
		Providers: config.CostProviders{
			Anthropic: config.CostProviderConfig{
				APIKey:  "sk-ant-admin-test",
				BaseURL: anthropic.URL,
				ModelRates: map[string]config.ModelRate{
					"claude-sonnet-4": {Input: 0.003, Output: 0.015},
				},
			},
			OpenAI: config.CostProviderConfig{
				APIKey:  "sk-openai-test",
				BaseURL: openai.URL,
				ModelRates: map[string]config.ModelRate{
					"gpt-4": {Input: 0.03, Output: 0.06},
				},
			},
		},
	}

	agg := NewCostAggregator(cfg, db, tracker, zap.NewNop())
	agg.now = func() time.Time { return time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC) }
	agg.PollOnce(context.Background())

	rows, err := db.Query(`SELECT provider, model, date, tokens, cost_usd FROM costs ORDER BY provider, model`)
	if err != nil {
		t.Fatalf("query costs: %v", err)
	}
	defer rows.Close()

	type row struct {
		provider string
		model    string
		date     string
		tokens   int64
		costUSD  float64
	}
	actual := make([]row, 0)
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.provider, &r.model, &r.date, &r.tokens, &r.costUSD); err != nil {
			t.Fatalf("scan cost row: %v", err)
		}
		actual = append(actual, r)
	}

	if len(actual) != 2 {
		t.Fatalf("expected 2 cost rows, got %d", len(actual))
	}
	if actual[0].provider != providerAnthropic || actual[0].model != "claude-sonnet-4" || actual[0].tokens != 1234 {
		t.Fatalf("unexpected anthropic row: %+v", actual[0])
	}
	if actual[1].provider != providerOpenAI || actual[1].model != "gpt-4" || actual[1].tokens != 2300 {
		t.Fatalf("unexpected openai row: %+v", actual[1])
	}
}

func TestCostAggregatorDegraded(t *testing.T) {
	db := setupSupervisorTestDB(t)
	tracker := NewSessionTracker(db, zap.NewNop())

	if _, err := db.Exec(`INSERT INTO nodes (id, hostname, status, last_heartbeat) VALUES (?, ?, ?, ?)`, "n-1", "node-1", "online", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID:  "s-1",
		NodeID:     "n-1",
		Project:    "proj-a",
		Status:     SessionStatusRunning,
		TokenUsage: TokenUsage{Total: 4000},
		Model:      "claude-sonnet-4",
		StartedAt:  time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("add session: %v", err)
	}

	var anthropicCalls atomic.Int32
	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "rate_limited"})
	}))
	defer anthropic.Close()

	openai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]interface{}{
				{"date": "2026-02-16", "model": "gpt-4", "tokens": 2000, "cost_usd": 0.6},
			},
		})
	}))
	defer openai.Close()

	cfg := config.CostConfig{
		PollIntervalMinutes: 60,
		RequestTimeoutSec:   3,
		MaxRetries:          2,
		BackoffBaseMS:       1,
		Providers: config.CostProviders{
			Anthropic: config.CostProviderConfig{
				APIKey:  "sk-ant-admin-test",
				BaseURL: anthropic.URL,
				ModelRates: map[string]config.ModelRate{
					"claude-sonnet-4": {Input: 0.003, Output: 0.015},
				},
			},
			OpenAI: config.CostProviderConfig{
				APIKey:  "sk-openai-test",
				BaseURL: openai.URL,
				ModelRates: map[string]config.ModelRate{
					"gpt-4": {Input: 0.03, Output: 0.06},
				},
			},
		},
	}

	agg := NewCostAggregator(cfg, db, tracker, zap.NewNop())
	agg.now = func() time.Time { return time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC) }
	agg.sleep = func(time.Duration) {}
	agg.PollOnce(context.Background())

	if anthropicCalls.Load() < 2 {
		t.Fatalf("expected anthropic retries, got %d calls", anthropicCalls.Load())
	}

	degraded := agg.DegradedProviders()
	if !slices.Contains(degraded, providerAnthropic) {
		t.Fatalf("expected anthropic in degraded providers, got %v", degraded)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM costs WHERE provider = ?`, providerOpenAI).Scan(&count); err != nil {
		t.Fatalf("count openai rows: %v", err)
	}
	if count == 0 {
		t.Fatal("expected openai row inserted even when anthropic is degraded")
	}

	session, err := tracker.GetSession("s-1")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.SessionCost <= 0 {
		t.Fatalf("expected fallback estimate to update session cost, got %f", session.SessionCost)
	}
}

func TestCostReport(t *testing.T) {
	db := setupSupervisorTestDB(t)
	tracker := NewSessionTracker(db, zap.NewNop())

	now := time.Date(2026, 2, 16, 11, 0, 0, 0, time.UTC)

	if _, err := db.Exec(`
		INSERT INTO costs (id, provider, model, date, tokens, cost_usd)
		VALUES
			('anthropic|claude-sonnet-4|2026-02-16', 'anthropic', 'claude-sonnet-4', '2026-02-16', 1000, 0.3),
			('openai|gpt-4|2026-02-16', 'openai', 'gpt-4', '2026-02-16', 2000, 0.8)
	`); err != nil {
		t.Fatalf("insert costs: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO nodes (id, hostname, status, last_heartbeat) VALUES (?, ?, ?, ?)`, "n-1", "node-1", "online", now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tracker.AddSession(TrackedSession{
		SessionID:   "s-1",
		NodeID:      "n-1",
		Project:     "proj-a",
		Status:      SessionStatusRunning,
		TokenUsage:  TokenUsage{Total: 1200},
		SessionCost: 0.21,
		StartedAt:   now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("add session: %v", err)
	}

	agg := NewCostAggregator(config.CostConfig{}, db, tracker, zap.NewNop())
	agg.now = func() time.Time { return now }

	report, err := agg.Report("today")
	if err != nil {
		t.Fatalf("report today: %v", err)
	}

	if report.TotalTokens != 3000 {
		t.Fatalf("expected total tokens 3000, got %d", report.TotalTokens)
	}
	if len(report.ByProvider) != 2 {
		t.Fatalf("expected 2 provider rows, got %d", len(report.ByProvider))
	}
	if len(report.ByModel) != 2 {
		t.Fatalf("expected 2 model rows, got %d", len(report.ByModel))
	}
	if len(report.ByProject) != 1 || report.ByProject[0].Project != "proj-a" {
		t.Fatalf("unexpected project totals: %+v", report.ByProject)
	}
}
