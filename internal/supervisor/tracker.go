package supervisor

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type SessionStatus string

const (
	SessionStatusRunning     SessionStatus = "running"
	SessionStatusIdle        SessionStatus = "idle"
	SessionStatusError       SessionStatus = "error"
	SessionStatusUnreachable SessionStatus = "unreachable"
)

type TokenUsage struct {
	Prompt     int
	Completion int
	Total      int
}

type TrackedSession struct {
	SessionID       string
	NodeID          string
	Project         string
	Status          SessionStatus
	TokenUsage      TokenUsage
	CompactionCount int
	CurrentTask     string
	LastActivity    time.Time
	SessionCost     float64
	Model           string
	StartedAt       time.Time
}

var ErrSessionNotFound = errors.New("session not found")

type SessionTracker struct {
	db     *sql.DB
	logger *zap.Logger

	mu       sync.RWMutex
	sessions map[string]TrackedSession

	recoveryErrors atomic.Uint64
}

func NewSessionTracker(db *sql.DB, logger *zap.Logger) *SessionTracker {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &SessionTracker{
		db:       db,
		logger:   logger,
		sessions: make(map[string]TrackedSession),
	}
}

func (t *SessionTracker) AddSession(session TrackedSession) error {
	if session.SessionID == "" {
		return fmt.Errorf("add session: missing session_id")
	}
	if session.NodeID == "" {
		return fmt.Errorf("add session %s: missing node_id", session.SessionID)
	}
	if session.Project == "" {
		return fmt.Errorf("add session %s: missing project", session.SessionID)
	}

	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now().UTC()
	}
	if session.LastActivity.IsZero() {
		session.LastActivity = time.Now().UTC()
	}
	if session.Status == "" {
		session.Status = SessionStatusRunning
	}

	if err := t.upsertSession(session); err != nil {
		return fmt.Errorf("add session %s: %w", session.SessionID, err)
	}

	t.mu.Lock()
	t.sessions[session.SessionID] = session
	t.mu.Unlock()

	return nil
}

func (t *SessionTracker) UpdateSession(sessionID string, updates map[string]interface{}) error {
	session, err := t.GetSession(sessionID)
	if err != nil {
		return fmt.Errorf("update session %s: %w", sessionID, err)
	}

	for key, value := range updates {
		switch key {
		case "node_id":
			nodeID, ok := value.(string)
			if !ok {
				return fmt.Errorf("update session %s: node_id must be string", sessionID)
			}
			session.NodeID = nodeID
		case "project":
			project, ok := value.(string)
			if !ok {
				return fmt.Errorf("update session %s: project must be string", sessionID)
			}
			session.Project = project
		case "status":
			status, ok := value.(string)
			if !ok {
				return fmt.Errorf("update session %s: status must be string", sessionID)
			}
			session.Status = SessionStatus(status)
		case "tokens":
			tokens, ok := value.(int)
			if !ok {
				return fmt.Errorf("update session %s: tokens must be int", sessionID)
			}
			session.TokenUsage.Total = tokens
		case "cost":
			cost, ok := value.(float64)
			if !ok {
				return fmt.Errorf("update session %s: cost must be float64", sessionID)
			}
			session.SessionCost = cost
		case "started_at":
			startedAt, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("update session %s: started_at must be time.Time", sessionID)
			}
			session.StartedAt = startedAt.UTC()
		case "token_usage":
			tokenUsage, ok := value.(TokenUsage)
			if !ok {
				return fmt.Errorf("update session %s: token_usage must be TokenUsage", sessionID)
			}
			session.TokenUsage = tokenUsage
		case "session_cost":
			sessionCost, ok := value.(float64)
			if !ok {
				return fmt.Errorf("update session %s: session_cost must be float64", sessionID)
			}
			session.SessionCost = sessionCost
		case "last_activity":
			lastActivity, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("update session %s: last_activity must be time.Time", sessionID)
			}
			session.LastActivity = lastActivity.UTC()
		case "current_task":
			currentTask, ok := value.(string)
			if !ok {
				return fmt.Errorf("update session %s: current_task must be string", sessionID)
			}
			session.CurrentTask = currentTask
		case "model":
			model, ok := value.(string)
			if !ok {
				return fmt.Errorf("update session %s: model must be string", sessionID)
			}
			session.Model = model
		case "compaction_count":
			compactionCount, ok := value.(int)
			if !ok {
				return fmt.Errorf("update session %s: compaction_count must be int", sessionID)
			}
			session.CompactionCount = compactionCount
		default:
			return fmt.Errorf("update session %s: unsupported field %q", sessionID, key)
		}
	}

	if err := t.upsertSession(session); err != nil {
		return fmt.Errorf("update session %s: %w", sessionID, err)
	}

	t.mu.Lock()
	t.sessions[session.SessionID] = session
	t.mu.Unlock()

	return nil
}

func (t *SessionTracker) GetSession(sessionID string) (TrackedSession, error) {
	t.mu.RLock()
	session, ok := t.sessions[sessionID]
	t.mu.RUnlock()
	if ok {
		return session, nil
	}

	session, err := t.readSession(sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TrackedSession{}, ErrSessionNotFound
		}
		return TrackedSession{}, fmt.Errorf("get session %s: %w", sessionID, err)
	}

	t.mu.Lock()
	t.sessions[session.SessionID] = session
	t.mu.Unlock()

	return session, nil
}

func (t *SessionTracker) GetAllSessions() []TrackedSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]TrackedSession, 0, len(t.sessions))
	for _, session := range t.sessions {
		out = append(out, session)
	}
	return out
}

func (t *SessionTracker) GetSessionsByProject(project string) []TrackedSession {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]TrackedSession, 0)
	for _, session := range t.sessions {
		if session.Project == project {
			out = append(out, session)
		}
	}
	return out
}

func (t *SessionTracker) MarkUnreachable(nodeID string) error {
	if _, err := t.db.Exec(`UPDATE sessions SET status = ? WHERE node_id = ?`, string(SessionStatusUnreachable), nodeID); err != nil {
		return fmt.Errorf("mark sessions unreachable for node %s: %w", nodeID, err)
	}

	t.mu.Lock()
	for sessionID, session := range t.sessions {
		if session.NodeID != nodeID {
			continue
		}
		session.Status = SessionStatusUnreachable
		t.sessions[sessionID] = session
	}
	t.mu.Unlock()

	return nil
}

func (t *SessionTracker) LoadSessionsFromDB() error {
	if _, err := t.db.Exec(`UPDATE sessions SET status = ?`, string(SessionStatusUnreachable)); err != nil {
		return fmt.Errorf("load sessions: mark unreachable: %w", err)
	}

	rows, err := t.db.Query(`
		SELECT id, node_id, project, status, tokens, cost, started_at
		FROM sessions
	`)
	if err != nil {
		return fmt.Errorf("load sessions: query rows: %w", err)
	}
	defer rows.Close()

	sessions := make(map[string]TrackedSession)
	for rows.Next() {
		session, rowErr := scanSessionRow(rows)
		if rowErr != nil {
			t.incrementRecoveryError("load sessions: corrupted row", rowErr)
			continue
		}
		session.Status = SessionStatusUnreachable
		sessions[session.SessionID] = session
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load sessions: iterate rows: %w", err)
	}

	t.mu.Lock()
	t.sessions = sessions
	t.mu.Unlock()

	return nil
}

func (t *SessionTracker) ApplyEvent(eventType string, sessionID string) error {
	statusByEvent := map[string]SessionStatus{
		"session.running": SessionStatusRunning,
		"session.idle":    SessionStatusIdle,
		"session.error":   SessionStatusError,
	}

	status, ok := statusByEvent[eventType]
	if !ok {
		return fmt.Errorf("apply event: unsupported event type %q", eventType)
	}

	return t.UpdateSession(sessionID, map[string]interface{}{
		"status":        string(status),
		"last_activity": time.Now().UTC(),
	})
}

func (t *SessionTracker) RestoreFromSnapshot(nodeID string, sessions []TrackedSession) error {
	for _, session := range sessions {
		session.NodeID = nodeID
		if session.Status == "" {
			session.Status = SessionStatusRunning
		}
		if err := t.AddSession(session); err != nil {
			return fmt.Errorf("restore snapshot session %s: %w", session.SessionID, err)
		}
	}

	return nil
}

func (t *SessionTracker) RecoveryErrorCount() uint64 {
	return t.recoveryErrors.Load()
}

func (t *SessionTracker) upsertSession(session TrackedSession) error {
	_, err := t.db.Exec(`
		INSERT INTO sessions (id, node_id, project, status, tokens, cost, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_id = excluded.node_id,
			project = excluded.project,
			status = excluded.status,
			tokens = excluded.tokens,
			cost = excluded.cost,
			started_at = excluded.started_at
	`,
		session.SessionID,
		session.NodeID,
		session.Project,
		string(session.Status),
		session.TokenUsage.Total,
		session.SessionCost,
		session.StartedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", session.SessionID, err)
	}

	return nil
}

func (t *SessionTracker) readSession(sessionID string) (TrackedSession, error) {
	row := t.db.QueryRow(`
		SELECT id, node_id, project, status, tokens, cost, started_at
		FROM sessions
		WHERE id = ?
	`, sessionID)

	return scanSessionSingleRow(row)
}

func (t *SessionTracker) incrementRecoveryError(msg string, err error) {
	t.recoveryErrors.Add(1)
	t.logger.Warn(msg, zap.Error(err))
}

func scanSessionRow(rows *sql.Rows) (TrackedSession, error) {
	var (
		sessionID string
		nodeID    string
		project   string
		statusRaw string
		tokens    int
		cost      float64
		startedAt string
	)

	if err := rows.Scan(&sessionID, &nodeID, &project, &statusRaw, &tokens, &cost, &startedAt); err != nil {
		return TrackedSession{}, fmt.Errorf("scan session row: %w", err)
	}

	startedAtTime, err := parseSQLiteTimestamp(startedAt)
	if err != nil {
		return TrackedSession{}, fmt.Errorf("parse started_at for session %s: %w", sessionID, err)
	}

	return TrackedSession{
		SessionID:   sessionID,
		NodeID:      nodeID,
		Project:     project,
		Status:      SessionStatus(statusRaw),
		TokenUsage:  TokenUsage{Total: tokens},
		SessionCost: cost,
		StartedAt:   startedAtTime,
	}, nil
}

func scanSessionSingleRow(row *sql.Row) (TrackedSession, error) {
	var (
		sessionID string
		nodeID    string
		project   string
		statusRaw string
		tokens    int
		cost      float64
		startedAt string
	)

	if err := row.Scan(&sessionID, &nodeID, &project, &statusRaw, &tokens, &cost, &startedAt); err != nil {
		return TrackedSession{}, err
	}

	startedAtTime, err := parseSQLiteTimestamp(startedAt)
	if err != nil {
		return TrackedSession{}, fmt.Errorf("parse started_at for session %s: %w", sessionID, err)
	}

	return TrackedSession{
		SessionID:   sessionID,
		NodeID:      nodeID,
		Project:     project,
		Status:      SessionStatus(statusRaw),
		TokenUsage:  TokenUsage{Total: tokens},
		SessionCost: cost,
		StartedAt:   startedAtTime,
	}, nil
}
