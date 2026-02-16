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

type NodeStatus string

const (
	NodeStatusOnline   NodeStatus = "online"
	NodeStatusOffline  NodeStatus = "offline"
	NodeStatusDegraded NodeStatus = "degraded"
)

type NodeEntry struct {
	ID            string
	Hostname      string
	Address       string
	Projects      []string
	Capabilities  []string
	Status        NodeStatus
	LastHeartbeat time.Time
	ConnectedAt   time.Time
}

var ErrNodeNotFound = errors.New("node not found")

type NodeRegistry struct {
	db     *sql.DB
	logger *zap.Logger

	mu    sync.RWMutex
	nodes map[string]NodeEntry

	recoveryErrors atomic.Uint64
}

func NewNodeRegistry(db *sql.DB, logger *zap.Logger) *NodeRegistry {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &NodeRegistry{
		db:     db,
		logger: logger,
		nodes:  make(map[string]NodeEntry),
	}
}

func (r *NodeRegistry) Register(node NodeEntry) error {
	if node.ID == "" {
		return fmt.Errorf("register node: missing id")
	}
	if node.Hostname == "" {
		return fmt.Errorf("register node %s: missing hostname", node.ID)
	}

	now := time.Now().UTC()
	if node.ConnectedAt.IsZero() {
		node.ConnectedAt = now
	}
	node.Status = NodeStatusOnline
	node.LastHeartbeat = now

	if err := r.upsertNode(node); err != nil {
		return fmt.Errorf("register node %s: %w", node.ID, err)
	}

	r.mu.Lock()
	r.nodes[node.ID] = node
	r.mu.Unlock()

	return nil
}

func (r *NodeRegistry) Unregister(nodeID string) error {
	return r.MarkOffline(nodeID)
}

func (r *NodeRegistry) MarkOffline(nodeID string) error {
	r.mu.Lock()
	node, ok := r.nodes[nodeID]
	r.mu.Unlock()

	if ok {
		node.Status = NodeStatusOffline
		node.LastHeartbeat = time.Now().UTC()
	} else {
		fromDB, err := r.readNode(nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNodeNotFound
			}
			return fmt.Errorf("mark node offline %s: %w", nodeID, err)
		}
		node = fromDB
		node.Status = NodeStatusOffline
		node.LastHeartbeat = time.Now().UTC()
	}

	if err := r.upsertNode(node); err != nil {
		return fmt.Errorf("mark node offline %s: %w", nodeID, err)
	}

	r.mu.Lock()
	r.nodes[nodeID] = node
	r.mu.Unlock()

	return nil
}

func (r *NodeRegistry) GetNode(nodeID string) (NodeEntry, error) {
	r.mu.RLock()
	node, ok := r.nodes[nodeID]
	r.mu.RUnlock()
	if ok {
		return node, nil
	}

	node, err := r.readNode(nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeEntry{}, ErrNodeNotFound
		}
		return NodeEntry{}, fmt.Errorf("get node %s: %w", nodeID, err)
	}

	r.mu.Lock()
	r.nodes[nodeID] = node
	r.mu.Unlock()

	return node, nil
}

func (r *NodeRegistry) ListNodes() []NodeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]NodeEntry, 0, len(r.nodes))
	for _, node := range r.nodes {
		out = append(out, node)
	}
	return out
}

func (r *NodeRegistry) LoadNodesFromDB() error {
	if _, err := r.db.Exec(`UPDATE nodes SET status = ?`, string(NodeStatusOffline)); err != nil {
		return fmt.Errorf("load nodes: mark offline: %w", err)
	}

	rows, err := r.db.Query(`
		SELECT id, hostname, status, last_heartbeat, connected_at
		FROM nodes
	`)
	if err != nil {
		return fmt.Errorf("load nodes: query rows: %w", err)
	}
	defer rows.Close()

	nodes := make(map[string]NodeEntry)
	for rows.Next() {
		entry, rowErr := scanNodeRow(rows)
		if rowErr != nil {
			r.incrementRecoveryError("load nodes: corrupted row", rowErr)
			continue
		}
		entry.Status = NodeStatusOffline
		nodes[entry.ID] = entry
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("load nodes: iterate rows: %w", err)
	}

	r.mu.Lock()
	r.nodes = nodes
	r.mu.Unlock()

	return nil
}

func (r *NodeRegistry) RecoveryErrorCount() uint64 {
	return r.recoveryErrors.Load()
}

func (r *NodeRegistry) upsertNode(node NodeEntry) error {
	_, err := r.db.Exec(`
		INSERT INTO nodes (id, hostname, status, last_heartbeat, connected_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			hostname = excluded.hostname,
			status = excluded.status,
			last_heartbeat = excluded.last_heartbeat,
			connected_at = excluded.connected_at
	`,
		node.ID,
		node.Hostname,
		string(node.Status),
		node.LastHeartbeat.UTC().Format(time.RFC3339Nano),
		node.ConnectedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("upsert node %s: %w", node.ID, err)
	}

	return nil
}

func (r *NodeRegistry) readNode(nodeID string) (NodeEntry, error) {
	row := r.db.QueryRow(`
		SELECT id, hostname, status, last_heartbeat, connected_at
		FROM nodes
		WHERE id = ?
	`, nodeID)

	return scanNodeSingleRow(row)
}

func (r *NodeRegistry) incrementRecoveryError(msg string, err error) {
	r.recoveryErrors.Add(1)
	r.logger.Warn(msg, zap.Error(err))
}

func scanNodeRow(rows *sql.Rows) (NodeEntry, error) {
	var (
		id            string
		hostname      string
		statusRaw     string
		lastHeartbeat sql.NullString
		connectedAt   sql.NullString
	)

	if err := rows.Scan(&id, &hostname, &statusRaw, &lastHeartbeat, &connectedAt); err != nil {
		return NodeEntry{}, fmt.Errorf("scan node row: %w", err)
	}

	entry := NodeEntry{
		ID:       id,
		Hostname: hostname,
		Status:   NodeStatus(statusRaw),
	}

	if lastHeartbeat.Valid {
		parsed, err := parseSQLiteTimestamp(lastHeartbeat.String)
		if err != nil {
			return NodeEntry{}, fmt.Errorf("parse last_heartbeat for node %s: %w", id, err)
		}
		entry.LastHeartbeat = parsed
	}

	if connectedAt.Valid {
		parsed, err := parseSQLiteTimestamp(connectedAt.String)
		if err != nil {
			return NodeEntry{}, fmt.Errorf("parse connected_at for node %s: %w", id, err)
		}
		entry.ConnectedAt = parsed
	}

	return entry, nil
}

func scanNodeSingleRow(row *sql.Row) (NodeEntry, error) {
	var (
		id            string
		hostname      string
		statusRaw     string
		lastHeartbeat sql.NullString
		connectedAt   sql.NullString
	)

	if err := row.Scan(&id, &hostname, &statusRaw, &lastHeartbeat, &connectedAt); err != nil {
		return NodeEntry{}, err
	}

	entry := NodeEntry{
		ID:       id,
		Hostname: hostname,
		Status:   NodeStatus(statusRaw),
	}

	if lastHeartbeat.Valid {
		parsed, err := parseSQLiteTimestamp(lastHeartbeat.String)
		if err != nil {
			return NodeEntry{}, fmt.Errorf("parse last_heartbeat for node %s: %w", id, err)
		}
		entry.LastHeartbeat = parsed
	}

	if connectedAt.Valid {
		parsed, err := parseSQLiteTimestamp(connectedAt.String)
		if err != nil {
			return NodeEntry{}, fmt.Errorf("parse connected_at for node %s: %w", id, err)
		}
		entry.ConnectedAt = parsed
	}

	return entry, nil
}

func parseSQLiteTimestamp(value string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999",
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp format: %q", value)
}
