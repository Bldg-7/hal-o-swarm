# Learnings: HAL-O-SWARM Implementation

## Conventions

_(To be populated as implementation progresses)_

## Patterns

_(To be populated as implementation progresses)_

## Task 1: Module Layout & Config Validation

### Completed Artifacts

**Directory Structure** (per spec 735-789):
- `cmd/supervisor/`, `cmd/agent/`, `cmd/halctl/` - entry point stubs
- `internal/supervisor/` - registry, tracker, router, cost, commands, depgraph, envapi
- `internal/agent/` - proxy, forwarder, wsclient, envcheck, provision
- `internal/halctl/` - env, agentmd, session
- `internal/shared/` - types, protocol, envtypes
- `internal/config/` - config validation package
- `templates/` - AGENT.md template directories

**Config Validation Package** (`internal/config/`):
- `supervisor.go` - SupervisorConfig with strict validation (port range, auth token, heartbeat intervals)
- `agent.go` - AgentConfig with strict validation (supervisor URL, auth token, port range, non-empty projects)
- `env_manifest.go` - EnvManifest with strict validation (version, non-empty requirements)
- `config_test.go` - 13 test cases covering happy path (example configs) and error paths (field-level validation)

**Example Configs**:
- `supervisor.config.example.json` - All required fields with documentation
- `agent.config.example.json` - All required fields with documentation
- `env-manifest.example.json` - All required fields with documentation

**Validation Behavior**:
- All validation errors include explicit field path (e.g., "validation error: server.port must be between 1 and 65535, got 0")
- Malformed JSON returns parse error with character position
- Empty arrays/missing required fields caught at validation layer

### Test Results

**Happy Path** (example configs load successfully):
- TestLoadSupervisorConfigExample: PASS
- TestLoadAgentConfigExample: PASS
- TestLoadEnvManifestExample: PASS

**Error Path** (validation catches all invalid inputs):
- TestSupervisorConfigValidationInvalidPort: PASS
- TestSupervisorConfigValidationMissingAuthToken: PASS
- TestSupervisorConfigValidationInvalidHeartbeatInterval: PASS
- TestAgentConfigValidationMissingSupervisorURL: PASS
- TestAgentConfigValidationMissingAuthToken: PASS
- TestAgentConfigValidationInvalidPort: PASS
- TestAgentConfigValidationEmptyProjects: PASS
- TestEnvManifestValidationMissingVersion: PASS
- TestEnvManifestValidationEmptyRequirements: PASS
- TestMalformedConfigFile: PASS

**Module Verification**:
- `go list ./...` succeeds with 9 packages (cmd/*, internal/*)
- All packages compile without errors
- No Slack-specific runtime wiring in config layer

### Key Patterns

1. **Validation Strategy**: Separate validation functions per config type, explicit error messages with field paths
2. **Config Structure**: Nested structs with JSON tags matching spec (e.g., server.port, supervisor_url)
3. **Test Organization**: Separate test functions per validation rule (not table-driven) for clarity
4. **Example Configs**: All required fields present with placeholder values, documented inline

### Blockers Resolved

- None - Task completed without blockers

### Next Steps (T4, T5, T16, T20)

- T4: Implement supervisor registry and connection management
- T5: Implement agent proxy and WebSocket client
- T16: Add environment provisioning API handlers
- T20: Implement CLI commands (halctl)


## Supervisor Lifecycle Patterns (T4)

### Context-Based Graceful Shutdown
- Use `context.WithCancel()` to create cancellable context at server initialization
- Pass context to all goroutines for coordinated shutdown
- Signal handler calls `cancel()` to propagate shutdown signal
- All goroutines listen on `<-ctx.Done()` to exit cleanly

### Server Lifecycle State Management
- Use `sync.Mutex` to protect `running` boolean state
- Prevent double-start: check `running` before Start()
- Prevent double-stop: check `running` before Stop()
- Update state atomically with mutex held

### Goroutine Cleanup Pattern
- Use `sync.WaitGroup` to track all spawned goroutines
- Call `wg.Add(1)` before spawning, `wg.Done()` in goroutine
- In Stop(), call `wg.Wait()` with timeout to ensure cleanup
- Use channel to signal completion: `done := make(chan struct{}); go func() { wg.Wait(); close(done) }()`

### Signal Handling in main()
- Create buffered channel: `sigChan := make(chan os.Signal, 1)`
- Register signals: `signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)`
- Block on receive: `sig := <-sigChan`
- Log signal received with context before shutdown

### Config Validation Before Startup
- Validate config in LoadSupervisorConfig() before returning
- Check required fields: auth_token, port range, positive intervals
- Return clear error messages with field names and constraints
- Fail fast: exit(1) if config invalid, never start with bad config

### Logger Initialization Pattern
- Initialize logger early in main() before any other setup
- Use `defer logger.Sync()` to flush buffered logs on exit
- Log startup/shutdown events with structured fields (port, intervals, etc.)
- Use appropriate log levels: Info for lifecycle, Error for failures, Debug for details

### Port Binding Verification
- In Start(), attempt to bind to configured port with net.Listen()
- Verify port is available before marking server as running
- Return error if bind fails (port in use, permission denied)
- Defer listener.Close() to clean up immediately after verification

### Testing Lifecycle with Race Detector
- Run tests with `-race` flag: `go test -race ./internal/supervisor/...`
- Race detector catches data races in concurrent access
- Mutex protection prevents race conditions on shared state
- All tests pass with race detector enabled = safe concurrent code

## WebSocket Reconnect Patterns (T7)

### Jittered Exponential Backoff
- Min: 100ms, Max: 60s, Factor: 2.0, Jitter: ±25%
- Jitter uses uniform random in [-jitter, +jitter] range to prevent thundering herd
- Clamp final value to [Min, Max] after applying jitter
- Reset attempt counter on successful connection
- Manual implementation preferred over external lib (simple enough, no dep needed)

### Connection Loop Pattern
- Background goroutine runs `connectLoop(ctx)` with deferred `close(done)`
- Each iteration: dial → send snapshot → resend pending → readLoop
- On error: log, compute backoff, select between ctx.Done and timer
- Close(): cancel context + close connection (unblocks ReadMessage) + wait on done channel

### State Snapshot on Reconnect
- SnapshotProvider callback collects current sessions with status/tokens/cost
- Sent as REGISTER envelope immediately after successful dial
- Includes lastAckedSeq so supervisor can detect event gaps

### Sequence-Aware Event Resend
- Each SendEvent assigns monotonically increasing sequence number
- Events buffered in pendingEvents slice until AcknowledgeSeq(n) called
- AcknowledgeSeq removes all events with seq <= n
- On reconnect, all remaining pending events are resent in order

### Auth Pattern
- Authorization header: "Bearer <token>" sent in WebSocket handshake
- gorilla/websocket Dialer supports custom headers via DialContext

### Testing Patterns
- httptest.NewServer + websocket.Upgrader for mock WS server
- connCh channel for synchronizing on connection events
- Close server-side connection to trigger client reconnect (server stays up)
- Port 1 (127.0.0.1:1) for unreachable server tests
- Fast backoff (10ms min, 100ms max) for test speed
- atomic.Int32 for thread-safe counters in test assertions

## WebSocket Hub Implementation Patterns (T6)

### Gorilla Hub Pattern
- Central Hub struct with register/unregister/broadcast channels
- Single Run() goroutine processes all channel operations (no concurrent map access)
- sync.RWMutex protects clients map for concurrent reads (ClientCount) while Run() holds write lock
- Non-blocking broadcast: `select { case conn.send <- msg: default: close(conn.send); delete(...) }`

### Auth During WebSocket Upgrade
- Check Authorization header (Bearer token) OR query param (?token=) BEFORE calling upgrader.Upgrade
- Return http.Error(w, "Unauthorized", 401) for invalid/missing token — gorilla Dialer receives this as resp.StatusCode
- Origin validation via upgrader.CheckOrigin callback — gorilla returns 403 automatically on rejection

### Ping/Pong Protocol Constants
- writeWait: 10s (deadline for all writes)
- pongWait: 60s (read deadline, extended on each pong)
- pingPeriod: 54s (90% of pongWait — ensures ping arrives before read deadline expires)
- maxMessageSize: 64KB

### Heartbeat vs Ping/Pong (Two Separate Mechanisms)
- Ping/pong: WebSocket-level connection liveness (handled by gorilla)
- Application heartbeat: Agent sends Envelope{type:"heartbeat"} every 30s
- Hub checks every heartbeatInterval; if time.Since(lastHeartbeat) > interval * timeoutCount → node.offline event + close

### Connection Lifecycle Safety
- readPump defers: `select { case hub.unregister <- c: case <-hub.ctx.Done(): }` — prevents goroutine leak on shutdown
- writePump detects closed send channel (ok=false) → sends CloseMessage → exits
- Hub.Run ctx.Done case closes all connections and returns
- conn.conn.Close() is idempotent (net.Conn.Close on already-closed returns error, no panic)

### Testing WebSocket with httptest
- httptest.NewServer + http.ServeMux for test server
- websocket.DefaultDialer.Dial returns (*Conn, *http.Response, error)
- On auth failure: resp.StatusCode == 401, err != nil
- On origin failure: resp.StatusCode == 403, err != nil
- Short heartbeat intervals (50ms) for fast timeout tests
- Always drain Events() channel between assertions to avoid blocking

## T14: Auto-Intervention Policy Engine

### Retry Ceiling Pattern
- Keep retry counters in `map[sessionID]map[policyName]retryState` guarded by mutex.
- Gate each intervention with `count >= maxRetries` and `lastAttempt + retryResetWindow` to prevent infinite loops.
- Reset retry counter on successful action so transient failures do not permanently block future interventions.

### Action Routing Pattern
- Read candidate sessions from `SessionTracker.GetAllSessions()` only; do not mutate tracker state directly.
- Execute interventions exclusively through `CommandDispatcher.DispatchCommand()` with canonical command types (`prompt_session`, `restart_session`, `kill_session`).
- Include `session_id` and `policy` in command args for traceability of automated interventions.

### Policy Event Pattern
- Emit `policy.action` for every attempt outcome (`success`/`failure`) with retry count.
- Emit `policy.alert` and `policy.retry_cap` when a session hits max retries.
- Route policy events through T9 `EventPipeline.ProcessEvent()` with monotonic local sequence IDs.

### Graceful Shutdown Pattern
- Drive periodic checks with `time.NewTicker` inside a goroutine that exits on `ctx.Done()`.
- `Stop()` cancels context and waits briefly for the worker, so supervisor shutdown is not blocked by policy checks.

## T12: Discord Slash Command Integration

### DiscordSession Interface Pattern
- Abstract `*discordgo.Session` behind an interface (`DiscordSession`) with only the methods used: `AddHandler`, `Open`, `Close`, `ApplicationCommandCreate`, `ApplicationCommandDelete`, `InteractionRespond`, `FollowupMessageCreate`, `State`.
- `realDiscordSession` wraps the concrete session; tests inject `mockDiscordSession`.
- This avoids needing a real Discord token or network for unit tests.

### Deferred Response Flow (3s Timeout)
- On `InteractionCreate`, immediately call `InteractionRespond` with `InteractionResponseDeferredChannelMessageWithSource`.
- Execute command (dispatcher or direct query) synchronously after acknowledging.
- Send result as `FollowupMessageCreate` with embed payload.
- This pattern ensures Discord never times out the interaction.

### Command Routing Strategy
- Commands that modify state (`/start`, `/kill`, `/restart`, `/resume`, `/inject`, `/status`) go through `CommandDispatcher.DispatchCommand()`.
- Direct-query commands (`/nodes`, `/logs`, `/cost`) read from Hub, SessionTracker, or aggregate data directly.
- Session-targeted commands (`/inject`, `/restart`, `/kill`) resolve session → node via `SessionTracker.GetSession()` before dispatching.

### Error Sanitization
- All `CommandResult.Error` strings pass through `sanitizeError()` which maps known internal patterns to user-safe messages.
- Patterns: "not connected" → "Target node is not connected.", "offline" → "Target node is offline.", "timed out" → "The command timed out."
- Unknown errors get generic fallback: "An error occurred while processing the command."
- Never expose node IDs, file paths, or stack traces to Discord users.

### Embed Color Convention
- Success (0x00CC66): green for successful commands.
- Failure (0xCC3333): red for command failures and validation errors.
- Timeout (0xFF9900): orange for timed-out commands.
- Info (0x3399FF): blue for informational queries (/nodes, /logs, /cost).

### Testing Discord Bots Without API
- `mockDiscordSession` captures all `InteractionRespond` and `FollowupMessageCreate` calls.
- `simulateInteraction()` helper constructs `InteractionCreate` events with typed options.
- `discordgo.State` requires setting `User` via the embedded `Ready` struct: `s.User = &discordgo.User{ID: "..."}` (not struct literal).
- Reuse `mockCommandTransport` from T11 tests; async `HandleCommandResult` with short sleep simulates real dispatch.

### Config Wiring
- Config already had `channels.discord.bot_token` and `channels.discord.guild_id` fields.
- Discord bot lifecycle: create in `main()` if token is non-empty, `Start()` after server, `Stop()` before server during shutdown.
- Non-fatal: if bot creation or start fails, log error but don't crash the supervisor.

## T13: Supervisor HTTP API

### Go 1.22+ ServeMux Routing
- `net/http.ServeMux` supports method+path patterns: `"GET /api/v1/sessions/{id}"`.
- Path parameters via `r.PathValue("id")` — no external router dependency needed.
- Register patterns on a sub-mux, then mount under versioned prefix with `http.StripPrefix`.

### Bearer Token Auth Middleware
- Reuse same `cfg.Server.AuthToken` as WebSocket hub auth.
- Middleware wraps `http.Handler`; checks `Authorization: Bearer <token>` header.
- `/api/v1/health` excluded from auth (registered outside middleware wrapper).
- Return `401 Unauthorized` with JSON error envelope on failure.

### Consistent JSON Response Envelopes
- Success: `{"data": ..., "meta": {"count": N}}` with `200 OK`.
- Error: `{"error": "message", "code": N}` with appropriate HTTP status.
- Always set `Content-Type: application/json` via middleware, even on errors.

### Read-Only Queries + Command Dispatch
- All GET endpoints query SQLite directly (sessions, nodes, events tables).
- All mutations go through `POST /api/v1/commands` → `CommandDispatcher.DispatchCommand()` (T11).
- If dispatcher is nil (not configured), return `503 Service Unavailable`.

### Query Filtering Pattern
- Sessions: `?project=`, `?status=`, `?node_id=`, `?limit=` with dynamic WHERE clause building.
- Events: `?session_id=`, `?type=`, `?since=` (RFC3339), `?limit=` with same pattern.
- Default limit: 100. Build query with `[]string` conditions and `[]any` args, join with AND.

### SQLite Timestamp Parsing
- Reuse `parseSQLiteTimestamp()` from `registry.go` for consistent time parsing.
- SQLite stores timestamps as text; parse with multiple format attempts.

### HTTP Server Lifecycle Integration
- `HTTPAPI` struct created in `main()`, set on Server via `SetHTTPAPI()`.
- Server.Start() launches HTTP listener in background goroutine if HTTPAPI is set.
- Server.Stop() calls `httpShutdown(ctx)` for graceful drain before cancelling main context.
- Config: `server.http_port` (0 = disabled, 1-65535 = enabled).

### Testing HTTP APIs
- `httptest.NewRecorder()` + `handler.ServeHTTP()` for unit tests (no real server needed).
- Helper functions: `setupHTTPAPI()`, `authRequest()` for DRY test setup.
- FK constraints require seeding in order: nodes → sessions → events.
- In-memory SQLite with `?_foreign_keys=on` for realistic constraint testing.
