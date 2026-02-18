package supervisor

import (
	"sync"
	"time"

	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 54 * time.Second // 90% of pongWait
	maxMessageSize = 65536
)

type AgentConn struct {
	hub     *Hub
	conn    *websocket.Conn
	agentID string
	send    chan []byte

	lastHeartbeat time.Time
	mu            sync.Mutex
}

func newAgentConn(hub *Hub, conn *websocket.Conn, agentID string) *AgentConn {
	return &AgentConn{
		hub:           hub,
		conn:          conn,
		agentID:       agentID,
		send:          make(chan []byte, 256),
		lastHeartbeat: time.Now(),
	}
}

func (c *AgentConn) readPump() {
	defer func() {
		select {
		case c.hub.unregister <- c:
		case <-c.hub.ctx.Done():
		}
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				c.hub.logger.Warn("unexpected close",
					zap.String("agent_id", c.agentID),
					zap.Error(err),
				)
			}
			return
		}

		env, err := shared.UnmarshalEnvelope(message)
		if err != nil {
			continue
		}

		c.handleEnvelope(env)
	}
}

func (c *AgentConn) handleEnvelope(env *shared.Envelope) {
	c.mu.Lock()
	c.lastHeartbeat = time.Now()
	c.mu.Unlock()

	if env.Type == string(shared.MessageTypeHeartbeat) {
		return
	}

	if env.Type == string(shared.MessageTypeCredentialSync) {
		c.hub.reconcileCredentialSync(env.Payload)
		return
	}

	if env.Type == string(shared.MessageTypeRegister) {
		return
	}

	if env.Type == string(shared.MessageTypeAuthState) {
		c.hub.reconcileAuthState(c.agentID, env.Payload)
		return
	}

	if env.Type == string(shared.MessageTypeCommandResult) {
		c.hub.handleCommandResultEnvelope(env)
	}
}

func (c *AgentConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
