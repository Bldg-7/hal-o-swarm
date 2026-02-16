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
