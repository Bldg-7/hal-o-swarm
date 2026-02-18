package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type HTTPAPI struct {
	registry      *NodeRegistry
	tracker       *SessionTracker
	dispatcher    *CommandDispatcher
	hub           *Hub
	oauth         *OAuthOrchestrator
	costs         *CostAggregator
	db            *sql.DB
	authToken     string
	logger        *zap.Logger
	healthChecker *HealthChecker
	metrics       *Metrics
	auditLogger   *AuditLogger

	credentialIdempotencyMu    sync.Mutex
	credentialIdempotencyCache map[string]*CommandResult
	credentialIdempotencyOrder []string
}

const maxCredentialIdempotencyEntries = 1000

func NewHTTPAPI(
	registry *NodeRegistry,
	tracker *SessionTracker,
	dispatcher *CommandDispatcher,
	db *sql.DB,
	authToken string,
	logger *zap.Logger,
) *HTTPAPI {
	if logger == nil {
		logger = zap.NewNop()
	}
	var oauthOrchestrator *OAuthOrchestrator
	if dispatcher != nil {
		oauthOrchestrator = NewOAuthOrchestrator(dispatcher, logger)
	}
	return &HTTPAPI{
		registry:                   registry,
		tracker:                    tracker,
		dispatcher:                 dispatcher,
		oauth:                      oauthOrchestrator,
		db:                         db,
		authToken:                  authToken,
		logger:                     logger,
		metrics:                    GetMetrics(),
		credentialIdempotencyCache: make(map[string]*CommandResult),
	}
}

func (a *HTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", a.handleHealth)
	mux.HandleFunc("GET /healthz", a.handleLiveness)
	mux.HandleFunc("GET /readyz", a.handleReadiness)
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.Handle("GET /api/v1/sessions", a.requireAuth(http.HandlerFunc(a.handleListSessions)))
	mux.Handle("GET /api/v1/sessions/{id}", a.requireAuth(http.HandlerFunc(a.handleGetSession)))
	mux.Handle("GET /api/v1/nodes", a.requireAuth(http.HandlerFunc(a.handleListNodes)))
	mux.Handle("GET /api/v1/nodes/{id}", a.requireAuth(http.HandlerFunc(a.handleGetNode)))
	mux.Handle("GET /api/v1/nodes/{id}/auth", a.requireAuth(http.HandlerFunc(a.handleNodeAuth)))
	mux.Handle("GET /api/v1/auth/drift", a.requireAuth(http.HandlerFunc(a.handleAuthDrift)))
	mux.Handle("GET /api/v1/events", a.requireAuth(http.HandlerFunc(a.handleListEvents)))
	mux.Handle("GET /api/v1/cost", a.requireAuth(http.HandlerFunc(a.handleCostReport)))
	mux.Handle("GET /api/v1/cost/{period}", a.requireAuth(http.HandlerFunc(a.handleCostReportByPath)))
	mux.Handle("GET /api/v1/env/status/{project}", a.requireAuth(http.HandlerFunc(a.handleEnvStatus)))
	mux.Handle("GET /api/v1/agentmd/diff/{project}", a.requireAuth(http.HandlerFunc(a.handleAgentMDDiff)))
	mux.Handle("POST /api/v1/commands", a.requireAuth(http.HandlerFunc(a.handleCommand)))
	mux.Handle("POST /api/v1/commands/credentials/push", a.requireAuth(http.HandlerFunc(a.handleCredentialPush)))
	mux.Handle("POST /api/v1/oauth/trigger", a.requireAuth(http.HandlerFunc(a.handleOAuthTrigger)))
	if a.hub != nil {
		mux.HandleFunc("GET /ws/agent", a.hub.ServeWS)
	}

	return mux
}

func (a *HTTPAPI) SetCostAggregator(aggregator *CostAggregator) {
	a.costs = aggregator
}

func (a *HTTPAPI) SetHealthChecker(hc *HealthChecker) {
	a.healthChecker = hc
}

func (a *HTTPAPI) SetAuditLogger(al *AuditLogger) {
	a.auditLogger = al
}

func (a *HTTPAPI) SetOAuthOrchestrator(orchestrator *OAuthOrchestrator) {
	a.oauth = orchestrator
}

func (a *HTTPAPI) SetHub(hub *Hub) {
	a.hub = hub
}

type apiResponse struct {
	Data interface{} `json:"data"`
	Meta *apiMeta    `json:"meta,omitempty"`
}

type apiMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func (a *HTTPAPI) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if token == "" || token != a.authToken {
			writeError(w, http.StatusUnauthorized, "unauthorized", "AUTH_REQUIRED")
			return
		}

		next.ServeHTTP(w, r)
	})
}

type healthResponse struct {
	Status     string            `json:"status"`
	Components map[string]string `json:"components"`
	Timestamp  time.Time         `json:"timestamp"`
}

func (a *HTTPAPI) handleLiveness(w http.ResponseWriter, r *http.Request) {
	if a.healthChecker == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
		return
	}

	result := a.healthChecker.CheckLiveness(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if result.Status == HealthHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(result)
}

func (a *HTTPAPI) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if a.healthChecker == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}

	result := a.healthChecker.CheckReadiness(r.Context())
	w.Header().Set("Content-Type", "application/json")
	statusCode := http.StatusOK
	if result.Status == HealthDegraded {
		statusCode = http.StatusServiceUnavailable
	} else if result.Status == HealthUnhealthy {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(result)
}

func (a *HTTPAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	components := map[string]string{
		"database":           a.checkDBHealth(),
		"websocket_hub":      "ok",
		"command_dispatcher": a.checkDispatcherHealth(),
	}

	status := "healthy"
	for _, v := range components {
		if v != "ok" {
			status = "degraded"
			break
		}
	}

	writeJSON(w, http.StatusOK, healthResponse{
		Status:     status,
		Components: components,
		Timestamp:  time.Now().UTC(),
	})
}

func (a *HTTPAPI) checkDBHealth() string {
	if a.db == nil {
		return "unavailable"
	}
	if err := a.db.Ping(); err != nil {
		return "unavailable"
	}
	return "ok"
}

func (a *HTTPAPI) checkDispatcherHealth() string {
	if a.dispatcher == nil {
		return "unavailable"
	}
	return "ok"
}

type sessionJSON struct {
	ID        string        `json:"id"`
	NodeID    string        `json:"node_id"`
	Project   string        `json:"project"`
	Status    SessionStatus `json:"status"`
	Tokens    int           `json:"tokens"`
	Cost      float64       `json:"cost"`
	StartedAt time.Time     `json:"started_at"`
}

func toSessionJSON(s TrackedSession) sessionJSON {
	return sessionJSON{
		ID:        s.SessionID,
		NodeID:    s.NodeID,
		Project:   s.Project,
		Status:    s.Status,
		Tokens:    s.TokenUsage.Total,
		Cost:      s.SessionCost,
		StartedAt: s.StartedAt,
	}
}

func (a *HTTPAPI) handleListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project := q.Get("project")
	status := q.Get("status")
	nodeID := q.Get("node_id")
	limit := parseIntParam(q.Get("limit"), 100)

	var sessions []TrackedSession
	if project != "" {
		sessions = a.tracker.GetSessionsByProject(project)
	} else {
		sessions = a.tracker.GetAllSessions()
	}

	filtered := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		if status != "" && string(s.Status) != status {
			continue
		}
		if nodeID != "" && s.NodeID != nodeID {
			continue
		}
		filtered = append(filtered, toSessionJSON(s))
		if len(filtered) >= limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: filtered,
		Meta: &apiMeta{
			Total:  len(filtered),
			Limit:  limit,
			Offset: 0,
		},
	})
}

func (a *HTTPAPI) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, err := a.tracker.GetSession(id)
	if err != nil {
		if err == ErrSessionNotFound {
			writeError(w, http.StatusNotFound, "session not found", "NOT_FOUND")
			return
		}
		a.logger.Error("get session failed", zap.String("session_id", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: toSessionJSON(session)})
}

type nodeJSON struct {
	ID            string     `json:"id"`
	Hostname      string     `json:"hostname"`
	Status        NodeStatus `json:"status"`
	LastHeartbeat time.Time  `json:"last_heartbeat"`
	ConnectedAt   time.Time  `json:"connected_at"`
}

func toNodeJSON(n NodeEntry) nodeJSON {
	return nodeJSON{
		ID:            n.ID,
		Hostname:      n.Hostname,
		Status:        n.Status,
		LastHeartbeat: n.LastHeartbeat,
		ConnectedAt:   n.ConnectedAt,
	}
}

func (a *HTTPAPI) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := a.registry.ListNodes()

	out := make([]nodeJSON, len(nodes))
	for i, n := range nodes {
		out[i] = toNodeJSON(n)
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: out,
		Meta: &apiMeta{Total: len(out)},
	})
}

func (a *HTTPAPI) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := a.registry.GetNode(id)
	if err != nil {
		if err == ErrNodeNotFound {
			writeError(w, http.StatusNotFound, "node not found", "NOT_FOUND")
			return
		}
		a.logger.Error("get node failed", zap.String("node_id", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: toNodeJSON(node)})
}

type nodeAuthJSON struct {
	NodeID            string                   `json:"node_id"`
	AuthStates        map[string]NodeAuthState `json:"auth_states"`
	CredentialSync    CredentialSyncStatus     `json:"credential_sync"`
	CredentialVersion int                      `json:"credential_version"`
}

func (a *HTTPAPI) handleNodeAuth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, err := a.registry.GetNode(id)
	if err != nil {
		if err == ErrNodeNotFound {
			writeError(w, http.StatusNotFound, "node not found", "NOT_FOUND")
			return
		}
		a.logger.Error("get node failed", zap.String("node_id", id), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	authStates := a.registry.GetAuthState(id)

	writeJSON(w, http.StatusOK, apiResponse{
		Data: nodeAuthJSON{
			NodeID:            node.ID,
			AuthStates:        authStates,
			CredentialSync:    node.CredSyncStatus,
			CredentialVersion: node.CredVersion,
		},
	})
}

type driftNodeJSON struct {
	NodeID            string               `json:"node_id"`
	CredentialSync    CredentialSyncStatus `json:"credential_sync"`
	CredentialVersion int                  `json:"credential_version"`
}

func (a *HTTPAPI) handleAuthDrift(w http.ResponseWriter, r *http.Request) {
	nodes := a.registry.ListNodes()

	drifted := make([]driftNodeJSON, 0)
	for _, n := range nodes {
		if n.CredSyncStatus == CredentialSyncStatusDriftDetected {
			drifted = append(drifted, driftNodeJSON{
				NodeID:            n.ID,
				CredentialSync:    n.CredSyncStatus,
				CredentialVersion: n.CredVersion,
			})
		}
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: drifted,
		Meta: &apiMeta{Total: len(drifted)},
	})
}

type eventJSON struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

func (a *HTTPAPI) handleListEvents(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeError(w, http.StatusServiceUnavailable, "event storage unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	q := r.URL.Query()
	sessionID := q.Get("session_id")
	eventType := q.Get("type")
	since := q.Get("since")
	limit := parseIntParam(q.Get("limit"), 100)

	query := `SELECT id, session_id, type, data, timestamp FROM events WHERE 1=1`
	args := make([]interface{}, 0)

	if sessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if eventType != "" {
		query += ` AND type = ?`
		args = append(args, eventType)
	}
	if since != "" {
		query += ` AND timestamp >= ?`
		args = append(args, since)
	}

	query += ` ORDER BY timestamp DESC LIMIT ?`
	args = append(args, limit)

	rows, err := a.db.Query(query, args...)
	if err != nil {
		a.logger.Error("query events failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	events := make([]eventJSON, 0)
	for rows.Next() {
		var (
			id, sid, typ, data, ts string
		)
		if err := rows.Scan(&id, &sid, &typ, &data, &ts); err != nil {
			a.logger.Error("scan event row failed", zap.Error(err))
			continue
		}

		timestamp, _ := parseSQLiteTimestamp(ts)
		events = append(events, eventJSON{
			ID:        id,
			SessionID: sid,
			Type:      typ,
			Data:      json.RawMessage(data),
			Timestamp: timestamp,
		})
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: events,
		Meta: &apiMeta{
			Total:  len(events),
			Limit:  limit,
			Offset: 0,
		},
	})
}

type commandRequest struct {
	Type           string                 `json:"type"`
	Target         string                 `json:"target"`
	Args           map[string]interface{} `json:"args"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Timeout        int                    `json:"timeout"`
}

type commandResultJSON struct {
	CommandID string        `json:"command_id"`
	Status    CommandStatus `json:"status"`
	Output    string        `json:"output"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

type oauthTriggerRequest struct {
	NodeID string                `json:"node_id"`
	Tool   shared.ToolIdentifier `json:"tool"`
}

func (a *HTTPAPI) handleCommand(w http.ResponseWriter, r *http.Request) {
	if a.dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "command dispatcher unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if req.Type == "" {
		writeError(w, http.StatusBadRequest, "type is required", "BAD_REQUEST")
		return
	}

	commandType, err := ParseCommandIntent(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "BAD_REQUEST")
		return
	}

	correlationID := shared.GetCorrelationID(r.Context())
	ctx := shared.WithCorrelationID(r.Context(), correlationID)

	timeout := time.Duration(req.Timeout) * time.Second
	cmd := Command{
		Type:           commandType,
		IdempotencyKey: req.IdempotencyKey,
		Target:         CommandTarget{Project: req.Target},
		Args:           req.Args,
		Timeout:        timeout,
	}

	ctx, cancel := context.WithTimeout(ctx, cmd.EffectiveTimeout()+5*time.Second)
	defer cancel()

	start := time.Now()
	result, err := a.dispatcher.DispatchCommand(ctx, cmd)
	duration := time.Since(start).Seconds()

	if err != nil {
		shared.LogErrorWithContext(ctx, a.logger, "dispatch command failed", err)
		if a.metrics != nil {
			a.metrics.RecordCommand(req.Type, "error")
			a.metrics.RecordError("dispatcher", "dispatch_failed")
		}
		writeError(w, http.StatusInternalServerError, "command dispatch failed", "DISPATCH_ERROR")
		return
	}

	if a.metrics != nil {
		a.metrics.RecordCommand(req.Type, string(result.Status))
		a.metrics.RecordCommandDuration(req.Type, duration)
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: commandResultJSON{
			CommandID: result.CommandID,
			Status:    result.Status,
			Output:    result.Output,
			Error:     result.Error,
			Timestamp: result.Timestamp,
		},
	})
}

func (a *HTTPAPI) handleCredentialPush(w http.ResponseWriter, r *http.Request) {
	if a.dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "command dispatcher unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	var req struct {
		TargetNode     string            `json:"target_node"`
		EnvVars        map[string]string `json:"env_vars"`
		Version        int               `json:"version"`
		IdempotencyKey string            `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	payload := shared.CredentialPushPayload{
		TargetNode: req.TargetNode,
		EnvVars:    req.EnvVars,
		Version:    req.Version,
	}

	if err := payload.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	// Convert env vars to map[string]interface{} so SanitizeArgs can
	// recursively traverse and redact secret-named keys in audit logs.
	envVarsMap := make(map[string]interface{}, len(payload.EnvVars))
	for k, v := range payload.EnvVars {
		envVarsMap[k] = v
	}

	cmd := Command{
		Type:           CommandTypeCredentialPush,
		IdempotencyKey: req.IdempotencyKey,
		Target:         CommandTarget{NodeID: payload.TargetNode},
		Args: map[string]interface{}{
			"env_vars": envVarsMap,
			"version":  payload.Version,
		},
	}

	ctx := r.Context()
	ctx, cancel := context.WithTimeout(ctx, cmd.EffectiveTimeout()+5*time.Second)
	defer cancel()

	start := time.Now()
	result, ok := a.credentialPushIdempotencyGet(req.IdempotencyKey)
	var err error
	if !ok {
		result, err = a.dispatcher.DispatchCommand(ctx, cmd)
		if err == nil {
			a.credentialPushIdempotencySet(req.IdempotencyKey, result)
		}
	}
	elapsed := time.Since(start)

	// Audit the credential push command regardless of outcome
	if a.auditLogger != nil {
		a.auditLogger.LogCommand(cmd, result, "api", r.RemoteAddr, elapsed)
	}

	if err != nil {
		a.logger.Error("credential push dispatch failed", zap.Error(err))
		if a.metrics != nil {
			a.metrics.RecordCommand(string(CommandTypeCredentialPush), "error")
			a.metrics.RecordError("dispatcher", "dispatch_failed")
		}
		writeError(w, http.StatusInternalServerError, "command dispatch failed", "DISPATCH_ERROR")
		return
	}

	if a.metrics != nil {
		a.metrics.RecordCommand(string(CommandTypeCredentialPush), string(result.Status))
		a.metrics.RecordCommandDuration(string(CommandTypeCredentialPush), elapsed.Seconds())
	}

	writeJSON(w, http.StatusOK, apiResponse{
		Data: commandResultJSON{
			CommandID: result.CommandID,
			Status:    result.Status,
			Output:    result.Output,
			Error:     result.Error,
			Timestamp: result.Timestamp,
		},
	})
}

func (a *HTTPAPI) handleOAuthTrigger(w http.ResponseWriter, r *http.Request) {
	if a.oauth == nil {
		writeError(w, http.StatusServiceUnavailable, "oauth orchestrator unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	var req oauthTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body", "BAD_REQUEST")
		return
	}

	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, http.StatusBadRequest, "node_id is required", "BAD_REQUEST")
		return
	}
	if strings.TrimSpace(string(req.Tool)) == "" {
		writeError(w, http.StatusBadRequest, "tool is required", "BAD_REQUEST")
		return
	}

	result, err := a.oauth.TriggerOAuth(r.Context(), req.NodeID, req.Tool)
	if err != nil {
		a.logger.Error("oauth trigger failed",
			zap.String("node_id", req.NodeID),
			zap.String("tool", string(req.Tool)),
			zap.Error(err),
		)
		writeError(w, http.StatusInternalServerError, "oauth trigger failed", "DISPATCH_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: result})
}

func (a *HTTPAPI) handleEnvStatus(w http.ResponseWriter, r *http.Request) {
	if a.dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "command dispatcher unavailable", "SERVICE_UNAVAILABLE")
		return
	}
	project := strings.TrimSpace(r.PathValue("project"))
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required", "BAD_REQUEST")
		return
	}

	output, err := a.dispatchJSONCommand(r.Context(), Command{Type: CommandTypeEnvCheck, Target: CommandTarget{Project: project}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DISPATCH_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: output})
}

func (a *HTTPAPI) handleAgentMDDiff(w http.ResponseWriter, r *http.Request) {
	if a.dispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "command dispatcher unavailable", "SERVICE_UNAVAILABLE")
		return
	}
	project := strings.TrimSpace(r.PathValue("project"))
	if project == "" {
		writeError(w, http.StatusBadRequest, "project is required", "BAD_REQUEST")
		return
	}

	output, err := a.dispatchJSONCommand(r.Context(), Command{Type: CommandTypeAgentMDDiff, Target: CommandTarget{Project: project}})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), "DISPATCH_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: output})
}

func (a *HTTPAPI) dispatchJSONCommand(ctx context.Context, cmd Command) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, cmd.EffectiveTimeout()+5*time.Second)
	defer cancel()

	result, err := a.dispatcher.DispatchCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("command dispatch failed: %w", err)
	}
	if result.Status != CommandStatusSuccess {
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return nil, fmt.Errorf("command failed with status %s", result.Status)
	}

	decoded := make(map[string]interface{})
	if result.Output == "" {
		return decoded, nil
	}
	if err := json.Unmarshal([]byte(result.Output), &decoded); err != nil {
		return nil, fmt.Errorf("invalid command output: %w", err)
	}

	return decoded, nil
}

func (a *HTTPAPI) credentialPushIdempotencyGet(key string) (*CommandResult, bool) {
	if key == "" {
		return nil, false
	}

	a.credentialIdempotencyMu.Lock()
	defer a.credentialIdempotencyMu.Unlock()

	result, ok := a.credentialIdempotencyCache[key]
	if !ok || result == nil {
		return nil, false
	}

	copy := *result
	return &copy, true
}

func (a *HTTPAPI) credentialPushIdempotencySet(key string, result *CommandResult) {
	if key == "" || result == nil {
		return
	}

	a.credentialIdempotencyMu.Lock()
	defer a.credentialIdempotencyMu.Unlock()

	resultCopy := *result
	if _, exists := a.credentialIdempotencyCache[key]; !exists {
		a.credentialIdempotencyOrder = append(a.credentialIdempotencyOrder, key)
		if len(a.credentialIdempotencyOrder) > maxCredentialIdempotencyEntries {
			evicted := a.credentialIdempotencyOrder[0]
			a.credentialIdempotencyOrder = a.credentialIdempotencyOrder[1:]
			delete(a.credentialIdempotencyCache, evicted)
		}
	}
	a.credentialIdempotencyCache[key] = &resultCopy
}

func (a *HTTPAPI) handleCostReport(w http.ResponseWriter, r *http.Request) {
	if a.costs == nil {
		writeError(w, http.StatusServiceUnavailable, "cost aggregator unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	period := strings.ToLower(r.URL.Query().Get("period"))
	if period == "" {
		period = "today"
	}

	report, err := a.costs.Report(period)
	if err != nil {
		a.logger.Error("cost report failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: report})
}

func (a *HTTPAPI) handleCostReportByPath(w http.ResponseWriter, r *http.Request) {
	if a.costs == nil {
		writeError(w, http.StatusServiceUnavailable, "cost aggregator unavailable", "SERVICE_UNAVAILABLE")
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.PathValue("period")))
	if period == "" {
		period = "today"
	}

	report, err := a.costs.Report(period)
	if err != nil {
		a.logger.Error("cost report failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL_ERROR")
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{Data: report})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiError{Error: message, Code: code})
}

func parseIntParam(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

func StartHTTPServer(addr string, handler http.Handler, logger *zap.Logger) (shutdown func(ctx context.Context) error, err error) {
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http api server starting", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return nil, fmt.Errorf("http server failed to start: %w", err)
	case <-time.After(50 * time.Millisecond):
	}

	return srv.Shutdown, nil
}
