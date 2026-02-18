package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/config"
	"go.uber.org/zap"
)

const (
	providerAnthropic = "anthropic"
	providerOpenAI    = "openai"
)

type CostReport struct {
	Period            string          `json:"period"`
	TotalTokens       int64           `json:"total_tokens"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	ByProvider        []CostBreakdown `json:"by_provider"`
	ByModel           []CostBreakdown `json:"by_model"`
	ByProject         []ProjectCost   `json:"by_project"`
	DegradedProviders []string        `json:"degraded_providers"`
}

type CostBreakdown struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model,omitempty"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

type ProjectCost struct {
	Project string  `json:"project"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

type usageRow struct {
	Provider string
	Model    string
	Project  string
	Date     string
	Tokens   int64
	CostUSD  float64
}

type CostAggregator struct {
	cfg     config.CostConfig
	db      *sql.DB
	tracker *SessionTracker
	logger  *zap.Logger

	httpClient *http.Client
	now        func() time.Time
	sleep      func(time.Duration)

	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	degraded map[string]string
}

func NewCostAggregator(cfg config.CostConfig, db *sql.DB, tracker *SessionTracker, logger *zap.Logger) *CostAggregator {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.PollIntervalMinutes <= 0 {
		cfg.PollIntervalMinutes = 60
	}
	if cfg.RequestTimeoutSec <= 0 {
		cfg.RequestTimeoutSec = 15
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BackoffBaseMS <= 0 {
		cfg.BackoffBaseMS = 500
	}

	return &CostAggregator{
		cfg:        cfg,
		db:         db,
		tracker:    tracker,
		logger:     logger,
		httpClient: &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second},
		now:        func() time.Time { return time.Now().UTC() },
		sleep:      time.Sleep,
		degraded:   make(map[string]string),
	}
}

func (c *CostAggregator) Start(parent context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	c.wg.Add(1)
	go c.run(ctx)
}

func (c *CostAggregator) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		c.logger.Warn("cost aggregator stop timeout")
	}
}

func (c *CostAggregator) run(ctx context.Context) {
	defer c.wg.Done()

	interval := time.Duration(c.cfg.PollIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.pollCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollCycle(ctx)
		}
	}
}

func (c *CostAggregator) PollOnce(ctx context.Context) {
	c.pollCycle(ctx)
}

func (c *CostAggregator) pollCycle(ctx context.Context) {
	providers := map[string]config.CostProviderConfig{
		providerAnthropic: c.cfg.Providers.Anthropic,
		providerOpenAI:    c.cfg.Providers.OpenAI,
	}

	var wg sync.WaitGroup
	for provider, providerCfg := range providers {
		if !providerCfg.IsEnabled() {
			continue
		}
		wg.Add(1)
		go func(provider string, providerCfg config.CostProviderConfig) {
			defer wg.Done()
			c.pollProvider(ctx, provider, providerCfg)
		}(provider, providerCfg)
	}
	wg.Wait()
}

func (c *CostAggregator) pollProvider(ctx context.Context, provider string, providerCfg config.CostProviderConfig) {
	rows, err := c.fetchProviderWithRetry(ctx, provider, providerCfg)
	if err != nil {
		c.setDegraded(provider, err.Error())
		c.logger.Warn("cost provider polling degraded",
			zap.String("provider", provider),
			zap.String("reason", err.Error()),
		)
		c.applySessionEstimateFallback(provider)
		return
	}

	c.clearDegraded(provider)
	if err := c.persistDailyBuckets(rows); err != nil {
		c.logger.Warn("failed to persist cost buckets", zap.String("provider", provider), zap.Error(err))
		return
	}
}

func (c *CostAggregator) fetchProviderWithRetry(ctx context.Context, provider string, providerCfg config.CostProviderConfig) ([]usageRow, error) {
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		rows, retryable, err := c.fetchProviderUsage(ctx, provider, providerCfg)
		if err == nil {
			return rows, nil
		}

		lastErr = err
		if !retryable || attempt == c.cfg.MaxRetries {
			break
		}

		backoff := time.Duration(float64(c.cfg.BackoffBaseMS)*math.Pow(2, float64(attempt))) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			c.sleep(backoff)
		}
	}

	return nil, lastErr
}

func (c *CostAggregator) fetchProviderUsage(ctx context.Context, provider string, providerCfg config.CostProviderConfig) ([]usageRow, bool, error) {
	endpoint, query := c.providerRequest(provider, providerCfg)
	if endpoint == "" {
		return nil, false, fmt.Errorf("provider endpoint is empty")
	}

	if len(query) > 0 {
		if strings.Contains(endpoint, "?") {
			endpoint += "&" + query.Encode()
		} else {
			endpoint += "?" + query.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}

	apiKey := providerCfg.EffectiveAPIKey()
	switch provider {
	case providerAnthropic:
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case providerOpenAI:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, true, fmt.Errorf("read response: %w", readErr)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	parsedRows, parseErr := parseProviderUsage(provider, body)
	if parseErr != nil {
		return nil, false, parseErr
	}

	return parsedRows, false, nil
}

func (c *CostAggregator) providerRequest(provider string, providerCfg config.CostProviderConfig) (string, url.Values) {
	now := c.now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	if providerCfg.BaseURL != "" {
		return providerCfg.BaseURL, nil
	}

	query := url.Values{}
	switch provider {
	case providerAnthropic:
		query.Set("starting_at", start.Format(time.RFC3339))
		query.Set("ending_at", end.Format(time.RFC3339))
		query.Set("bucket_width", "1d")
		query.Add("group_by[]", "model")
		return "https://api.anthropic.com/v1/organizations/usage_report/messages", query
	case providerOpenAI:
		query.Set("start_date", start.Format("2006-01-02"))
		query.Set("end_date", end.Format("2006-01-02"))
		query.Set("bucket_width", "1d")
		query.Add("group_by[]", "model")
		query.Add("group_by[]", "project_id")
		return "https://api.openai.com/v1/organization/usage/completions", query
	default:
		return "", nil
	}
}

func parseProviderUsage(provider string, body []byte) ([]usageRow, error) {
	switch provider {
	case providerAnthropic:
		return parseAnthropicUsage(body)
	case providerOpenAI:
		return parseOpenAIUsage(body)
	default:
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
}

func parseAnthropicUsage(body []byte) ([]usageRow, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}

	var rows []usageRow
	if data, ok := raw["usage"]; ok {
		var usage []struct {
			Date    string  `json:"date"`
			Model   string  `json:"model"`
			Tokens  int64   `json:"tokens"`
			CostUSD float64 `json:"cost_usd"`
		}
		if err := json.Unmarshal(data, &usage); err != nil {
			return nil, fmt.Errorf("decode anthropic usage: %w", err)
		}
		for _, row := range usage {
			rows = append(rows, usageRow{
				Provider: providerAnthropic,
				Model:    row.Model,
				Date:     normalizeDate(row.Date),
				Tokens:   row.Tokens,
				CostUSD:  row.CostUSD,
			})
		}
		return rows, nil
	}

	if data, ok := raw["data"]; ok {
		var usage []map[string]interface{}
		if err := json.Unmarshal(data, &usage); err != nil {
			return nil, fmt.Errorf("decode anthropic data: %w", err)
		}
		for _, item := range usage {
			tokens := asInt64(item["tokens"])
			if tokens == 0 {
				tokens = asInt64(item["total_tokens"])
			}
			cost := asFloat64(item["cost_usd"])
			if cost == 0 {
				cost = asFloat64(item["total_cost"])
			}
			rows = append(rows, usageRow{
				Provider: providerAnthropic,
				Model:    asString(item["model"]),
				Date:     normalizeDate(asString(item["date"]), asString(item["time"]), asString(item["timestamp"]), asString(item["created_at"])),
				Tokens:   tokens,
				CostUSD:  cost,
			})
		}
	}

	return rows, nil
}

func parseOpenAIUsage(body []byte) ([]usageRow, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	data, ok := raw["data"]
	if !ok {
		return nil, nil
	}

	var usage []map[string]interface{}
	if err := json.Unmarshal(data, &usage); err != nil {
		return nil, fmt.Errorf("decode openai data: %w", err)
	}

	rows := make([]usageRow, 0, len(usage))
	for _, item := range usage {
		model := asString(item["model"])
		project := asString(item["project"], item["project_id"])
		tokens := asInt64(item["tokens"])
		if tokens == 0 {
			tokens = asInt64(item["total_tokens"], item["input_tokens"])
		}
		cost := asFloat64(item["cost_usd"], item["cost"])
		rows = append(rows, usageRow{
			Provider: providerOpenAI,
			Model:    model,
			Project:  project,
			Date:     normalizeDate(asString(item["date"]), asString(item["time"]), asString(item["timestamp"]), asString(item["start_time"])),
			Tokens:   tokens,
			CostUSD:  cost,
		})
	}

	return rows, nil
}

func (c *CostAggregator) persistDailyBuckets(rows []usageRow) error {
	if c.db == nil || len(rows) == 0 {
		return nil
	}

	combined := map[string]usageRow{}
	for _, row := range rows {
		if row.Date == "" {
			row.Date = c.now().Format("2006-01-02")
		}
		if row.Model == "" {
			row.Model = "unknown"
		}
		key := row.Provider + "|" + row.Model + "|" + row.Date
		current := combined[key]
		current.Provider = row.Provider
		current.Model = row.Model
		current.Date = row.Date
		current.Tokens += row.Tokens
		current.CostUSD += row.CostUSD
		combined[key] = current
	}

	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin costs transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO costs (id, provider, model, date, tokens, cost_usd)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			provider = excluded.provider,
			model = excluded.model,
			date = excluded.date,
			tokens = excluded.tokens,
			cost_usd = excluded.cost_usd
	`)
	if err != nil {
		return fmt.Errorf("prepare cost upsert: %w", err)
	}
	defer stmt.Close()

	for _, row := range combined {
		id := row.Provider + "|" + row.Model + "|" + row.Date
		if _, err := stmt.Exec(id, row.Provider, row.Model, row.Date, row.Tokens, row.CostUSD); err != nil {
			return fmt.Errorf("upsert cost row %s: %w", id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit costs transaction: %w", err)
	}

	return nil
}

func (c *CostAggregator) applySessionEstimateFallback(provider string) {
	if c.tracker == nil {
		return
	}

	sessions := c.tracker.GetAllSessions()
	for _, session := range sessions {
		sessionProvider, ok := c.providerForModel(session.Model)
		if !ok || sessionProvider != provider {
			continue
		}

		rate := c.modelRate(provider, session.Model)
		if rate.Input == 0 && rate.Output == 0 {
			continue
		}

		ratePerK := rate.Input
		if rate.Output > 0 {
			if ratePerK > 0 {
				ratePerK = (rate.Input + rate.Output) / 2
			} else {
				ratePerK = rate.Output
			}
		}

		estimate := (float64(session.TokenUsage.Total) / 1000) * ratePerK
		if estimate <= 0 {
			continue
		}

		if err := c.tracker.UpdateSession(session.SessionID, map[string]interface{}{
			"session_cost": estimate,
		}); err != nil {
			c.logger.Warn("failed to apply session fallback estimate", zap.String("session_id", session.SessionID), zap.Error(err))
		}
	}
}

func (c *CostAggregator) providerForModel(model string) (string, bool) {
	if model == "" {
		return "", false
	}
	if _, ok := c.cfg.Providers.Anthropic.ModelRates[model]; ok {
		return providerAnthropic, true
	}
	if _, ok := c.cfg.Providers.OpenAI.ModelRates[model]; ok {
		return providerOpenAI, true
	}
	return "", false
}

func (c *CostAggregator) modelRate(provider string, model string) config.ModelRate {
	switch provider {
	case providerAnthropic:
		return c.cfg.Providers.Anthropic.ModelRates[model]
	case providerOpenAI:
		return c.cfg.Providers.OpenAI.ModelRates[model]
	default:
		return config.ModelRate{}
	}
}

func (c *CostAggregator) Report(period string) (CostReport, error) {
	start, normalized := periodStart(c.now, period)
	report := CostReport{Period: normalized}

	if c.db != nil {
		providerRows, err := c.queryProviderTotals(start)
		if err != nil {
			return CostReport{}, err
		}
		report.ByProvider = providerRows

		modelRows, err := c.queryModelTotals(start)
		if err != nil {
			return CostReport{}, err
		}
		report.ByModel = modelRows

		for _, row := range modelRows {
			report.TotalTokens += row.Tokens
			report.TotalCostUSD += row.CostUSD
		}
	}

	if c.tracker != nil {
		report.ByProject = c.projectTotals(start)
	}

	report.DegradedProviders = c.DegradedProviders()

	return report, nil
}

func (c *CostAggregator) queryProviderTotals(start time.Time) ([]CostBreakdown, error) {
	rows, err := c.db.Query(`
		SELECT provider, SUM(tokens), SUM(cost_usd)
		FROM costs
		WHERE date >= ?
		GROUP BY provider
		ORDER BY provider ASC
	`, start.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query provider totals: %w", err)
	}
	defer rows.Close()

	result := make([]CostBreakdown, 0)
	for rows.Next() {
		var row CostBreakdown
		if err := rows.Scan(&row.Provider, &row.Tokens, &row.CostUSD); err != nil {
			return nil, fmt.Errorf("scan provider total: %w", err)
		}
		result = append(result, row)
	}

	return result, rows.Err()
}

func (c *CostAggregator) queryModelTotals(start time.Time) ([]CostBreakdown, error) {
	rows, err := c.db.Query(`
		SELECT provider, model, SUM(tokens), SUM(cost_usd)
		FROM costs
		WHERE date >= ?
		GROUP BY provider, model
		ORDER BY provider ASC, model ASC
	`, start.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query model totals: %w", err)
	}
	defer rows.Close()

	result := make([]CostBreakdown, 0)
	for rows.Next() {
		var row CostBreakdown
		if err := rows.Scan(&row.Provider, &row.Model, &row.Tokens, &row.CostUSD); err != nil {
			return nil, fmt.Errorf("scan model total: %w", err)
		}
		result = append(result, row)
	}

	return result, rows.Err()
}

func (c *CostAggregator) projectTotals(start time.Time) []ProjectCost {
	totals := map[string]ProjectCost{}
	for _, session := range c.tracker.GetAllSessions() {
		if session.StartedAt.Before(start) {
			continue
		}
		item := totals[session.Project]
		item.Project = session.Project
		item.Tokens += int64(session.TokenUsage.Total)
		item.CostUSD += session.SessionCost
		totals[session.Project] = item
	}

	out := make([]ProjectCost, 0, len(totals))
	for _, value := range totals {
		out = append(out, value)
	}
	return out
}

func (c *CostAggregator) DegradedProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]string, 0, len(c.degraded))
	for provider := range c.degraded {
		out = append(out, provider)
	}
	return out
}

func (c *CostAggregator) setDegraded(provider string, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.degraded[provider] = reason
}

func (c *CostAggregator) clearDegraded(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.degraded, provider)
}

func periodStart(nowFn func() time.Time, period string) (time.Time, string) {
	now := nowFn().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	switch strings.ToLower(period) {
	case "week":
		return startOfDay.AddDate(0, 0, -6), "week"
	case "month":
		return startOfDay.AddDate(0, 0, -29), "month"
	default:
		return startOfDay, "today"
	}
}

func normalizeDate(values ...string) string {
	for _, raw := range values {
		if raw == "" {
			continue
		}
		if len(raw) >= 10 {
			tail := raw[:10]
			if _, err := time.Parse("2006-01-02", tail); err == nil {
				return tail
			}
		}
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			return parsed.UTC().Format("2006-01-02")
		}
		if epoch, err := strconv.ParseInt(raw, 10, 64); err == nil && epoch > 0 {
			return time.Unix(epoch, 0).UTC().Format("2006-01-02")
		}
	}
	return ""
}

func asString(values ...interface{}) string {
	for _, value := range values {
		s, ok := value.(string)
		if ok && s != "" {
			return s
		}
	}
	return ""
}

func asInt64(values ...interface{}) int64 {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return int64(typed)
		case int64:
			return typed
		case int:
			return int64(typed)
		case json.Number:
			parsed, err := typed.Int64()
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func asFloat64(values ...interface{}) float64 {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return typed
		case int:
			return float64(typed)
		case int64:
			return float64(typed)
		case json.Number:
			parsed, err := typed.Float64()
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}
