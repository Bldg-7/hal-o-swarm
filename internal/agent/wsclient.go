package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hal-o-swarm/hal-o-swarm/internal/shared"
	"go.uber.org/zap"
)

const (
	wsReadDeadline  = 60 * time.Second
	wsWriteDeadline = 10 * time.Second
)

// SessionSnapshot represents a local session's state for the supervisor.
type SessionSnapshot struct {
	SessionID string  `json:"session_id"`
	Project   string  `json:"project"`
	Status    string  `json:"status"`
	Tokens    int64   `json:"tokens"`
	Cost      float64 `json:"cost"`
	StartedAt int64   `json:"started_at"`
}

// StateSnapshot is sent on every connect/reconnect so the supervisor
// has a full picture of this agent's state.
type StateSnapshot struct {
	Sessions []SessionSnapshot `json:"sessions"`
	LastSeq  int64             `json:"last_seq"`
}

// SnapshotProvider collects current agent state for the reconnect snapshot.
type SnapshotProvider func() *StateSnapshot

// MessageHandler processes messages received from the supervisor.
type MessageHandler func(env *shared.Envelope) error

// CommandHandler processes a specific command type received from the supervisor.
type CommandHandler func(ctx context.Context, envelope *shared.Envelope) error

// pendingEvent pairs a sequence number with an envelope for resend tracking.
type pendingEvent struct {
	seq int64
	env *shared.Envelope
}

// WSClient manages a WebSocket connection to the supervisor with:
//   - Jittered exponential backoff reconnect
//   - Full state snapshot on every connect/reconnect
//   - Sequence-aware event resend for unacknowledged events
//
// Usage: call Connect() once, then Close() to shut down.
type WSClient struct {
	url       string
	authToken string
	nodeID    string
	logger    *zap.Logger
	backoff   *Backoff

	snapshotProvider SnapshotProvider
	messageHandler   MessageHandler
	onConnectHooks   []func() error

	commandHandlers map[string]CommandHandler
	commandMu       sync.RWMutex

	conn   *websocket.Conn
	connMu sync.Mutex

	// Sequence tracking for event delivery guarantee
	lastAckedSeq int64
	nextSeq      int64
	seqMu        sync.Mutex

	// Buffered events awaiting acknowledgement
	pendingEvents []pendingEvent
	pendingMu     sync.Mutex

	cancel context.CancelFunc
	done   chan struct{}
}

// WSClientOption configures a WSClient.
type WSClientOption func(*WSClient)

// WithSnapshotProvider sets the function called on connect/reconnect to collect state.
func WithSnapshotProvider(sp SnapshotProvider) WSClientOption {
	return func(c *WSClient) { c.snapshotProvider = sp }
}

// WithMessageHandler sets the handler for incoming supervisor messages.
func WithMessageHandler(mh MessageHandler) WSClientOption {
	return func(c *WSClient) { c.messageHandler = mh }
}

func WithOnConnectHook(hook func() error) WSClientOption {
	return func(c *WSClient) {
		if hook != nil {
			c.onConnectHooks = append(c.onConnectHooks, hook)
		}
	}
}

// WithBackoff overrides the default backoff configuration.
func WithBackoff(b *Backoff) WSClientOption {
	return func(c *WSClient) { c.backoff = b }
}

func WithNodeID(nodeID string) WSClientOption {
	return func(c *WSClient) { c.nodeID = nodeID }
}

// NewWSClient creates a WebSocket client for supervisor communication.
func NewWSClient(url, authToken string, logger *zap.Logger, opts ...WSClientOption) *WSClient {
	c := &WSClient{
		url:             url,
		authToken:       authToken,
		logger:          logger,
		backoff:         DefaultBackoff(),
		commandHandlers: make(map[string]CommandHandler),
		done:            make(chan struct{}),
		nextSeq:         1,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Connect starts the reconnect loop in a background goroutine.
func (c *WSClient) Connect(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	go c.connectLoop(ctx)
}

// Close stops the reconnect loop and closes the active connection.
func (c *WSClient) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	// Close connection to unblock any pending ReadMessage
	c.closeConn()
	<-c.done
	return nil
}

func (c *WSClient) connectLoop(ctx context.Context) {
	defer close(c.done)

	for {
		err := c.dialAndServe(ctx)
		if ctx.Err() != nil {
			c.logger.Info("ws client shutting down")
			return
		}
		if err != nil {
			c.logger.Error("ws connection error", zap.Error(err))
		}

		wait := c.backoff.Duration()
		c.logger.Info("reconnecting",
			zap.Duration("backoff", wait),
			zap.Int("attempt", c.backoff.Attempt()),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (c *WSClient) dialAndServe(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.authToken)
	if c.nodeID != "" {
		header.Set("X-Node-ID", c.nodeID)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.url, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	c.backoff.Reset()
	c.logger.Info("connected to supervisor", zap.String("url", c.url))

	// Full state snapshot on every connect/reconnect
	if err := c.sendSnapshot(); err != nil {
		c.closeConn()
		return fmt.Errorf("send snapshot: %w", err)
	}

	// Resend any unacknowledged events
	if err := c.resendPendingEvents(); err != nil {
		c.closeConn()
		return fmt.Errorf("resend events: %w", err)
	}

	if err := c.runOnConnectHooks(); err != nil {
		c.closeConn()
		return fmt.Errorf("run on-connect hooks: %w", err)
	}

	return c.readLoop(ctx)
}

func (c *WSClient) runOnConnectHooks() error {
	for _, hook := range c.onConnectHooks {
		if err := hook(); err != nil {
			return err
		}
	}
	return nil
}

func (c *WSClient) readLoop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()
		if conn == nil {
			return fmt.Errorf("connection closed")
		}

		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		env, err := shared.UnmarshalEnvelope(msg)
		if err != nil {
			c.logger.Warn("invalid message from supervisor", zap.Error(err))
			continue
		}

		if env.Type == string(shared.MessageTypeCommand) {
			c.handleCommand(ctx, env)
			continue
		}

		if c.messageHandler != nil {
			if err := c.messageHandler(env); err != nil {
				c.logger.Error("message handler error",
					zap.Error(err),
					zap.String("type", env.Type),
				)
			}
		}
	}
}

func (c *WSClient) sendSnapshot() error {
	if c.snapshotProvider == nil {
		return nil
	}

	snapshot := c.snapshotProvider()

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	env := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeRegister),
		Timestamp: time.Now().Unix(),
		Payload:   payload,
	}

	return c.SendEnvelope(env)
}

// SendEnvelope sends an envelope over the WebSocket connection. Thread-safe.
func (c *WSClient) SendEnvelope(env *shared.Envelope) error {
	data, err := shared.MarshalEnvelope(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	c.conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// SendEvent sends an event and buffers it for potential resend on reconnect.
// Returns the assigned sequence number.
func (c *WSClient) SendEvent(env *shared.Envelope) (int64, error) {
	c.seqMu.Lock()
	seq := c.nextSeq
	c.nextSeq++
	c.seqMu.Unlock()

	c.pendingMu.Lock()
	c.pendingEvents = append(c.pendingEvents, pendingEvent{seq: seq, env: env})
	c.pendingMu.Unlock()

	return seq, c.SendEnvelope(env)
}

// AcknowledgeSeq marks all events with sequence <= seq as delivered.
// Acknowledged events are removed from the pending buffer.
func (c *WSClient) AcknowledgeSeq(seq int64) {
	c.seqMu.Lock()
	c.lastAckedSeq = seq
	c.seqMu.Unlock()

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	kept := c.pendingEvents[:0]
	for _, pe := range c.pendingEvents {
		if pe.seq > seq {
			kept = append(kept, pe)
		}
	}
	c.pendingEvents = kept
}

func (c *WSClient) resendPendingEvents() error {
	c.pendingMu.Lock()
	events := make([]pendingEvent, len(c.pendingEvents))
	copy(events, c.pendingEvents)
	c.pendingMu.Unlock()

	for _, pe := range events {
		if err := c.SendEnvelope(pe.env); err != nil {
			return fmt.Errorf("resend event seq=%d: %w", pe.seq, err)
		}
	}
	return nil
}

func (c *WSClient) closeConn() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// LastAckedSeq returns the last acknowledged sequence number.
func (c *WSClient) LastAckedSeq() int64 {
	c.seqMu.Lock()
	defer c.seqMu.Unlock()
	return c.lastAckedSeq
}

// IsConnected returns whether the client has an active connection.
func (c *WSClient) IsConnected() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn != nil
}

// RegisterCommandHandler registers a handler for a specific command type.
func (c *WSClient) RegisterCommandHandler(cmdType string, handler CommandHandler) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	c.commandHandlers[cmdType] = handler
}

func (c *WSClient) handleCommand(ctx context.Context, env *shared.Envelope) {
	var cmd struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(env.Payload, &cmd); err != nil {
		c.logger.Warn("failed to parse command payload",
			zap.Error(err),
			zap.String("request_id", env.RequestID),
		)
		return
	}

	c.commandMu.RLock()
	handler, ok := c.commandHandlers[cmd.Type]
	c.commandMu.RUnlock()

	if !ok {
		c.logger.Warn("no handler for command type",
			zap.String("command_type", cmd.Type),
			zap.String("request_id", env.RequestID),
		)
		return
	}

	if err := handler(ctx, env); err != nil {
		c.logger.Error("command handler error",
			zap.String("command_type", cmd.Type),
			zap.String("request_id", env.RequestID),
			zap.Error(err),
		)
	}
}
