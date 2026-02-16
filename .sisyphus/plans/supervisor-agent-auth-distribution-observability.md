# Supervisor-Agent Credential Distribution and Auth Observability Plan

## TL;DR

> **Quick Summary**: Extend HAL-O-SWARM to distribute API-key credentials from supervisor to agents at agent scope, and report per-tool auth state using CLI status commands as the canonical signal.
>
> **Deliverables**:
> - Credential push control path (supervisor config -> command -> agent apply)
> - Auth state collector using CLI commands (`opencode auth list`, `claude auth status`, `codex login --status`)
> - OAuth orchestration policy: remote flow only for supported tools; otherwise manual SSH login guidance
> - Auth status API/halctl surface with secure audit/redaction
>
> **Estimated Effort**: Large
> **Parallel Execution**: YES - 4 waves + FINAL review wave
> **Critical Path**: T1 -> T2 -> T7 -> T12 -> T17 -> F1-F4

---

## Context

### Original Request
- User requested implementation planning for credential/auth support with two constraints:
  - Agent-level shared auth per server (one login reused by multiple projects)
  - Auth observability unified by CLI status command execution
  - OAuth remote acquisition only when technically supported; otherwise direct SSH/manual login only

### Interview Summary
**Key Discussions**:
- Correct opencode target is `anomalyco/opencode` (supports `auth list/login/logout`).
- Claude Code supports `claude auth status/login/logout` and `apiKeyHelper` path.
- Codex supports `codex login --status` and device auth for headless OAuth.

**Research Findings**:
- Current HAL-O-SWARM has no supervisor->agent secret/config distribution channel.
- Existing agent message handling path currently processes heartbeat only; command execution path must be completed before credential push.
- Existing envcheck/provision code exists and should be extended, not replaced.

### Metis Review
**Identified Gaps (addressed in this plan)**:
- Missing prerequisite wiring in agent message handling for new supervisor commands.
- Missing TLS/secrets guardrails for credential transport and audit.
- Missing acceptance criteria for failure paths (tool missing, timeout, reconnect drift, concurrent push).
- Scope creep risks locked: no vault platform, no supervisor OAuth issuer role, no plaintext secret persistence.

---

## Work Objectives

### Core Objective
Implement secure, agent-scoped credential distribution plus reliable auth observability for opencode/Claude/Codex, while preserving existing architecture and avoiding plaintext secret leakage.

### Concrete Deliverables
- New credential distribution config schema in supervisor config.
- New protocol message(s) and command type(s) for credential push and auth-state reporting.
- Agent-side credential apply module (process-safe, redacted logging, no DB plaintext).
- Agent-side auth status collector using tool CLI commands with timeouts.
- Supervisor API and halctl commands to view per-node auth status and drift.
- Operator guidance for manual OAuth where remote flow is unsupported.

### Definition of Done
- [ ] One credential push config can target a node and be applied without restarting supervisor/agent processes beyond defined behavior.
- [ ] Auth status for all three tools is queryable from supervisor API and halctl.
- [ ] CLI-command-based checks return deterministic states (`authenticated|unauthenticated|not_installed|error|manual_required`).
- [ ] No plaintext credentials appear in logs, audit payloads, DB rows, or API responses.
- [ ] Unsupported remote OAuth cases produce explicit manual SSH guidance event.

### Must Have
- Agent-level credential scope (shared for all projects on the same server).
- Canonical auth observability by CLI status command execution.
- Remote OAuth execution only where supported by tool flow; explicit manual path otherwise.
- Timeout and retry safety for all external CLI checks.
- Idempotent credential push behavior by command id.

### Must NOT Have (Guardrails)
- No supervisor behavior as OAuth provider/issuer.
- No plaintext credential persistence in SQLite/audit/log files.
- No protocol version bump for this scope (additive messages under current envelope).
- No hidden fallback to file sniffing as primary status signal (CLI commands are primary).
- No auto-remediation policy loops for auth refresh in this scope.

---

## Verification Strategy (MANDATORY)

> **ZERO HUMAN INTERVENTION** — verification is agent-executed only.

### Test Decision
- **Infrastructure exists**: YES (Go tests + integration suite)
- **Automated tests**: YES (tests-after with targeted RED/GREEN for protocol and status parser)
- **Framework**: Go `testing`, integration tests, Bash CLI checks
- **Agent-Executed QA**: Mandatory for every task

### QA Policy
- Each task includes one happy path and one failure/edge scenario.
- Evidence stored under `.sisyphus/evidence/task-{N}-{scenario}.{ext}`.
- CLI checks executed with explicit timeout and exit-code assertions.

| Deliverable Type | Verification Tool | Method |
|---|---|---|
| Protocol/config changes | Bash | `go test` targeted packages |
| Agent command handling | Bash | integration tests with mock supervisor |
| CLI auth observability | Bash | run tool status commands in fixtures/mocks |
| API surface | Bash (`curl`) | assert status code + schema fields |
| Security redaction | Bash + SQL query | assert `[REDACTED]` and no secret leakage |

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Foundation + prerequisites, 6 parallel)
├── T1: Add auth domain types and status enums [quick]
├── T2: Complete agent inbound command routing scaffold [unspecified-high]
├── T3: Supervisor credential config schema + validation [quick]
├── T4: Tool capability matrix constants (remote-oauth/manual) [quick]
├── T5: Secure redaction hardening for nested payloads [unspecified-high]
└── T6: Auth-state storage model in registry (non-secret) [quick]

Wave 2 (Credential distribution plane, 6 parallel)
├── T7: Add credential push command type + payload contracts [deep]
├── T8: Supervisor command issuance endpoint for credential push [unspecified-high]
├── T9: Agent credential apply module (agent-scope env set path) [deep]
├── T10: Idempotency + replay safety for credential push [deep]
├── T11: Audit trail + secret-safe command recording [unspecified-high]
└── T12: Reconnect reconciliation (reapply or drift mark policy) [deep]

Wave 3 (Auth observability plane, 5 parallel)
├── T13: CLI runner with timeout/cancel semantics [quick]
├── T14: opencode auth parser (`opencode auth list`) [unspecified-high]
├── T15: Claude/Codex status adapters (exit-code model) [unspecified-high]
├── T16: Agent periodic auth-state reporter message [deep]
└── T17: Supervisor auth-state ingest + node status projection [deep]

Wave 4 (Operator surfaces + policy constraints, 4 parallel)
├── T18: HTTP API endpoints for auth status/drift [unspecified-high]
├── T19: halctl auth commands (`halctl auth status`) [quick]
├── T20: Remote OAuth orchestration paths for supported tools [deep]
└── T21: Manual-required workflow events/docs for unsupported OAuth [writing]

Wave FINAL (After all tasks, 4 parallel)
├── F1: Plan compliance audit (oracle)
├── F2: Code quality review (unspecified-high)
├── F3: Full QA scenario execution (unspecified-high)
└── F4: Scope fidelity check (deep)

Critical Path: T1 -> T2 -> T7 -> T12 -> T17 -> T18 -> F1-F4
Parallel Speedup: ~62% vs sequential
Max Concurrent: 6
```

### Dependency Matrix (FULL)

| Task | Depends On | Blocks | Wave |
|---|---|---|---|
| T1 | — | T7,T16 | 1 |
| T2 | — | T7,T9,T16 | 1 |
| T3 | — | T8,T20 | 1 |
| T4 | — | T20,T21 | 1 |
| T5 | — | T11,T18 | 1 |
| T6 | — | T17,T18 | 1 |
| T7 | T1,T2 | T8,T9,T10,T11,T12 | 2 |
| T8 | T3,T7 | T20 | 2 |
| T9 | T2,T7 | T10,T12 | 2 |
| T10 | T7,T9 | T12,T20 | 2 |
| T11 | T5,T7 | T18,F1 | 2 |
| T12 | T7,T9,T10 | T16,T17,T20 | 2 |
| T13 | — | T14,T15,T16 | 3 |
| T14 | T13 | T16,F3 | 3 |
| T15 | T13 | T16,F3 | 3 |
| T16 | T1,T2,T12,T14,T15 | T17,T18,T19,F3 | 3 |
| T17 | T6,T12,T16 | T18,T19,F3 | 3 |
| T18 | T5,T6,T11,T16,T17 | T19,F1,F3 | 4 |
| T19 | T16,T17,T18 | F3 | 4 |
| T20 | T3,T4,T8,T10,T12 | T21,F3 | 4 |
| T21 | T4,T20 | F1,F3 | 4 |
| F1 | T1-T21 | — | FINAL |
| F2 | T1-T21 | — | FINAL |
| F3 | T1-T21 | — | FINAL |
| F4 | T1-T21 | — | FINAL |

### Agent Dispatch Summary

| Wave | # Parallel | Tasks -> Agent Category |
|---|---:|---|
| 1 | **6** | T1,T3,T4,T6 -> `quick`; T2,T5 -> `unspecified-high` |
| 2 | **6** | T7,T9,T10,T12 -> `deep`; T8,T11 -> `unspecified-high` |
| 3 | **5** | T13 -> `quick`; T14,T15 -> `unspecified-high`; T16,T17 -> `deep` |
| 4 | **4** | T18 -> `unspecified-high`; T19 -> `quick`; T20 -> `deep`; T21 -> `writing` |
| FINAL | **4** | F1 -> `oracle`; F2,F3 -> `unspecified-high`; F4 -> `deep` |

---

## TODOs

> Implementation + verification are one task. Every task includes two executable QA scenarios.

- [ ] 1. Auth domain types and shared enums

  **What to do**:
  - Add shared auth state types (`tool`, `status`, `reason`, `checked_at`) and credential push payload DTOs.
  - Define canonical status enum: `authenticated|unauthenticated|not_installed|error|manual_required`.

  **Must NOT do**:
  - Include raw secret values in shared status structs.

  **Recommended Agent Profile**:
  - **Category**: `quick` (small, schema-focused change)
  - **Skills**: `git-master`

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 1)
  - **Blocks**: T7, T16
  - **Blocked By**: None

  **References**:
  - `internal/shared/types.go` - Existing protocol type definitions to extend safely.
  - `internal/shared/protocol.go` - Envelope contract to keep unchanged.
  - `internal/shared/envtypes.go` - Existing status enum style to mirror.

  **Acceptance Criteria**:
  - [ ] `go test ./internal/shared -run Auth` passes.

  **QA Scenarios**:
  ```text
  Scenario: Shared auth status serialization
    Tool: Bash
    Steps:
      1. Run `go test ./internal/shared -run TestAuthStatusJSONRoundTrip -v`
      2. Assert PASS and enum values preserved.
    Expected Result: 1 test PASS, status enum round-trip stable.
    Evidence: .sisyphus/evidence/task-1-auth-types-pass.txt

  Scenario: Reject secret field in status payload
    Tool: Bash
    Steps:
      1. Run `go test ./internal/shared -run TestAuthStatusNoSecretField -v`
      2. Assert build/test fails if secret field is accidentally added.
    Expected Result: Guard test PASS (no secret fields in status struct).
    Evidence: .sisyphus/evidence/task-1-auth-types-failguard.txt
  ```

- [ ] 2. Agent inbound command routing scaffold completion

  **What to do**:
  - Complete agent message dispatch path so supervisor commands beyond heartbeat can be handled.
  - Add routing hooks for credential push and future auth actions.

  **Must NOT do**:
  - Execute unrecognized command payloads without validation.

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: `git-master`

  **Parallelization**:
  - **Can Run In Parallel**: YES (Wave 1)
  - **Blocks**: T7, T9, T16
  - **Blocked By**: None

  **References**:
  - `internal/supervisor/agent_conn.go` - Current inbound handling limitation.
  - `internal/agent/wsclient.go` - Agent WS loop and message processing entrypoint.

  **Acceptance Criteria**:
  - [ ] Command envelopes are routed to registered handlers in integration tests.

  **QA Scenarios**:
  ```text
  Scenario: Command envelope reaches credential handler
    Tool: Bash
    Steps:
      1. Run `go test ./internal/agent -run TestWSClientRoutesCommandEnvelope -v`
      2. Assert handler invocation count is 1.
    Expected Result: PASS with deterministic route.
    Evidence: .sisyphus/evidence/task-2-route-pass.txt

  Scenario: Unknown command rejected safely
    Tool: Bash
    Steps:
      1. Run `go test ./internal/agent -run TestWSClientRejectsUnknownCommand -v`
      2. Assert no panic and structured error event emitted.
    Expected Result: PASS, connection remains alive.
    Evidence: .sisyphus/evidence/task-2-route-fail.txt
  ```

- [ ] 3. Supervisor credential config schema + validation

  **What to do**:
  - Add credential distribution config section in supervisor config.
  - Validate required fields and disallow empty secret values.

  **Must NOT do**:
  - Reuse existing cost provider keys implicitly as distribution source.

  **References**:
  - `internal/config/supervisor.go` - Extend config struct and validation.
  - `supervisor.config.example.json` - Add documented example.

  **Acceptance Criteria**:
  - [ ] Valid config loads; invalid credential blocks startup.

  **QA Scenarios**:
  ```text
  Scenario: Valid credential config loads
    Tool: Bash
    Steps:
      1. Run `go test ./internal/config -run TestSupervisorCredentialConfigValid -v`
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-3-config-pass.txt

  Scenario: Empty secret value rejected
    Tool: Bash
    Steps:
      1. Run `go test ./internal/config -run TestSupervisorCredentialConfigRejectsEmptySecret -v`
    Expected Result: PASS with explicit validation error path.
    Evidence: .sisyphus/evidence/task-3-config-fail.txt
  ```

- [ ] 4. Tool capability matrix constants

  **What to do**:
  - Encode tool capability metadata: status command, remote OAuth support, manual-only fallback.
  - Set remote OAuth policy: codex/opencode supported where flow exists, Claude manual-only.

  **Must NOT do**:
  - Infer capabilities dynamically from untrusted output at runtime.

  **References**:
  - `internal/agent/` (new capability map module)
  - `docs/DEVELOPMENT.md` (document policy source-of-truth)

  **Acceptance Criteria**:
  - [ ] Capability table used by both reporter and orchestration layers.

  **QA Scenarios**:
  ```text
  Scenario: Capability lookup returns expected policy
    Tool: Bash
    Steps:
      1. Run `go test ./internal/agent -run TestToolCapabilityMatrix -v`
    Expected Result: PASS for opencode/claude/codex entries.
    Evidence: .sisyphus/evidence/task-4-capability-pass.txt

  Scenario: Unknown tool defaults to manual_required
    Tool: Bash
    Steps:
      1. Run `go test ./internal/agent -run TestToolCapabilityUnknownDefaultsManual -v`
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-4-capability-fail.txt
  ```

- [ ] 5. Nested payload redaction hardening

  **What to do**:
  - Strengthen sanitization to recursively redact secret-like keys in nested maps/arrays.
  - Ensure audit serialization path uses the hardened sanitizer.

  **Must NOT do**:
  - Log raw credential values at debug/info/error levels.

  **References**:
  - `internal/supervisor/security.go` - Existing secret-key matcher and sanitizer.
  - `internal/supervisor/audit.go` - Audit event persistence path.

  **Acceptance Criteria**:
  - [ ] Any key matching secret patterns appears as `[REDACTED]` recursively.

  **QA Scenarios**:
  ```text
  Scenario: Recursive redaction in nested command args
    Tool: Bash
    Steps:
      1. Run `go test ./internal/supervisor -run TestSanitizeArgsRecursive -v`
    Expected Result: PASS, all nested secret keys redacted.
    Evidence: .sisyphus/evidence/task-5-redaction-pass.txt

  Scenario: Audit log never stores plaintext secret
    Tool: Bash
    Steps:
      1. Run `go test ./internal/supervisor -run TestAuditNoPlainSecret -v`
    Expected Result: PASS with `[REDACTED]` assertion.
    Evidence: .sisyphus/evidence/task-5-redaction-fail.txt
  ```

- [ ] 6. Supervisor registry auth-state projection model

  **What to do**:
  - Extend node registry model with auth-state summary fields (non-secret).
  - Add freshness timestamp and per-tool status map.

  **Must NOT do**:
  - Persist secret values in registry snapshots.

  **References**:
  - `internal/supervisor/registry.go` - Node state model.
  - `internal/shared/types.go` - Shared state payload schema.

  **Acceptance Criteria**:
  - [ ] Node status retrieval includes auth summary without secrets.

  **QA Scenarios**:
  ```text
  Scenario: Registry stores auth summary
    Tool: Bash
    Steps:
      1. Run `go test ./internal/supervisor -run TestRegistryAuthSummary -v`
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-6-registry-pass.txt

  Scenario: Registry rejects secret payload fields
    Tool: Bash
    Steps:
      1. Run `go test ./internal/supervisor -run TestRegistryNoSecretStore -v`
    Expected Result: PASS.
    Evidence: .sisyphus/evidence/task-6-registry-fail.txt
  ```

- [x] 7. Credential push command contract
  - **What to do**: add command type + payload schema for agent-scoped credentials and version.
  - **References**: `internal/supervisor/command_types.go`, `internal/shared/types.go`, `internal/shared/protocol.go`.
  - **Acceptance Criteria**: invalid payload rejected; valid payload round-trip works.
  - **QA (happy)**: `go test ./internal/shared -run TestCredentialPushEnvelopeRoundTrip -v` -> PASS -> `.sisyphus/evidence/task-7-contract-pass.txt`
  - **QA (error)**: `go test ./internal/shared -run TestCredentialPushRejectsInvalidPayload -v` -> PASS -> `.sisyphus/evidence/task-7-contract-fail.txt`

- [x] 8. Credential push command issuance endpoint
  - **What to do**: add supervisor API route to dispatch credential push to a target node.
  - **References**: `internal/supervisor/http_api.go`, `internal/supervisor/command_dispatcher.go`.
  - **Acceptance Criteria**: authorized request dispatches; unauthorized request returns 401.
  - **QA (happy)**: `curl -X POST /api/v1/commands/credentials/push` returns `queued` -> `.sisyphus/evidence/task-8-api-pass.json`
  - **QA (error)**: missing bearer token returns 401 -> `.sisyphus/evidence/task-8-api-fail.json`

- [x] 9. Agent credential apply module
  - **What to do**: apply pushed env credentials to agent runtime scope and mask values in logs.
  - **References**: `internal/agent/wsclient.go`, `internal/agent/provision.go`, `internal/agent/agent.go`.
  - **Acceptance Criteria**: pushed credentials become available to tool subprocess context.
  - **QA (happy)**: `go test ./internal/agent -run TestCredentialApplySetsRuntimeEnv -v` -> `.sisyphus/evidence/task-9-apply-pass.txt`
  - **QA (error)**: malformed payload ignored with error event (no panic) -> `.sisyphus/evidence/task-9-apply-fail.txt`

- [x] 10. Credential push idempotency and replay safety
  - **What to do**: enforce idempotency by `command_id`; duplicate push must not re-apply side effects.
  - **References**: `internal/supervisor/command_dispatcher.go`, `internal/storage/` idempotency tables.
  - **Acceptance Criteria**: duplicate requests return original outcome.
  - **QA (happy)**: duplicate same id returns cached success -> `.sisyphus/evidence/task-10-idem-pass.txt`
  - **QA (error)**: same id with different payload rejected deterministically -> `.sisyphus/evidence/task-10-idem-fail.txt`

- [x] 11. Secure audit integration for push commands
  - **What to do**: ensure push command audits actor/action/result with fully redacted args.
  - **References**: `internal/supervisor/audit.go`, `internal/supervisor/security.go`, `internal/storage/migrations/`.
  - **Acceptance Criteria**: audit row exists with no plaintext secrets.
  - **QA (happy)**: audit contains push event metadata -> `.sisyphus/evidence/task-11-audit-pass.txt`
  - **QA (error)**: secret leakage assertion test fails if plaintext present -> `.sisyphus/evidence/task-11-audit-fail.txt`

- [x] 12. Reconnect credential reconciliation policy
  - **What to do**: on reconnect, compare expected credential version and mark drift/reapply policy.
  - **References**: `internal/agent/wsclient.go`, `internal/supervisor/hub.go`, `internal/supervisor/registry.go`.
  - **Acceptance Criteria**: reconnect after disconnect converges to expected credential version.
  - **QA (happy)**: forced disconnect/reconnect ends in `in_sync` -> `.sisyphus/evidence/task-12-reconcile-pass.txt`
  - **QA (error)**: version mismatch yields `drift_detected` event -> `.sisyphus/evidence/task-12-reconcile-fail.txt`

- [x] 13. CLI runner with timeout/cancellation
  - **What to do**: add reusable command runner for auth checks with timeout and safe stderr capture.
  - **References**: `internal/agent/envcheck.go` (`CommandRunner` pattern).
  - **Acceptance Criteria**: timed-out tool command yields `error` status, no goroutine leak.
  - **QA (happy)**: short command finishes within timeout -> `.sisyphus/evidence/task-13-runner-pass.txt`
  - **QA (error)**: hanging command canceled at timeout -> `.sisyphus/evidence/task-13-runner-fail.txt`

- [x] 14. opencode auth list parser adapter
  - **What to do**: execute `opencode auth list` and map output to canonical auth state.
  - **References**: new adapter in `internal/agent/` + fixtures.
  - **Acceptance Criteria**: parser handles stored-credential and env-var sections.
  - **QA (happy)**: fixture output maps to `authenticated` -> `.sisyphus/evidence/task-14-opencode-pass.txt`
  - **QA (error)**: command-not-found maps to `not_installed` -> `.sisyphus/evidence/task-14-opencode-fail.txt`

- [x] 15. Claude/Codex status adapters
  - **What to do**: execute `claude auth status` and `codex login --status`, map exit code/status text.
  - **References**: new adapters in `internal/agent/`.
  - **Acceptance Criteria**: exit-code mapping deterministic for both tools.
  - **QA (happy)**: mocked exit 0 maps `authenticated` -> `.sisyphus/evidence/task-15-adapter-pass.txt`
  - **QA (error)**: non-zero and timeout map `unauthenticated|error` -> `.sisyphus/evidence/task-15-adapter-fail.txt`

- [x] 16. Agent periodic auth-state reporter
  - **What to do**: schedule auth checks, emit auth-state message on interval and on change.
  - **References**: `internal/agent/wsclient.go`, `internal/shared/types.go`.
  - **Acceptance Criteria**: initial report on connect + periodic report every configured interval.
  - **QA (happy)**: integration test receives periodic auth-state events -> `.sisyphus/evidence/task-16-report-pass.txt`
  - **QA (error)**: reporter survives one adapter failure and continues -> `.sisyphus/evidence/task-16-report-fail.txt`

- [x] 17. Supervisor auth-state ingest and projection
  - **What to do**: ingest auth-state messages and project per-node status to registry/API model.
  - **References**: `internal/supervisor/hub.go`, `internal/supervisor/registry.go`, `internal/supervisor/http_api.go`.
  - **Acceptance Criteria**: latest auth state visible for node without secret fields.
  - **QA (happy)**: posted auth-state updates node projection -> `.sisyphus/evidence/task-17-ingest-pass.txt`
  - **QA (error)**: malformed auth-state ignored with warning -> `.sisyphus/evidence/task-17-ingest-fail.txt`

- [ ] 18. HTTP auth status/drift endpoints
  - **What to do**: add `/api/v1/nodes/{id}/auth` and drift list endpoint.
  - **References**: `internal/supervisor/http_api.go`.
  - **Acceptance Criteria**: endpoints return stable JSON schema and proper auth enforcement.
  - **QA (happy)**: authorized GET returns auth status map -> `.sisyphus/evidence/task-18-http-pass.json`
  - **QA (error)**: unknown node returns 404 with structured error -> `.sisyphus/evidence/task-18-http-fail.json`

- [ ] 19. halctl auth status commands
  - **What to do**: add `halctl auth status <node-id>` and optional list/drift command.
  - **References**: `internal/halctl/client.go`, `cmd/halctl/main.go`.
  - **Acceptance Criteria**: command prints parseable status table/json.
  - **QA (happy)**: `halctl auth status node-1` returns expected fields -> `.sisyphus/evidence/task-19-halctl-pass.txt`
  - **QA (error)**: invalid node id returns non-zero with clear message -> `.sisyphus/evidence/task-19-halctl-fail.txt`

- [ ] 20. Remote OAuth orchestration for supported tools
  - **What to do**: implement remote trigger workflow only for supported flows (Codex device auth, opencode provider flows that support device/headless).
  - **References**: new orchestration handlers in `internal/supervisor/` and `internal/agent/`.
  - **Acceptance Criteria**: workflow emits URL+code challenge event and terminal success/failure state.
  - **QA (happy)**: mocked device flow emits challenge and completion -> `.sisyphus/evidence/task-20-oauth-pass.txt`
  - **QA (error)**: unsupported provider returns `manual_required` immediately -> `.sisyphus/evidence/task-20-oauth-fail.txt`

- [ ] 21. Manual-required workflow for unsupported OAuth
  - **What to do**: define and emit standardized manual guidance event (SSH login steps) for unsupported remote OAuth cases.
  - **References**: `internal/supervisor/` notification/event path, `docs/RUNBOOK.md`.
  - **Acceptance Criteria**: operators receive explicit steps and reason code.
  - **QA (happy)**: unsupported Claude remote OAuth generates guidance event -> `.sisyphus/evidence/task-21-manual-pass.txt`
  - **QA (error)**: missing node context generates safe fallback guidance -> `.sisyphus/evidence/task-21-manual-fail.txt`

---

## Final Verification Wave (MANDATORY)

- [ ] F1. **Plan Compliance Audit** — `oracle`
  - Validate Must Have and Must NOT Have against code + evidence.
  - Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT`

- [ ] F2. **Code Quality Review** — `unspecified-high`
  - Run build/tests/lint and scan for secret leakage patterns and unsafe handling.
  - Output: `Build [PASS/FAIL] | Tests [N/N] | Secret Leak Scan [PASS/FAIL] | VERDICT`

- [ ] F3. **Real QA Scenario Execution** — `unspecified-high`
  - Execute all task scenarios and verify evidence files exist.
  - Output: `Scenarios [N/N] | Integration [N/N] | Edge Cases [N] | VERDICT`

- [ ] F4. **Scope Fidelity Check** — `deep`
  - Ensure implementation is limited to credential distribution + auth observability scope.
  - Output: `Tasks [N/N compliant] | Unaccounted [CLEAN/N] | VERDICT`

---

## Commit Strategy

| After Task Group | Message Pattern | Verification |
|---|---|---|
| Wave 1 | `feat(auth-foundation): add auth schemas routing and guardrails` | `go test ./internal/...` |
| Wave 2 | `feat(auth-push): implement credential distribution plane` | integration push tests |
| Wave 3 | `feat(auth-observe): add cli-based auth state reporting` | tool adapter tests |
| Wave 4 | `feat(auth-surface): add api halctl and oauth workflows` | API + CLI smoke tests |

---

## Success Criteria

### Verification Commands

```bash
go test ./...
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8421/api/v1/nodes/<node-id>/auth
halctl auth status <node-id>
```

### Final Checklist
- [ ] Agent-level shared auth behavior verified across multi-project node.
- [ ] CLI-command-based auth observability operational for opencode/Claude/Codex.
- [ ] Remote OAuth limited to supported flows; unsupported paths emit manual-required guidance.
- [ ] No plaintext secrets in logs/audit/db/api.
- [ ] FINAL wave (F1-F4) all APPROVE.
