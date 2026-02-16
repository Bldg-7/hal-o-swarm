package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

type HTTPAPI struct {
	registry   *NodeRegistry
	tracker    *SessionTracker
	dispatcher *CommandDispatcher
	db         *sql.DB
	authToken  string
	logger     *zap.Logger
}

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
	return &HTTPAPI{
		registry:   registry,
		tracker:    tracker,
		dispatcher: dispatcher,
		db:         db,
		authToken:  authToken,
		logger:     logger,
	}
}

func (a *HTTPAPI) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", a.handleHealth)

	mux.Handle("GET /api/v1/sessions", a.requireAuth(http.HandlerFunc(a.handleListSessions)))
	mux.Handle("GET /api/v1/sessions/{id}", a.requireAuth(http.HandlerFunc(a.handleGetSession)))
	mux.Handle("GET /api/v1/nodes", a.requireAuth(http.HandlerFunc(a.handleListNodes)))
	mux.Handle("GET /api/v1/nodes/{id}", a.requireAuth(http.HandlerFunc(a.handleGetNode)))
	mux.Handle("GET /api/v1/events", a.requireAuth(http.HandlerFunc(a.handleListEvents)))
	mux.Handle("POST /api/v1/commands", a.requireAuth(http.HandlerFunc(a.handleCommand)))

	return mux
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

	timeout := time.Duration(req.Timeout) * time.Second
	cmd := Command{
		Type:           CommandType(req.Type),
		IdempotencyKey: req.IdempotencyKey,
		Target:         CommandTarget{Project: req.Target},
		Args:           req.Args,
		Timeout:        timeout,
	}

	ctx, cancel := context.WithTimeout(r.Context(), cmd.EffectiveTimeout()+5*time.Second)
	defer cancel()

	result, err := a.dispatcher.DispatchCommand(ctx, cmd)
	if err != nil {
		a.logger.Error("dispatch command failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "command dispatch failed", "DISPATCH_ERROR")
		return
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
