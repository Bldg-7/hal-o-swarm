package supervisor

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

type commandTransport interface {
	Send(nodeID string, cmd Command) error
}

type hubCommandTransport struct {
	hub *Hub
}

func (t *hubCommandTransport) Send(nodeID string, cmd Command) error {
	if t.hub == nil {
		return fmt.Errorf("command transport unavailable")
	}

	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command payload: %w", err)
	}

	env := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeCommand),
		RequestID: cmd.CommandID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}

	data, err := shared.MarshalEnvelope(env)
	if err != nil {
		return fmt.Errorf("marshal command envelope: %w", err)
	}

	t.hub.mu.RLock()
	conn, ok := t.hub.clients[nodeID]
	t.hub.mu.RUnlock()
	if !ok {
		return fmt.Errorf("node %s is not connected", nodeID)
	}

	select {
	case conn.send <- data:
		return nil
	default:
		return fmt.Errorf("node %s command channel is saturated", nodeID)
	}
}

type CommandDispatcher struct {
	db       *sql.DB
	registry *NodeRegistry
	tracker  *SessionTracker
	logger   *zap.Logger

	transport commandTransport

	pendingMu sync.Mutex
	pending   map[string]chan *CommandResult
}

func NewCommandDispatcher(db *sql.DB, registry *NodeRegistry, tracker *SessionTracker, hub *Hub, logger *zap.Logger) *CommandDispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &CommandDispatcher{
		db:        db,
		registry:  registry,
		tracker:   tracker,
		logger:    logger,
		transport: &hubCommandTransport{hub: hub},
		pending:   make(map[string]chan *CommandResult),
	}
}

func NewCommandDispatcherWithTransport(db *sql.DB, registry *NodeRegistry, tracker *SessionTracker, transport commandTransport, logger *zap.Logger) *CommandDispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &CommandDispatcher{
		db:        db,
		registry:  registry,
		tracker:   tracker,
		logger:    logger,
		transport: transport,
		pending:   make(map[string]chan *CommandResult),
	}
}

func (d *CommandDispatcher) DispatchCommand(ctx context.Context, cmd Command) (*CommandResult, error) {
	normalizedType, err := ParseCommandIntent(string(cmd.Type))
	if err != nil {
		return nil, err
	}
	cmd.Type = normalizedType

	if cmd.CommandID == "" {
		cmd.CommandID = uuid.NewString()
	} else if _, err := uuid.Parse(cmd.CommandID); err != nil {
		return nil, fmt.Errorf("invalid command_id %q: %w", cmd.CommandID, err)
	}

	if cmd.IdempotencyKey != "" {
		key, err := d.idempotencyHash(cmd)
		if err != nil {
			return nil, err
		}
		if cached, ok := d.checkIdempotency(key); ok {
			return cached, nil
		}

		result, dispatchErr := d.dispatchToTarget(ctx, cmd)
		if dispatchErr != nil {
			return nil, dispatchErr
		}
		if err := d.cacheResult(key, result); err != nil {
			return nil, err
		}
		return result, nil
	}

	return d.dispatchToTarget(ctx, cmd)
}

func (d *CommandDispatcher) dispatchToTarget(ctx context.Context, cmd Command) (*CommandResult, error) {
	nodeID, err := d.resolveTarget(cmd)
	if err != nil {
		return &CommandResult{
			CommandID: cmd.CommandID,
			Status:    CommandStatusFailure,
			Error:     err.Error(),
			Timestamp: time.Now().UTC(),
		}, nil
	}

	result, err := d.sendCommand(nodeID, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("empty command result for command %s", cmd.CommandID)
	}
	if result.CommandID == "" {
		result.CommandID = cmd.CommandID
	}
	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now().UTC()
	}

	return result, nil
}

func (d *CommandDispatcher) checkIdempotency(key string) (*CommandResult, bool) {
	if d.db == nil {
		return nil, false
	}

	row := d.db.QueryRow(`
		SELECT result, expires_at
		FROM command_idempotency
		WHERE key_hash = ?
	`, key)

	var (
		resultJSON string
		expiresAt  string
	)
	if err := row.Scan(&resultJSON, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false
		}
		d.logger.Warn("idempotency lookup failed", zap.String("key_hash", key), zap.Error(err))
		return nil, false
	}

	expiresAtTime, err := parseSQLiteTimestamp(expiresAt)
	if err != nil {
		d.logger.Warn("invalid idempotency expiry", zap.String("key_hash", key), zap.Error(err))
		return nil, false
	}
	if time.Now().UTC().After(expiresAtTime) {
		if _, err := d.db.Exec(`DELETE FROM command_idempotency WHERE key_hash = ?`, key); err != nil {
			d.logger.Warn("failed to purge expired idempotency row", zap.String("key_hash", key), zap.Error(err))
		}
		return nil, false
	}

	var result CommandResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		d.logger.Warn("failed to unmarshal cached command result", zap.String("key_hash", key), zap.Error(err))
		return nil, false
	}

	return &result, true
}

func (d *CommandDispatcher) cacheResult(key string, result *CommandResult) error {
	if result == nil {
		return fmt.Errorf("cannot cache nil command result")
	}
	if d.db == nil {
		return fmt.Errorf("command idempotency storage unavailable")
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal command result cache: %w", err)
	}

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	_, err = d.db.Exec(`
		INSERT INTO command_idempotency (key_hash, command_id, result, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key_hash) DO UPDATE SET
			command_id = excluded.command_id,
			result = excluded.result,
			expires_at = excluded.expires_at
	`, key, result.CommandID, string(resultJSON), expiresAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("cache command result: %w", err)
	}

	return nil
}

func (d *CommandDispatcher) resolveTarget(cmd Command) (nodeID string, err error) {
	if d.registry == nil {
		return "", fmt.Errorf("node registry unavailable")
	}

	if cmd.Target.NodeID != "" {
		node, err := d.registry.GetNode(cmd.Target.NodeID)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				return "", fmt.Errorf("target node not found: %s", cmd.Target.NodeID)
			}
			return "", fmt.Errorf("resolve node %s: %w", cmd.Target.NodeID, err)
		}
		if node.Status != NodeStatusOnline {
			return "", fmt.Errorf("target node offline: %s", cmd.Target.NodeID)
		}
		return node.ID, nil
	}

	if cmd.Target.Project == "" {
		return "", fmt.Errorf("target project or node_id is required")
	}

	if d.tracker != nil {
		sessions := d.tracker.GetAllSessions()
		for _, session := range sessions {
			if session.Project != cmd.Target.Project {
				continue
			}

			node, err := d.registry.GetNode(session.NodeID)
			if err != nil {
				continue
			}
			if node.Status == NodeStatusOnline {
				return node.ID, nil
			}
		}
	}

	for _, node := range d.registry.ListNodes() {
		if node.Status != NodeStatusOnline {
			continue
		}
		for _, project := range node.Projects {
			if project == cmd.Target.Project {
				return node.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no online node found for project: %s", cmd.Target.Project)
}

func (d *CommandDispatcher) sendCommand(nodeID string, cmd Command) (*CommandResult, error) {
	if d.transport == nil {
		return nil, fmt.Errorf("command transport unavailable")
	}

	d.pendingMu.Lock()
	if _, ok := d.pending[cmd.CommandID]; !ok {
		d.pending[cmd.CommandID] = make(chan *CommandResult, 1)
	}
	d.pendingMu.Unlock()

	if err := d.transport.Send(nodeID, cmd); err != nil {
		d.pendingMu.Lock()
		delete(d.pending, cmd.CommandID)
		d.pendingMu.Unlock()
		return nil, err
	}
	return d.waitForResult(cmd.CommandID, cmd.EffectiveTimeout())
}

func (d *CommandDispatcher) waitForResult(commandID string, timeout time.Duration) (*CommandResult, error) {
	d.pendingMu.Lock()
	resultCh, ok := d.pending[commandID]
	if !ok {
		resultCh = make(chan *CommandResult, 1)
		d.pending[commandID] = resultCh
	}
	d.pendingMu.Unlock()

	defer func() {
		d.pendingMu.Lock()
		delete(d.pending, commandID)
		d.pendingMu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		if result == nil {
			return nil, fmt.Errorf("received nil command result for %s", commandID)
		}
		return result, nil
	case <-timer.C:
		return &CommandResult{
			CommandID: commandID,
			Status:    CommandStatusTimeout,
			Error:     fmt.Sprintf("command timed out after %s", timeout),
			Timestamp: time.Now().UTC(),
		}, nil
	}
}

func (d *CommandDispatcher) HandleCommandResult(result CommandResult) bool {
	if result.CommandID == "" {
		return false
	}
	if result.Timestamp.IsZero() {
		result.Timestamp = time.Now().UTC()
	}

	d.pendingMu.Lock()
	resultCh, ok := d.pending[result.CommandID]
	d.pendingMu.Unlock()
	if !ok {
		return false
	}

	select {
	case resultCh <- &result:
		return true
	default:
		return false
	}
}

func (d *CommandDispatcher) HandleCommandResultEnvelope(env *shared.Envelope) error {
	if env == nil {
		return fmt.Errorf("nil command result envelope")
	}

	var result CommandResult
	if err := json.Unmarshal(env.Payload, &result); err != nil {
		return fmt.Errorf("unmarshal command result envelope: %w", err)
	}
	if result.CommandID == "" {
		result.CommandID = env.RequestID
	}

	if handled := d.HandleCommandResult(result); !handled {
		d.logger.Debug("dropped command result with no waiter", zap.String("command_id", result.CommandID))
	}

	return nil
}

func (d *CommandDispatcher) idempotencyHash(cmd Command) (string, error) {
	payload, err := cmd.canonicalPayload()
	if err != nil {
		return "", fmt.Errorf("build canonical command payload: %w", err)
	}

	buffer := make([]byte, 0, len(cmd.IdempotencyKey)+1+len(payload))
	buffer = append(buffer, []byte(cmd.IdempotencyKey)...)
	buffer = append(buffer, ':')
	buffer = append(buffer, payload...)

	digest := sha256.Sum256(buffer)
	return hex.EncodeToString(digest[:]), nil
}
