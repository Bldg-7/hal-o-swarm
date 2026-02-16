# Decisions: HAL-O-SWARM Implementation

## Architectural Choices

_(To be populated as implementation progresses)_

## T2: Protocol Envelope Design

### Envelope Structure
- **Version (int)**: Protocol version for forward compatibility. Current: 1
- **Type (string)**: Message type (event, command, heartbeat)
- **RequestID (string)**: Optional correlation ID for request-response tracking
- **Timestamp (int64)**: Unix timestamp in seconds (required, non-zero)
- **Payload (json.RawMessage)**: Arbitrary JSON payload, allows lazy unmarshaling

### Validation Strategy
1. **Version Validation**: Strict version checking on both marshal and unmarshal
   - Rejects version != ProtocolVersion (1)
   - Returns ErrUnsupportedVersion with context (actual vs expected)
   - Enables future protocol evolution with clear rejection

2. **Required Field Validation**:
   - Type: Must be non-empty string
   - Timestamp: Must be non-zero (0 is invalid)
   - RequestID: Optional (can be empty)
   - Payload: Optional (can be empty JSON object)

3. **Error Handling**:
   - Validation errors wrapped with context using fmt.Errorf
   - errors.Is() used for error type checking in tests
   - Unmarshal errors include JSON parsing context

### Implementation Details
- MarshalEnvelope: Validates before marshaling (fail-fast)
- UnmarshalEnvelope: Validates after unmarshaling (ensures data integrity)
- json.RawMessage for payload: Defers parsing to message handlers
- Timestamp as int64: Supports Unix timestamps up to year 292 billion

### Testing Coverage
- Roundtrip test: Verifies all fields preserved through marshal/unmarshal
- Version validation: Both marshal and unmarshal reject invalid versions
- Required field validation: Type and timestamp enforcement
- Optional fields: RequestID and payload can be empty
- Multiple message types: Event, command, heartbeat all supported

### Future Extensibility
- Version field allows protocol evolution
- Payload as json.RawMessage allows new message formats without struct changes
- Error types (ErrUnsupportedVersion, etc.) enable version-specific handling

## T5: Agent Configuration & Project Registry

### Decision: One OpenCode Serve Per Project

**Rationale**: The agent manages multiple projects, each with its own opencode serve instance. This architecture:
- Isolates project sessions (one project's crash doesn't affect others)
- Enables independent resource management per project
- Simplifies session routing (project name → serve instance)
- Aligns with spec requirement: "Start and manage opencode serve on the local machine"

**Implementation**:
- `ProjectRegistry`: Validates and stores project configurations
- Each project must have a valid directory on the filesystem
- Agent skeleton prepared for T10 (process management)

### Decision: Fail-Fast Project Validation

**Rationale**: Invalid project paths are detected at startup, not at runtime.
- Nonexistent directories → clear error message with path
- Duplicate project names → rejected with error
- Non-directory paths → rejected with error

This prevents silent failures and makes debugging easier.

### Decision: Agent Lifecycle Management

**Rationale**: Agent follows standard lifecycle:
1. Load config from file
2. Validate projects (registry initialization)
3. Start (T10: spawn opencode processes, T7: connect supervisor)
4. Stop (graceful shutdown)

Implemented with:
- `Agent.Start(ctx)` / `Agent.Stop(ctx)` for lifecycle
- `Agent.IsRunning()` for state tracking
- Mutex protection for concurrent access

### Decision: Config Loading Separation

**Rationale**: Config loading (T1) is separate from agent initialization (T5).
- `config.LoadAgentConfig()` handles file I/O and JSON parsing
- `agent.NewAgent()` handles project validation and registry setup
- Clear separation of concerns

### Blocking Dependencies

- T5 blocks T7 (WebSocket client needs agent running)
- T5 blocks T10 (process manager needs project registry)
- T5 blocks T17 (env checker needs agent context)
- T5 blocks T18 (provisioner needs agent context)


## Storage & Migration Architecture (Task 3)

### Database Choice
- **SQLite with modernc.org/sqlite**: Pure Go implementation, no CGO dependency
- **WAL Mode**: Enabled by default in migration runner for better concurrency
- **Location**: Embedded migrations in `internal/storage/migrations/`

### Schema Design

#### Tables

1. **events** - Session activity log
   - `id` (TEXT PRIMARY KEY): Unique event identifier
   - `session_id` (TEXT FK): Reference to sessions table
   - `type` (TEXT): Event type (e.g., "session.start", "session.error")
   - `data` (TEXT): JSON-encoded event payload
   - `timestamp` (DATETIME): Event occurrence time
   - Index: `idx_events_session_ts` on (session_id, timestamp)

2. **sessions** - Agent session tracking
   - `id` (TEXT PRIMARY KEY): Unique session identifier
   - `node_id` (TEXT FK): Reference to nodes table
   - `project` (TEXT): Project name/identifier
   - `status` (TEXT): Session state (running, idle, error, completed)
   - `tokens` (INTEGER): Cumulative token usage
   - `cost` (REAL): Estimated cost in USD
   - `started_at` (DATETIME): Session start time
   - Index: `idx_sessions_status` on (status)

3. **nodes** - Agent node registry
   - `id` (TEXT PRIMARY KEY): Node identifier (hostname-based)
   - `hostname` (TEXT): Full hostname
   - `status` (TEXT): Node state (connected, disconnected, unhealthy)
   - `last_heartbeat` (DATETIME): Last heartbeat timestamp
   - `connected_at` (DATETIME): Connection establishment time

4. **costs** - Cost aggregator data
   - `id` (TEXT PRIMARY KEY): Unique cost record identifier
   - `provider` (TEXT): LLM provider (Anthropic, OpenAI, Google)
   - `model` (TEXT): Model name (e.g., "Sonnet 4.5", "o3")
   - `date` (DATE): Cost aggregation date
   - `tokens` (INTEGER): Token count for period
   - `cost_usd` (REAL): Cost in USD
   - Index: `idx_costs_date` on (date)

5. **command_idempotency** - Command deduplication
   - `key_hash` (TEXT PRIMARY KEY): SHA256 hash of command + parameters
   - `command_id` (TEXT): Unique command execution identifier
   - `result` (TEXT): Serialized command result
   - `expires_at` (DATETIME): TTL for idempotency record
   - Index: `idx_idempotency_key` on (key_hash)

6. **schema_migrations** - Migration tracking (auto-created)
   - `version` (TEXT PRIMARY KEY): Migration version (e.g., "001")
   - `checksum` (TEXT): MD5 checksum of migration SQL
   - `applied_at` (DATETIME): Application timestamp

### Migration System

- **Versioning**: Numeric prefix (001, 002, etc.) in filename
- **Checksum Validation**: MD5 hash prevents accidental modification
- **Idempotency**: Rerunning migrations is safe; already-applied migrations are skipped
- **Fail-Fast**: Checksum mismatch immediately halts migration with clear error
- **Embedded Files**: Migrations compiled into binary via `//go:embed`

### Design Rationale

- **Foreign Keys**: Enabled for referential integrity (events → sessions, sessions → nodes)
- **Timestamps**: DATETIME with DEFAULT CURRENT_TIMESTAMP for audit trail
- **Indexes**: Strategic placement on query paths (session lookups, cost date ranges, idempotency checks)
- **Cost Tracking**: Separate table allows independent cost aggregation without session coupling
- **Command Idempotency**: Prevents duplicate execution of intervention commands via hash-based deduplication


## T9: Event Ordering, Dedup, and Replay

### Event Ordering Pattern
- Per-agent monotonic sequence is enforced with `lastSeq` state (`expected = lastSeq + 1`).
- `seq < expected` is treated as stale/old and ignored.
- `seq == expected` is processed immediately, then pending buffered events are drained in-order.
- `seq > expected` is treated as a gap; event is queued until missing range arrives.

### Dedup Pattern
- Dedup key is scoped per agent by event ID using per-agent LRU caches.
- LRU cache size is capped at 1000 IDs per agent to avoid unbounded growth.
- Duplicate events are ignored idempotently before sequence processing.

### Replay Pattern
- On sequence gap detection, supervisor sends `RequestEventRange{From: expected, To: received-1}` to the originating agent.
- Out-of-order events are held in per-agent pending queues keyed by sequence.
- Replay arrivals close gaps and trigger deterministic in-order drain of queued events.

### Persistence Pattern
- Event persistence is asynchronous via a bounded queue and background worker.
- Pipeline processing does not block on DB writes; write failures are logged as warnings.
## T8: Supervisor State Management (Registry + Tracker)

### Decision: Persistent state with in-memory cache front
- `NodeRegistry` and `SessionTracker` use SQLite as source of truth and keep a mutex-protected in-memory map for hot reads.
- Every mutating operation writes through to DB first (`INSERT ... ON CONFLICT DO UPDATE`) and then updates cache.
- This guarantees restart recovery without falling back to in-memory-only behavior.

### Decision: Recovery boot posture is pessimistic
- On supervisor startup, `LoadNodesFromDB()` marks all node rows `offline` before loading.
- On supervisor startup, `LoadSessionsFromDB()` marks all session rows `unreachable` before loading.
- This avoids stale optimistic state after crashes or unclean disconnects.

### Decision: Explicit transition hooks for reconnect/disconnect
- `Register()` moves node state to `online` with fresh heartbeat.
- `MarkOffline()` transitions nodes to `offline` (disconnect and heartbeat timeout path).
- `MarkUnreachable(nodeID)` transitions node-owned sessions to `unreachable`.
- `RestoreFromSnapshot(nodeID, sessions)` restores session states (`running`/`idle`) on reconnect.

### Decision: Guarded row recovery with metric-compatible counter
- Row-level parse/scan failures during load are treated as recoverable corruption.
- Corrupted rows are logged and skipped; valid rows continue loading.
- `RecoveryErrorCount()` tracks increments as a drop-in metric source for T22 observability wiring.

## T10: Opencode Adapter Pattern

### Decision: Adapter-Only SDK Access

All `opencode-sdk-go` usage is centralized in `internal/agent/opencode_adapter.go` behind `OpencodeAdapter`, so agent workflows call a mockable interface instead of SDK services directly.

### Decision: Per-Project Client Registry

`RealAdapter` stores `projectClients[projectName]` and `projectDirs[projectName]` to respect one-opencode-serve-per-project routing, while `sessionProjects[sessionID]` pins follow-up operations (`Prompt`, `Abort/Delete`, `Get`) to the correct project client.

### Decision: Global SSE Stream with Session Demux

`SubscribeEvents` opens SDK `Event.ListStreaming()` stream(s), normalizes payload to adapter `Event`, extracts `sessionID` from event properties, and fans out both to global subscribers and per-session channels (`SubscribeSessionEvents`) without blocking producers.

### Decision: Error Class Mapping

Adapter error mapping standardizes retries and terminal failures:
- Network/timeouts/5xx/429 -> `ErrRecoverable`
- Auth (401/403) -> `ErrNonRecoverable`
- Missing sessions (404) -> `ErrSessionNotFound`

This gives caller-side policy control without leaking SDK-specific error handling.
