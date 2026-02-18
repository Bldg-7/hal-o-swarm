package supervisor

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type HubEvent struct {
	Type    string
	AgentID string
	Time    time.Time
}

// Hub manages all WebSocket agent connections using the Gorilla hub pattern.
type Hub struct {
	clients    map[string]*AgentConn
	register   chan *AgentConn
	unregister chan *AgentConn
	broadcast  chan []byte
	events     chan HubEvent

	authToken      string
	allowedOrigins []string
	strictOrigin   bool

	heartbeatInterval time.Duration
	heartbeatTimeout  int

	upgrader websocket.Upgrader
	logger   *zap.Logger
	mu       sync.RWMutex
	ctx      context.Context

	credentialRegistry  *NodeRegistry
	expectedCredVersion int64
	commandDispatcher   *CommandDispatcher
	nodeRegistry        *NodeRegistry
}

func NewHub(
	ctx context.Context,
	authToken string,
	allowedOrigins []string,
	heartbeatInterval time.Duration,
	heartbeatTimeout int,
	logger *zap.Logger,
) *Hub {
	h := &Hub{
		clients:           make(map[string]*AgentConn),
		register:          make(chan *AgentConn),
		unregister:        make(chan *AgentConn),
		broadcast:         make(chan []byte, 256),
		events:            make(chan HubEvent, 64),
		authToken:         authToken,
		allowedOrigins:    allowedOrigins,
		heartbeatInterval: heartbeatInterval,
		heartbeatTimeout:  heartbeatTimeout,
		logger:            logger,
		ctx:               ctx,
	}
	h.upgrader = websocket.Upgrader{
		CheckOrigin: h.checkOrigin,
	}
	return h
}

func (h *Hub) Run() {
	ticker := time.NewTicker(h.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			h.mu.Lock()
			for id, conn := range h.clients {
				close(conn.send)
				conn.conn.Close()
				delete(h.clients, id)
			}
			h.mu.Unlock()
			return

		case conn := <-h.register:
			h.mu.Lock()
			if existing, ok := h.clients[conn.agentID]; ok {
				if existing != conn {
					close(existing.send)
					existing.conn.Close()
				}
			}
			h.clients[conn.agentID] = conn
			h.mu.Unlock()
			h.registerNodeOnline(conn.agentID)
			h.logger.Info("agent registered", zap.String("agent_id", conn.agentID))
			select {
			case h.events <- HubEvent{Type: "node.online", AgentID: conn.agentID, Time: time.Now()}:
			default:
			}

		case conn := <-h.unregister:
			removed := false
			h.mu.Lock()
			if current, ok := h.clients[conn.agentID]; ok && current == conn {
				delete(h.clients, conn.agentID)
				close(conn.send)
				removed = true
				h.logger.Info("agent unregistered", zap.String("agent_id", conn.agentID))
			}
			h.mu.Unlock()
			if removed {
				h.registerNodeOffline(conn.agentID)
			}

		case msg := <-h.broadcast:
			h.mu.Lock()
			for id, conn := range h.clients {
				select {
				case conn.send <- msg:
				default:
					h.logger.Warn("dropping slow client", zap.String("agent_id", id))
					close(conn.send)
					delete(h.clients, id)
				}
			}
			h.mu.Unlock()

		case <-ticker.C:
			h.checkHeartbeats()
		}
	}
}

// ServeWS handles WebSocket upgrade requests with token auth (header or query param).
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := ""
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else {
		token = r.URL.Query().Get("token")
	}

	h.mu.RLock()
	currentToken := h.authToken
	h.mu.RUnlock()

	if token != currentToken {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", zap.Error(err))
		return
	}

	agentID := strings.TrimSpace(r.Header.Get("X-Node-ID"))
	if agentID == "" {
		agentID = uuid.New().String()
	}
	agent := newAgentConn(h, conn, agentID)
	h.register <- agent

	go agent.writePump()
	go agent.readPump()
}

func (h *Hub) Events() <-chan HubEvent {
	return h.events
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) checkOrigin(r *http.Request) bool {
	if h.strictOrigin {
		return h.checkOriginStrict(r)
	}

	if len(h.allowedOrigins) == 0 {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range h.allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (h *Hub) checkOriginStrict(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		h.logger.Warn("rejected connection with missing origin")
		return false
	}

	for _, allowed := range h.allowedOrigins {
		if MatchOrigin(origin, allowed) {
			return true
		}
	}

	h.logger.Warn("rejected connection from unauthorized origin",
		zap.String("origin", origin))
	return false
}

func (h *Hub) UpdateAuthToken(newToken string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authToken = newToken
}

func (h *Hub) SetStrictOrigin(strict bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.strictOrigin = strict
}

func (h *Hub) SetAllowedOrigins(origins []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowedOrigins = origins
}

func (h *Hub) ConfigureCredentialReconciliation(registry *NodeRegistry, expectedVersion int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.credentialRegistry = registry
	h.expectedCredVersion = expectedVersion
}

func (h *Hub) ConfigureCommandResultDispatcher(dispatcher *CommandDispatcher) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commandDispatcher = dispatcher
}

func (h *Hub) ConfigureNodeRegistry(registry *NodeRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nodeRegistry = registry
}

func (h *Hub) reconcileCredentialSync(payload []byte) {
	h.mu.RLock()
	registry := h.credentialRegistry
	expectedVersion := h.expectedCredVersion
	h.mu.RUnlock()

	if registry == nil {
		return
	}

	if err := registry.HandleCredentialSyncMessage(payload, expectedVersion); err != nil {
		h.logger.Warn("credential sync reconciliation failed", zap.Error(err))
	}
}

func (h *Hub) reconcileAuthState(nodeID string, payload []byte) {
	h.mu.RLock()
	registry := h.credentialRegistry
	h.mu.RUnlock()

	if registry == nil {
		return
	}

	if err := registry.HandleAuthStateMessage(nodeID, payload); err != nil {
		h.logger.Warn("auth state ingest failed",
			zap.String("node_id", nodeID),
			zap.Error(err),
		)
	}
}

func (h *Hub) handleCommandResultEnvelope(env *shared.Envelope) {
	h.mu.RLock()
	dispatcher := h.commandDispatcher
	h.mu.RUnlock()

	if dispatcher == nil {
		return
	}

	if err := dispatcher.HandleCommandResultEnvelope(env); err != nil {
		h.logger.Warn("command result ingest failed", zap.Error(err))
	}
}

func (h *Hub) checkHeartbeats() {
	timeout := h.heartbeatInterval * time.Duration(h.heartbeatTimeout)
	now := time.Now()

	h.mu.RLock()
	var timedOut []*AgentConn
	for _, conn := range h.clients {
		conn.mu.Lock()
		elapsed := now.Sub(conn.lastHeartbeat)
		conn.mu.Unlock()
		if elapsed > timeout {
			timedOut = append(timedOut, conn)
		}
	}
	h.mu.RUnlock()

	for _, conn := range timedOut {
		h.logger.Warn("agent heartbeat timeout", zap.String("agent_id", conn.agentID))
		h.registerNodeOffline(conn.agentID)
		select {
		case h.events <- HubEvent{Type: "node.offline", AgentID: conn.agentID, Time: now}:
		default:
		}
		conn.conn.Close()
	}
}

func (h *Hub) registerNodeOnline(agentID string) {
	h.mu.RLock()
	registry := h.nodeRegistry
	h.mu.RUnlock()
	if registry == nil || agentID == "" {
		return
	}

	err := registry.Register(NodeEntry{ID: agentID, Hostname: agentID})
	if err != nil {
		h.logger.Warn("failed to register node online",
			zap.String("node_id", agentID),
			zap.Error(err),
		)
	}
}

func (h *Hub) registerNodeOffline(agentID string) {
	h.mu.RLock()
	registry := h.nodeRegistry
	h.mu.RUnlock()
	if registry == nil || agentID == "" {
		return
	}

	err := registry.MarkOffline(agentID)
	if err != nil && !errors.Is(err, ErrNodeNotFound) {
		h.logger.Warn("failed to mark node offline",
			zap.String("node_id", agentID),
			zap.Error(err),
		)
	}
}
