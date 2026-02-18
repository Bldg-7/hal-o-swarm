package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/Bldg-7/hal-o-swarm/internal/agent"
	"github.com/Bldg-7/hal-o-swarm/internal/shared"
	"github.com/Bldg-7/hal-o-swarm/internal/storage"
	"github.com/Bldg-7/hal-o-swarm/internal/supervisor"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

type supervisorHarness struct {
	t              *testing.T
	token          string
	db             *sql.DB
	registry       *supervisor.NodeRegistry
	tracker        *supervisor.SessionTracker
	pipeline       *supervisor.EventPipeline
	dispatcher     *supervisor.CommandDispatcher
	heartbeatGrace time.Duration

	httpServer *httptest.Server

	mu          sync.RWMutex
	connections map[string]*nodeConnection

	replayCount atomic.Int64
}

type nodeConnection struct {
	nodeID         string
	projects       []string
	conn           *websocket.Conn
	writeMu        sync.Mutex
	lastHeartbeat  time.Time
	heartbeatPause bool
}

type supervisorHarnessTransport struct {
	h *supervisorHarness
}

func (t *supervisorHarnessTransport) Send(nodeID string, cmd supervisor.Command) error {
	t.h.mu.RLock()
	node, ok := t.h.connections[nodeID]
	t.h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("node %s not connected", nodeID)
	}

	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
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
		return err
	}

	node.writeMu.Lock()
	defer node.writeMu.Unlock()
	node.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return node.conn.WriteMessage(websocket.TextMessage, data)
}

func newSupervisorHarness(t *testing.T) *supervisorHarness {
	t.Helper()

	db := setupIntegrationDB(t)
	h := newSupervisorHarnessWithDB(t, db, false)
	t.Cleanup(func() {
		h.stop()
	})
	return h
}

func newSupervisorHarnessWithDB(t *testing.T, db *sql.DB, restore bool) *supervisorHarness {
	t.Helper()

	logger := zap.NewNop()
	registry := supervisor.NewNodeRegistry(db, logger)
	tracker := supervisor.NewSessionTracker(db, logger)
	if restore {
		if err := registry.LoadNodesFromDB(); err != nil {
			t.Fatalf("load nodes: %v", err)
		}
		if err := tracker.LoadSessionsFromDB(); err != nil {
			t.Fatalf("load sessions: %v", err)
		}
	}

	pipeline, err := supervisor.NewEventPipeline(db, logger, func(_ string, _ supervisor.RequestEventRange) error {
		return nil
	})
	if err != nil {
		t.Fatalf("create event pipeline: %v", err)
	}

	h := &supervisorHarness{
		t:              t,
		token:          "integration-token",
		db:             db,
		registry:       registry,
		tracker:        tracker,
		pipeline:       pipeline,
		heartbeatGrace: 350 * time.Millisecond,
		connections:    make(map[string]*nodeConnection),
	}
	h.dispatcher = supervisor.NewCommandDispatcherWithTransport(db, registry, tracker, &supervisorHarnessTransport{h: h}, logger)
	h.startWebsocketServer()
	h.startHeartbeatMonitor()
	return h
}

func (h *supervisorHarness) startWebsocketServer() {
	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			token = r.URL.Query().Get("token")
		}
		if token != h.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		nodeID := r.URL.Query().Get("node_id")
		if nodeID == "" {
			nodeID = fmt.Sprintf("node-%d", time.Now().UnixNano())
		}
		projects := splitCSV(r.URL.Query().Get("projects"))

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		node := &nodeConnection{
			nodeID:        nodeID,
			projects:      projects,
			conn:          conn,
			lastHeartbeat: time.Now().UTC(),
		}

		h.mu.Lock()
		h.connections[nodeID] = node
		h.mu.Unlock()

		if err := h.registry.Register(supervisor.NodeEntry{ID: nodeID, Hostname: nodeID, Projects: projects}); err != nil {
			h.t.Fatalf("register node %s: %v", nodeID, err)
		}

		h.readLoop(node)
	})

	h.httpServer = httptest.NewServer(mux)
}

func (h *supervisorHarness) readLoop(node *nodeConnection) {
	defer func() {
		h.mu.Lock()
		delete(h.connections, node.nodeID)
		h.mu.Unlock()
		_ = node.conn.Close()
		_ = h.registry.MarkOffline(node.nodeID)
		_ = h.tracker.MarkUnreachable(node.nodeID)
	}()

	for {
		node.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := node.conn.ReadMessage()
		if err != nil {
			return
		}

		env, err := shared.UnmarshalEnvelope(data)
		if err != nil {
			continue
		}

		switch env.Type {
		case string(shared.MessageTypeHeartbeat):
			h.mu.Lock()
			if current, ok := h.connections[node.nodeID]; ok {
				current.lastHeartbeat = time.Now().UTC()
			}
			h.mu.Unlock()
		case string(shared.MessageTypeRegister):
			var snapshot agent.StateSnapshot
			if err := json.Unmarshal(env.Payload, &snapshot); err != nil {
				continue
			}
			tracked := make([]supervisor.TrackedSession, 0, len(snapshot.Sessions))
			for _, sess := range snapshot.Sessions {
				tracked = append(tracked, supervisor.TrackedSession{
					SessionID:   sess.SessionID,
					NodeID:      node.nodeID,
					Project:     sess.Project,
					Status:      supervisor.SessionStatus(sess.Status),
					TokenUsage:  supervisor.TokenUsage{Total: int(sess.Tokens)},
					SessionCost: sess.Cost,
					StartedAt:   time.Unix(sess.StartedAt, 0).UTC(),
				})
			}
			if len(tracked) > 0 {
				if err := h.tracker.RestoreFromSnapshot(node.nodeID, tracked); err != nil {
					h.t.Errorf("restore snapshot: %v", err)
					return
				}
			}
		case string(shared.MessageTypeCommandResult):
			if err := h.dispatcher.HandleCommandResultEnvelope(env); err != nil {
				h.t.Errorf("handle command result envelope: %v", err)
				return
			}
		case string(shared.MessageTypeEvent):
			var event supervisor.Event
			if err := json.Unmarshal(env.Payload, &event); err != nil {
				continue
			}
			if err := h.pipeline.ProcessEvent(node.nodeID, event); err != nil {
				h.t.Errorf("process event: %v", err)
				return
			}
		}
	}
}

func (h *supervisorHarness) startHeartbeatMonitor() {
	ticker := time.NewTicker(25 * time.Millisecond)
	h.t.Cleanup(ticker.Stop)

	go func() {
		for range ticker.C {
			now := time.Now().UTC()
			var stale []*nodeConnection

			h.mu.RLock()
			for _, conn := range h.connections {
				if conn.heartbeatPause {
					continue
				}
				if now.Sub(conn.lastHeartbeat) > h.heartbeatGrace {
					stale = append(stale, conn)
				}
			}
			h.mu.RUnlock()

			for _, conn := range stale {
				_ = conn.conn.Close()
			}
		}
	}()
}

func (h *supervisorHarness) wsURL(nodeID string, projects []string) string {
	raw := "ws" + strings.TrimPrefix(h.httpServer.URL, "http") + "/ws/agent"
	return fmt.Sprintf("%s?token=%s&node_id=%s&projects=%s", raw, h.token, nodeID, strings.Join(projects, ","))
}

func (h *supervisorHarness) dispatch(t *testing.T, cmd supervisor.Command) *supervisor.CommandResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := h.dispatcher.DispatchCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("dispatch command: %v", err)
	}
	if result == nil {
		t.Fatal("expected command result")
	}
	return result
}

func (h *supervisorHarness) stop() {
	if h.httpServer != nil {
		h.httpServer.Close()
	}
	if h.pipeline != nil {
		h.pipeline.Close()
	}
}

func (h *supervisorHarness) closeNodeConnection(nodeID string) {
	h.mu.RLock()
	conn, ok := h.connections[nodeID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	_ = conn.conn.Close()
}

func (h *supervisorHarness) setHeartbeatPaused(nodeID string, paused bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if conn, ok := h.connections[nodeID]; ok {
		conn.heartbeatPause = paused
	}
}

func (h *supervisorHarness) waitForNodeCount(t *testing.T, count int, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, func() bool {
		return len(h.registry.ListNodes()) >= count
	}, "node count")
}

func (h *supervisorHarness) waitForNodeStatus(t *testing.T, nodeID string, status supervisor.NodeStatus, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, func() bool {
		node, err := h.registry.GetNode(nodeID)
		return err == nil && node.Status == status
	}, fmt.Sprintf("node %s status %s", nodeID, status))
}

func (h *supervisorHarness) waitForSession(t *testing.T, sessionID string, timeout time.Duration) supervisor.TrackedSession {
	t.Helper()
	var found supervisor.TrackedSession
	waitFor(t, timeout, func() bool {
		sess, err := h.tracker.GetSession(sessionID)
		if err != nil {
			return false
		}
		found = sess
		return true
	}, "session created")
	return found
}

func (h *supervisorHarness) allEventIDs() []string {
	rows, err := h.db.Query(`SELECT id FROM events ORDER BY timestamp, id`)
	if err != nil {
		h.t.Fatalf("query events: %v", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			h.t.Fatalf("scan event id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func setupIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "integration-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	_ = tmpFile.Close()

	db, err := sql.Open("sqlite", tmpFile.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(tmpFile.Name())
	})

	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}

	runner := storage.NewMigrationRunner(db)
	if err := runner.Migrate(); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	return db
}

type agentHarness struct {
	t            *testing.T
	nodeID       string
	projects     []string
	adapter      *agent.MockOpencodeAdapter
	wsClient     *agent.WSClient
	ctx          context.Context
	cancel       context.CancelFunc
	eventCancel  context.CancelFunc
	eventSeq     atomic.Uint64
	hbPaused     atomic.Bool
	snapshotCall atomic.Int64
}

func newAgentHarness(t *testing.T, h *supervisorHarness, nodeID string, projects []string, existing *agent.MockOpencodeAdapter) *agentHarness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	adapter := existing
	if adapter == nil {
		adapter = agent.NewMockOpencodeAdapter()
	}

	ag := &agentHarness{
		t:        t,
		nodeID:   nodeID,
		projects: projects,
		adapter:  adapter,
		ctx:      ctx,
		cancel:   cancel,
	}

	ag.wsClient = agent.NewWSClient(
		h.wsURL(nodeID, projects),
		h.token,
		zap.NewNop(),
		agent.WithBackoff(&agent.Backoff{Min: 20 * time.Millisecond, Max: 150 * time.Millisecond, Factor: 2, Jitter: 0.1}),
		agent.WithSnapshotProvider(ag.snapshot),
		agent.WithMessageHandler(ag.handleMessage),
	)
	if err := agent.RegisterSessionCommandHandlers(ag.wsClient, ag.adapter, zap.NewNop()); err != nil {
		t.Fatalf("register session command handlers: %v", err)
	}

	ag.wsClient.Connect(ctx)
	ag.startHeartbeats()
	ag.startEventForwarder()

	t.Cleanup(func() {
		ag.stop()
	})

	return ag
}

func (a *agentHarness) snapshot() *agent.StateSnapshot {
	a.snapshotCall.Add(1)
	sessions, _ := a.adapter.ListSessions(context.Background())
	out := make([]agent.SessionSnapshot, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, agent.SessionSnapshot{
			SessionID: string(sess.ID),
			Project:   sess.Project,
			Status:    string(sess.Status),
			StartedAt: time.Now().UTC().Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return &agent.StateSnapshot{Sessions: out, LastSeq: int64(a.eventSeq.Load())}
}

func (a *agentHarness) startHeartbeats() {
	ticker := time.NewTicker(80 * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				if a.hbPaused.Load() {
					continue
				}
				env := &shared.Envelope{
					Version:   shared.ProtocolVersion,
					Type:      string(shared.MessageTypeHeartbeat),
					Timestamp: time.Now().UTC().Unix(),
					Payload:   json.RawMessage(`{"status":"ok"}`),
				}
				_ = a.wsClient.SendEnvelope(env)
			}
		}
	}()
}

func (a *agentHarness) startEventForwarder() {
	evtCtx, evtCancel := context.WithCancel(a.ctx)
	a.eventCancel = evtCancel

	ch, err := a.adapter.SubscribeEvents(evtCtx)
	if err != nil {
		a.t.Fatalf("subscribe adapter events: %v", err)
	}

	go func() {
		for {
			select {
			case <-evtCtx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				seq := a.eventSeq.Add(1)
				payload, _ := json.Marshal(supervisor.Event{
					ID:        fmt.Sprintf("%s-%06d", a.nodeID, seq),
					SessionID: string(evt.SessionID),
					Type:      evt.Type,
					Data:      evt.Payload,
					Timestamp: time.Now().UTC(),
					Seq:       seq,
				})

				env := &shared.Envelope{
					Version:   shared.ProtocolVersion,
					Type:      string(shared.MessageTypeEvent),
					Timestamp: time.Now().UTC().Unix(),
					Payload:   payload,
				}
				_, _ = a.wsClient.SendEvent(env)
			}
		}
	}()
}

func (a *agentHarness) handleMessage(env *shared.Envelope) error {
	if env.Type != string(shared.MessageTypeCommand) {
		return nil
	}

	var cmd supervisor.Command
	if err := json.Unmarshal(env.Payload, &cmd); err != nil {
		return err
	}

	result := supervisor.CommandResult{
		CommandID: cmd.CommandID,
		Status:    supervisor.CommandStatusSuccess,
		Timestamp: time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	switch cmd.Type {
	case supervisor.CommandTypeCreateSession:
		prompt := readStringArg(cmd.Args, "prompt")
		sessionID, err := a.adapter.CreateSession(ctx, cmd.Target.Project, prompt)
		if err != nil {
			result.Status = supervisor.CommandStatusFailure
			result.Error = err.Error()
			break
		}
		result.Output = string(sessionID)
	case supervisor.CommandTypePromptSession:
		sessionID := agent.SessionID(readStringArg(cmd.Args, "session_id"))
		message := readStringArg(cmd.Args, "message")
		if message == "" {
			message = readStringArg(cmd.Args, "prompt")
		}
		if err := a.adapter.PromptSession(ctx, sessionID, message); err != nil {
			result.Status = supervisor.CommandStatusFailure
			result.Error = err.Error()
		}
	case supervisor.CommandTypeKillSession:
		sessionID := agent.SessionID(readStringArg(cmd.Args, "session_id"))
		if err := a.adapter.KillSession(ctx, sessionID); err != nil {
			result.Status = supervisor.CommandStatusFailure
			result.Error = err.Error()
		}
	case supervisor.CommandTypeSessionStatus:
		sessionID := agent.SessionID(readStringArg(cmd.Args, "session_id"))
		status, err := a.adapter.SessionStatus(ctx, sessionID)
		if err != nil {
			result.Status = supervisor.CommandStatusFailure
			result.Error = err.Error()
			break
		}
		result.Output = string(status)
	case supervisor.CommandTypeRestartSession:
		sessionID := agent.SessionID(readStringArg(cmd.Args, "session_id"))
		_ = a.adapter.KillSession(ctx, sessionID)
		newSessionID, err := a.adapter.CreateSession(ctx, cmd.Target.Project, "restart")
		if err != nil {
			result.Status = supervisor.CommandStatusFailure
			result.Error = err.Error()
			break
		}
		result.Output = string(newSessionID)
	default:
		result.Status = supervisor.CommandStatusFailure
		result.Error = fmt.Sprintf("unsupported command type %s", cmd.Type)
	}

	payload, _ := json.Marshal(result)
	response := &shared.Envelope{
		Version:   shared.ProtocolVersion,
		Type:      string(shared.MessageTypeCommandResult),
		RequestID: cmd.CommandID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}
	return a.wsClient.SendEnvelope(response)
}

func (a *agentHarness) stop() {
	if a.eventCancel != nil {
		a.eventCancel()
	}
	a.cancel()
	_ = a.wsClient.Close()
}

func (a *agentHarness) setHeartbeatPaused(paused bool) {
	a.hbPaused.Store(paused)
}

func (a *agentHarness) emitEvents(count int, sessionID string) {
	for i := 0; i < count; i++ {
		a.adapter.EmitEvent(agent.Event{Type: "message.updated", SessionID: agent.SessionID(sessionID), Payload: json.RawMessage(fmt.Sprintf(`{"i":%d}`, i))})
	}
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool, label string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}

func readStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func splitCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
