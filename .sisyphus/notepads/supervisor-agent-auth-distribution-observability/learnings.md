# Auth Types Implementation Learnings

## Task 1: Auth Domain Types and Shared Enums

### Completed
- Created `internal/shared/auth_types.go` with canonical auth state types
- Implemented enum pattern matching `DriftCategory`/`DriftStatus` from `envtypes.go`
- Added all required types: ToolIdentifier, AuthStatus, AuthStateReport, CredentialPushPayload
- Updated `types.go` with MessageTypeAuthState and MessageTypeConfigUpdate constants

### Type Definitions

#### ToolIdentifier (enum)
- `opencode` - OpenCode tool
- `claude_code` - Claude Code tool
- `codex` - Codex tool

#### AuthStatus (enum)
- `authenticated` - Tool is authenticated and ready
- `unauthenticated` - Tool exists but not authenticated
- `not_installed` - Tool is not installed on node
- `error` - Error occurred during auth check
- `manual_required` - Manual intervention required for auth

#### AuthStateReport (struct)
- `Tool` (ToolIdentifier) - Which tool this report is for
- `Status` (AuthStatus) - Current auth status
- `Reason` (string) - Human-readable reason for status
- `CheckedAt` (time.Time) - When the status was checked

#### CredentialPushPayload (struct)
- `TargetNode` (string) - Node identifier to push credentials to
- `EnvVars` (map[string]string) - Environment variables to set (carries credentials)
- `Version` (int) - Payload version for compatibility

### Testing Strategy
- `TestAuthStatusJSONRoundTrip` - Verifies all AuthStatus enum values serialize/deserialize correctly
- `TestToolIdentifierJSONRoundTrip` - Verifies all ToolIdentifier enum values serialize/deserialize correctly
- `TestCredentialPushPayloadJSONRoundTrip` - Verifies CredentialPushPayload round-trips correctly
- `TestAuthStatusNoSecretField` - Reflection-based guard: no "key", "secret", "token", "password" fields in AuthStateReport
- `TestCredentialPushPayloadNoSecretField` - Reflection-based guard: no forbidden fields in CredentialPushPayload

### Key Design Decisions

1. **Enum Pattern**: Used `type X string` with `const` block, matching existing codebase patterns
2. **JSON Tags**: All struct fields use snake_case JSON tags for protocol consistency
3. **No Embedded Secrets**: AuthStateReport contains no credential values, only status information
4. **Credential Transport**: CredentialPushPayload uses EnvVars map for intentional credential distribution
5. **Message Types**: Added to types.go constants for protocol envelope routing

### Test Results
- All 6 tests pass (5 round-trip tests + 2 guard tests)
- No LSP errors in auth_types.go or auth_types_test.go
- Protocol version unchanged (still 1)
- No external dependencies added

### Integration Points
- AuthStateReport used for observability: agents report auth status to supervisor
- CredentialPushPayload used for distribution: supervisor pushes credentials to agents
- Message types enable protocol routing via Envelope.Type field
- All types are WebSocket-serializable via JSON marshaling

### Next Steps (for future tasks)
- Implement auth state reporting in agent
- Implement credential push handling in agent
- Add supervisor-side auth state aggregation
- Implement auth status dashboard/API endpoint

---

## Task 4: Tool Capability Matrix

### Completed
- Created `internal/agent/tool_capabilities.go` with static metadata for all 3 tools
- Implemented `ToolID` type with constants: `ToolOpencode`, `ToolClaudeCode`, `ToolCodex`
- Implemented `ToolCapability` struct with all required fields
- Created `GetToolCapabilities()` returning map[ToolID]ToolCapability
- Created `GetToolCapability(id ToolID)` returning capability or nil
- All tests pass: TestToolCapabilityMatrix, TestToolCapabilityUnknownDefaultsManual, TestGetToolCapabilitiesReturnsAllThree

### Tool Capability Matrix

#### opencode
- **Status Command**: `["opencode", "auth", "list"]`
- **Status Parser**: `output_parse` (parses command output for credential info)
- **Remote OAuth**: `true` (supports device code flow for headless environments)
- **OAuth Flows**: `["device_code"]`
- **Manual Fallback**: SSH into agent and run: `opencode auth login`

#### claude_code
- **Status Command**: `["claude", "auth", "status"]`
- **Status Parser**: `exit_code` (0=authenticated, 1=not authenticated)
- **Remote OAuth**: `false` (NO device code flow available)
- **OAuth Flows**: `[]` (empty - must use manual login)
- **Manual Fallback**: SSH into agent and run: `claude auth login`

#### codex
- **Status Command**: `["codex", "login", "--status"]`
- **Status Parser**: `exit_code` (0=authenticated, 1=not authenticated)
- **Remote OAuth**: `true` (supports device code flow)
- **OAuth Flows**: `["device_code"]`
- **Manual Fallback**: SSH into agent and run: `codex login --device-auth`

### Key Insights

1. **Status Check Diversity**: Tools use different mechanisms:
   - opencode: Output parsing (more complex, more info)
   - claude_code: Exit code only (simple, limited info)
   - codex: Exit code only (simple, limited info)

2. **Remote OAuth Capability**: 
   - opencode: Supports device code (can auth headless)
   - claude_code: NO remote auth (must SSH in)
   - codex: Supports device code (can auth headless)

3. **Fallback Strategy**:
   - For tools with RemoteOAuth=true: Try device code flow first, then SSH fallback
   - For tools with RemoteOAuth=false: SSH fallback is ONLY option

4. **Static vs Dynamic**:
   - All capabilities are static metadata (no runtime execution)
   - Enables supervisor to make auth strategy decisions without executing tools
   - Supports offline planning and policy decisions

### Design Rationale

- **Static Metadata**: Avoids runtime tool execution, enables offline decision-making
- **Struct Pattern**: Matches existing codebase patterns (envtypes.go)
- **No External Deps**: Pure Go, no external packages
- **Nil Safety**: Unknown tools return nil, not panic
- **Comprehensive Tests**: Covers all 3 tools + unknown tool case

### Integration Points

- Supervisor can use GetToolCapabilities() to determine auth strategy per tool
- Agent can use GetToolCapability() to validate auth requirements
- Policy engine can make decisions based on RemoteOAuth and OAuthFlows
- Manual intervention hints guide operators through SSH-based auth

---

## Task 3: Credential Distribution Config

### Completed
- Added `CredentialDistributionConfig` struct to `internal/config/supervisor.go`
- Added `CredentialDefaults` struct for env var mapping
- Added `Credentials` field to `SupervisorConfig`
- Implemented validation function `validateCredentialDistributionConfig()`
- Updated `supervisor.config.example.json` with credentials section
- Added 4 comprehensive tests covering valid and invalid configs

### Config Structure

#### CredentialDefaults (struct)
- `Env` (map[string]string) - Environment variables to distribute

#### CredentialDistributionConfig (struct)
- `Version` (int64) - Config version for compatibility
- `Defaults` (CredentialDefaults) - Default credentials for all agents
- `Agents` (map[string]CredentialDefaults) - Per-agent credential overrides

### Validation Rules Implemented

1. **Version Validation**
   - Must be >= 0 (non-negative)
   - Error: "credentials.version must be >= 0, got X"

2. **Empty Secret Rejection**
   - No empty string values allowed in Defaults.Env
   - Error: "credentials.defaults.env.KEY must not be empty"
   - No empty string values allowed in Agents[name].Env
   - Error: "credentials.agents.AGENT_NAME.env.KEY must not be empty"

3. **Optional Section**
   - Credentials section is optional (nil is valid)
   - Only validates if section is present

### Test Coverage

1. **TestSupervisorCredentialConfigValid**
   - Valid config with version=1, defaults, and per-agent overrides
   - Verifies all valid configs pass validation

2. **TestSupervisorCredentialConfigRejectsEmptySecret**
   - Empty value in defaults.env
   - Verifies error message includes field path

3. **TestSupervisorCredentialConfigRejectsEmptySecretInAgent**
   - Empty value in agents[agent-1].env
   - Verifies error message includes agent name and field path

4. **TestSupervisorCredentialConfigRejectsNegativeVersion**
   - Negative version number
   - Verifies version constraint enforcement

### Key Design Decisions

1. **Field Path in Errors**: Error messages include full path (e.g., "credentials.agents.agent-1.env.KEY")
   - Matches existing validation pattern in supervisor.go
   - Enables precise debugging of config issues

2. **Optional Section**: Credentials config is optional
   - Allows gradual adoption without breaking existing configs
   - Validation only runs if section is present

3. **Per-Agent Overrides**: Agents map allows credential customization
   - Supports multi-tenant scenarios
   - Enables agent-specific API keys or secrets

4. **No Credential Reuse**: Separate from cost provider API keys
   - Cost providers have their own API key management
   - Credentials section is for agent environment distribution
   - Prevents accidental credential leakage between systems

### Integration Points

- SupervisorConfig now includes Credentials field
- Validation integrated into validateSupervisorConfig()
- Example config shows realistic usage patterns
- Ready for supervisor-side credential distribution logic

### Test Results
- All 4 new tests pass
- All existing tests still pass (no regressions)
- No LSP errors in supervisor.go or config_test.go
- Evidence files created: task-3-config-pass.txt and task-3-config-fail.txt

---

## Task 6: Supervisor Node Registry Auth-State Summary Fields

### Completed
- Extended `NodeEntry` struct with `AuthStates` and `AuthUpdatedAt` fields
- Created `NodeAuthState` type with Tool, Status, Reason, CheckedAt fields
- Implemented `UpdateAuthState(nodeID, states)` method on NodeRegistry
- Implemented `GetAuthState(nodeID)` method on NodeRegistry
- Added `TestRegistryAuthSummary` test for update/retrieval
- Added `TestRegistryNoSecretStore` test for security verification
- All 4 registry tests pass (including 2 new tests)

### NodeAuthState Type Definition

```go
type NodeAuthState struct {
    Tool      string    `json:"tool"`
    Status    string    `json:"status"`    // authenticated|unauthenticated|not_installed|error|manual_required
    Reason    string    `json:"reason,omitempty"`
    CheckedAt time.Time `json:"checked_at"`
}
```

### NodeEntry Extension

```go
type NodeEntry struct {
    // ... existing fields ...
    AuthStates    map[string]NodeAuthState `json:"auth_states,omitempty"`
    AuthUpdatedAt time.Time                `json:"auth_updated_at,omitempty"`
}
```

### Registry Methods

#### UpdateAuthState(nodeID string, states map[string]NodeAuthState) error
- Updates auth states for a node
- Sets AuthUpdatedAt to current UTC time
- Thread-safe with RWMutex
- Returns ErrNodeNotFound if node doesn't exist
- Loads from DB if not in memory

#### GetAuthState(nodeID string) map[string]NodeAuthState
- Retrieves auth states for a node
- Returns empty map if not found (never nil)
- Thread-safe with RWMutex
- Loads from DB if not in memory

### Security Verification

**TestRegistryNoSecretStore** uses reflection to verify:
- NodeAuthState has NO fields named: key, token, secret, password, apikey, api_key
- Forbidden field check is case-insensitive
- Prevents accidental secret storage in auth state

### Test Coverage

**TestRegistryAuthSummary**:
- Creates node with 3 auth states (github, anthropic, docker)
- Tests different status values: authenticated, unauthenticated, not_installed
- Verifies AuthUpdatedAt is set after update
- Verifies GetAuthState retrieves all states correctly
- Verifies NodeEntry.AuthStates is populated

**TestRegistryNoSecretStore**:
- Reflection-based guard against secret fields
- Checks both NodeAuthState and NodeEntry.AuthStates
- Ensures no forbidden field names can be added

### Backward Compatibility

✓ All existing registry tests pass (TestRegistryPersistReload, TestRegistryHeartbeatTimeoutMarksOffline)
✓ No existing NodeEntry fields modified
✓ No DB schema changes required (auth states in-memory only)
✓ Existing persistence (SaveNodeToDB/LoadNodesFromDB) unaffected
✓ No external dependencies added

### Design Decisions

1. **In-Memory Only**: Auth states not persisted to DB (can be added later if needed)
2. **Map-Based Storage**: map[string]NodeAuthState allows flexible tool identification
3. **No Secrets**: Status-only design prevents credential leakage
4. **Reflection Guards**: TestRegistryNoSecretStore prevents future secret field additions
5. **Thread-Safe**: RWMutex protects concurrent access to auth states

### Integration Points

- Agents can report auth status via UpdateAuthState
- Supervisor can query per-node auth status via GetAuthState
- Auth status queryable without exposing credentials
- Enables observability dashboard for tool authentication status
- Foundation for auth-state-based policy decisions

### Test Results
- All 4 registry tests pass (2 existing + 2 new)
- No LSP errors in registry.go or registry_test.go
- Evidence file created: task-6-registry-pass.txt

---

## Task 5: Harden SanitizeArgs — Recursive Redaction

### Completed
- Hardened `SanitizeArgs` in `internal/supervisor/security.go` to recursively redact secret-like keys in nested maps and arrays
- Added `sanitizeMap()` and `sanitizeValue()` helpers for recursive traversal
- Audit log path (`audit.go:47`) already calls `SanitizeArgs` — recursive fix applies automatically

### Implementation

`SanitizeArgs` now delegates to `sanitizeMap()` which:
1. Iterates map keys — if `IsSecretKey(k)`, replaces value with `[REDACTED]`
2. Otherwise calls `sanitizeValue(v)` which:
   - `map[string]interface{}` → recurse via `sanitizeMap()`
   - `[]interface{}` → iterate elements, recurse each via `sanitizeValue()`
   - Everything else → pass through unchanged

### Key Insight
- Keys matching secret patterns (e.g. `credentials` matches `credential`) redact the **entire subtree** — correct behavior since the key itself signals sensitive content

### Test Coverage
- `TestSanitizeArgsRecursive`: Nested maps at depth 2+, arrays with embedded maps, mixed secret/non-secret keys
- `TestAuditNoPlainSecret`: End-to-end audit path — writes nested secrets via LogCommand, queries DB, asserts no plaintext leaks

### Test Results
- All 26 existing security tests pass (zero regressions)
- 2 new tests pass (TestSanitizeArgsRecursive, TestAuditNoPlainSecret)
- No LSP errors in security.go or security_test.go

---

## Task 2: Agent Inbound Command Routing Scaffold

### Completed
- Added `CommandHandler` type: `func(ctx context.Context, envelope *shared.Envelope) error`
- Added `commandHandlers map[string]CommandHandler` with RWMutex to WSClient
- Added `RegisterCommandHandler(cmdType string, handler CommandHandler)` method
- Added `handleCommand(ctx, env)` routing method
- Modified `readLoop` to intercept `MessageTypeCommand` and route before messageHandler

### Routing Flow
1. readLoop reads envelope from WebSocket
2. If `env.Type == "command"`: extract `type` field from JSON payload → lookup handler → call
3. If unknown command type: log warning, continue (no panic, no connection close)
4. Non-command messages: fall through to existing messageHandler as before

### Test Coverage
- `TestWSClientRoutesCommandEnvelope`: Registers handler for "create_session", mock supervisor sends command envelope, verifies handler called with correct envelope
- `TestWSClientRejectsUnknownCommand`: Sends unknown command type, verifies no panic, connection alive, then sends valid command and verifies it routes correctly

### Key Design Decisions
1. **Command routing before messageHandler**: Commands are intercepted in readLoop before the generic messageHandler, using `continue` to avoid double-processing
2. **RWMutex for handler map**: Allows concurrent reads (readLoop) with writes (RegisterCommandHandler)
3. **Minimal payload parsing**: Only extracts `type` field from payload via anonymous struct
4. **No external dependencies**: Pure Go implementation
5. **Thread-safe**: commandMu protects commandHandlers map

### Test Results
- All 8 WSClient tests pass (6 existing + 2 new)
- No existing behavior broken (heartbeat, events, reconnect, snapshot)
- No LSP errors in wsclient.go or wsclient_test.go
