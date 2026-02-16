# HAL-O-SWARM v1.0-v1.1 Implementation Plan

## TL;DR

> **Quick Summary**: Build a production-ready distributed supervisor-agent control plane in Go, starting with reliable core orchestration (WebSocket hub, session tracking, command dispatch) and then adding environment lifecycle management, cost aggregation, and operational hardening.
>
> **Deliverables**:
> - `hal-supervisor` daemon with node/session/event/cost/command/env APIs
> - `hal-agent` node process wrapping opencode SDK + env check/provision
> - `halctl` CLI (remote mode first) for operations without chat platforms
> - Discord command integration (Slack deferred)
> - Verification artifacts under `.sisyphus/evidence/`
>
> **Estimated Effort**: XL
> **Parallel Execution**: YES - 5 waves + FINAL review wave
> **Critical Path**: T2 -> T6 -> T8 -> T10 -> T11 -> T12 -> T23

---

## Context

### Original Request
- User requested an implementation plan from `Hal-o-swarm_Product_Spec_v1.1.md`.

### Interview Summary
**Key Discussions**:
- Plan-only output (no implementation) in `.sisyphus/plans/`.
- Scope should cover full product spec v1.1 and be execution-ready.

**Research Findings**:
- Spec defines architecture, module boundaries, command/API surfaces, state machines, env lifecycle, and deployment model in `Hal-o-swarm_Product_Spec_v1.1.md:35` and `Hal-o-swarm_Product_Spec_v1.1.md:735`.
- opencode integration details require adapter mapping: SDK has session/event APIs but not direct one-to-one `AgentAPI` methods from spec.
- Production guardrails needed for auth-at-handshake, jittered reconnect, heartbeat deadlines, idempotency keys, event sequencing/dedup, and offline recovery.

### Metis Review
**Identified Gaps (addressed in this plan)**:
- SDK/spec contract mismatch -> explicit adapter task + validation acceptance criteria.
- Scope creep risk in env automation and Slack parity -> locked as phased constraints.
- Missing resilience/security details -> explicit hardening tasks and guardrails.
- Missing concrete module-level verification -> every task includes executable QA scenarios.

---

## Work Objectives

### Core Objective
Deliver a dependable multi-node supervisor-agent system that can observe, control, and cost-track LLM coding sessions across servers, with safe remote intervention and standardized environment checks.

### Concrete Deliverables
- `cmd/supervisor/main.go`, `internal/supervisor/*` core modules.
- `cmd/agent/main.go`, `internal/agent/*` execution + env modules.
- `cmd/halctl/main.go`, `internal/halctl/*` remote operator CLI.
- Shared protocol/types in `internal/shared/*`.
- Config + manifest templates: `supervisor.config.json`, `agent.config.json`, `env-manifest.json`.

### Definition of Done
- [ ] 2+ agents connect concurrently and remain healthy for 30+ minutes.
- [ ] `/status`, `/resume`, `/inject`, `/restart`, `/kill`, `/start`, `/cost`, `/nodes` function end-to-end via Discord.
- [ ] Environment check/provision endpoints and halctl commands return deterministic structured output.
- [ ] Cost report (today/week/month) returns non-empty aggregated payload from at least Anthropic + OpenAI sources.
- [ ] Failure scenarios (node offline, SSE drop, command timeout, heartbeat miss) are detected and surfaced.

### Must Have
- Protocol-versioned WebSocket envelope for all agent-supervisor messages.
- Agent reconnection with jittered exponential backoff and full-state resync.
- Command dispatch idempotency and result correlation by `command_id`.
- Event ordering + dedupe protections.
- Testable adapter around `opencode-sdk-go` API.

### Must NOT Have (Guardrails)
- No Slack implementation in v1.0 execution scope.
- No Discord button interactions in v1.0 (slash command + embed only).
- No dependency auto-trigger on milestone parsing in v1.0 (manual trigger only).
- No unsafe auto-provisioning for package installs/SDK downloads without explicit approval flow.
- No direct SDK calls spread across codebase (must go through adapter).

---

## Verification Strategy (MANDATORY)

> **ZERO HUMAN INTERVENTION** — verification is agent-executed only.

### Test Decision
- **Infrastructure exists**: NO (greenfield repository)
- **Automated tests**: YES (TDD for core modules; tests-after allowed for wiring layers)
- **Framework**: Go `testing` + integration tests (`go test ./...`) + HTTP/WebSocket smoke checks via `curl` and CLI
- **If TDD**: RED -> GREEN -> REFACTOR for protocol, state, command, and env check modules

### QA Policy
- Each task includes at least one happy path and one failure/edge scenario.
- Evidence stored in `.sisyphus/evidence/task-{N}-{scenario}.{ext}`.
- For Web/API checks use Bash (`go test`, `curl`), for CLI checks use `interactive_bash` where needed, for Discord integration use mock adapter tests + webhook smoke script.

| Deliverable Type | Verification Tool | Method |
|---|---|---|
| Core modules | Bash | `go test` with explicit package targeting |
| WebSocket flows | Bash | integration test with simulated agent connections |
| HTTP API | Bash (`curl`) | assert status and required JSON fields |
| CLI | Bash / interactive_bash | run `halctl` commands and assert output schema |
| Env checks/provision | Bash | fixture directories + deterministic check outputs |

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Start immediately — foundations, 5 parallel)
├── T1: Repo scaffolding + config schema defaults [quick]
├── T2: Shared protocol/types v1 envelope [quick]
├── T3: SQLite schema + migration bootstrap [quick]
├── T4: Supervisor runtime bootstrap (lifecycle/logging) [quick]
└── T5: Agent runtime bootstrap (config/process skeleton) [quick]

Wave 2 (After Wave 1 — comms and state, 5 parallel)
├── T6: Supervisor WebSocket hub + auth + heartbeat [unspecified-high]
├── T7: Agent WS client reconnect/backoff/full-resync [unspecified-high]
├── T8: Node registry + session tracker persistence [deep]
├── T9: Event ingest ordering/dedupe pipeline [deep]
└── T10: opencode SDK adapter + SSE demux + mocks [deep]

Wave 3 (After Wave 2 — control plane, 5 parallel)
├── T11: Command dispatcher idempotency + result contracts [deep]
├── T12: Discord slash command integration [unspecified-high]
├── T13: Supervisor HTTP API (sessions/nodes/events) [unspecified-high]
├── T14: Auto-intervention engine (resume/restart/cost) [deep]
└── T15: Dependency graph validator + manual trigger API [quick]

Wave 4 (After Wave 3 — env and cost surface, 5 parallel)
├── T16: Env manifest parser + schema validator [quick]
├── T17: Env checker (runtime/tool/env/git/docs/context) [unspecified-high]
├── T18: Safe auto-provisioner + approval-required pipeline [unspecified-high]
├── T19: Cost aggregator Anthropic/OpenAI + estimator [deep]
└── T20: halctl remote-mode command suite [quick]

Wave 5 (After Wave 4 — hardening and release, 4 parallel)
├── T21: Security hardening (TLS/origin/token/audit) [unspecified-high]
├── T22: Observability (metrics/health/logging) [quick]
├── T23: End-to-end + chaos integration tests [deep]
└── T24: Packaging/systemd/install/runbook [writing]

Wave FINAL (After all tasks — independent review, 4 parallel)
├── F1: Plan compliance audit (oracle)
├── F2: Code quality review (unspecified-high)
├── F3: Real QA scenario execution (unspecified-high)
└── F4: Scope fidelity check (deep)

Critical Path: T2 -> T6 -> T8 -> T10 -> T11 -> T12 -> T23 -> F1-F4
Parallel Speedup: ~65% vs naive sequential
Max Concurrent: 5
```

### Dependency Matrix (FULL)

| Task | Depends On | Blocks | Wave |
|---|---|---|---|
| T1 | — | T4,T5,T16,T20 | 1 |
| T2 | — | T6,T7,T8,T9,T10,T11,T13 | 1 |
| T3 | — | T8,T19 | 1 |
| T4 | T1 | T6,T13,T22,T24 | 1 |
| T5 | T1 | T7,T10,T17,T18 | 1 |
| T6 | T2,T4 | T8,T9,T11,T12,T14 | 2 |
| T7 | T2,T5 | T9,T10,T14 | 2 |
| T8 | T2,T3,T6 | T11,T13,T14,T19 | 2 |
| T9 | T2,T6,T7 | T12,T14,T22 | 2 |
| T10 | T2,T5,T7 | T11,T14,T23 | 2 |
| T11 | T2,T6,T8,T10 | T12,T13,T14,T20,T23 | 3 |
| T12 | T6,T9,T11 | T23 | 3 |
| T13 | T2,T4,T8,T11 | T20,T23 | 3 |
| T14 | T6,T7,T8,T9,T10,T11 | T23 | 3 |
| T15 | T2,T8 | T20,T23 | 3 |
| T16 | T1 | T17,T18 | 4 |
| T17 | T5,T16 | T18,T20,T23 | 4 |
| T18 | T5,T16,T17 | T20,T23 | 4 |
| T19 | T3,T8 | T20,T23 | 4 |
| T20 | T1,T11,T13,T15,T17,T18,T19 | T23 | 4 |
| T21 | T6,T11,T13 | T23,T24 | 5 |
| T22 | T4,T9,T13 | T23,T24 | 5 |
| T23 | T10,T11,T12,T13,T14,T15,T17,T18,T19,T20,T21,T22 | F1-F4 | 5 |
| T24 | T4,T21,T22 | F1-F4 | 5 |
| F1 | T1-T24 | — | FINAL |
| F2 | T1-T24 | — | FINAL |
| F3 | T1-T24 | — | FINAL |
| F4 | T1-T24 | — | FINAL |

### Agent Dispatch Summary

| Wave | # Parallel | Tasks -> Agent Category |
|---|---:|---|
| 1 | **5** | T1,T2,T3,T4,T5 -> `quick` |
| 2 | **5** | T6,T7 -> `unspecified-high`; T8,T9,T10 -> `deep` |
| 3 | **5** | T11,T14 -> `deep`; T12,T13 -> `unspecified-high`; T15 -> `quick` |
| 4 | **5** | T16,T20 -> `quick`; T17,T18 -> `unspecified-high`; T19 -> `deep` |
| 5 | **4** | T21 -> `unspecified-high`; T22 -> `quick`; T23 -> `deep`; T24 -> `writing` |
| FINAL | **4** | F1 -> `oracle`; F2,F3 -> `unspecified-high`; F4 -> `deep` |

---

## TODOs

> Implementation + verification are one task. Each task includes two executable QA scenarios.

- [ ] 1. Repository scaffolding and base config
  - **What to do**: initialize module layout per `Hal-o-swarm_Product_Spec_v1.1.md:735`; add config examples and strict validation defaults.
  - **Must NOT do**: add Slack-specific runtime wiring.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master` (atomic setup commits).
  - **Parallelization**: YES, Wave 1, Blocks T4/T5/T16/T20, Blocked By none.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:735`, `Hal-o-swarm_Product_Spec_v1.1.md:676`, `Hal-o-swarm_Product_Spec_v1.1.md:718`.
  - **Acceptance Criteria**: `go list ./...` succeeds; config example files parse via validation tests.
  - **QA Scenario (happy)**: Tool Bash; `go test ./internal/config -run TestLoadExample`; expect PASS; evidence `.sisyphus/evidence/task-1-config-pass.txt`.
  - **QA Scenario (error)**: Tool Bash; load malformed config fixture; expect explicit validation error field path; evidence `.sisyphus/evidence/task-1-config-fail.txt`.

- [ ] 2. Shared protocol/types v1 envelope
  - **What to do**: define protocol envelope with `version,type,request_id,timestamp,payload`; implement marshal/unmarshal validation.
  - **Must NOT do**: unversioned message handlers.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 1, Blocks T6-T13, Blocked By none.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:78`, `Hal-o-swarm_Product_Spec_v1.1.md:772`.
  - **Acceptance Criteria**: unknown protocol version rejected; required fields validated.
  - **QA Scenario (happy)**: Bash `go test ./internal/shared -run TestEnvelopeRoundTrip`; PASS; evidence `.sisyphus/evidence/task-2-envelope-pass.txt`.
  - **QA Scenario (error)**: Bash invalid version payload test returns `ErrUnsupportedVersion`; evidence `.sisyphus/evidence/task-2-envelope-fail.txt`.

- [ ] 3. SQLite schema and migration bootstrap
  - **What to do**: create migration runner and initial schema for events, sessions, costs, command idempotency.
  - **Must NOT do**: production retention deletions without config gate.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 1, Blocks T8/T19, Blocked By none.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:205`, `Hal-o-swarm_Product_Spec_v1.1.md:312`.
  - **Acceptance Criteria**: fresh DB migrates to latest; rerun migrations is idempotent.
  - **QA Scenario (happy)**: Bash `go test ./internal/storage -run TestMigrateFresh`; PASS; evidence `.sisyphus/evidence/task-3-migrate-pass.txt`.
  - **QA Scenario (error)**: Bash simulate broken migration checksum; expect fail-fast; evidence `.sisyphus/evidence/task-3-migrate-fail.txt`.

- [ ] 4. Supervisor runtime bootstrap
  - **What to do**: initialize supervisor main lifecycle, graceful shutdown hooks, config + logger wiring.
  - **Must NOT do**: bind chat integrations before command contracts are ready.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 1, Blocks T6/T13/T22/T24, Blocked By T1.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:134`, `Hal-o-swarm_Product_Spec_v1.1.md:801`.
  - **Acceptance Criteria**: clean startup/shutdown exit code 0 and no goroutine leak in integration test.
  - **QA Scenario (happy)**: Bash run supervisor for 5s then SIGTERM; expect graceful stop log; evidence `.sisyphus/evidence/task-4-supervisor-pass.txt`.
  - **QA Scenario (error)**: Bash start with missing token; expect startup rejection; evidence `.sisyphus/evidence/task-4-supervisor-fail.txt`.

- [ ] 5. Agent runtime bootstrap
  - **What to do**: initialize agent config loading, project registry, local opencode process supervisor skeleton.
  - **Must NOT do**: assume one opencode serve handles all projects.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 1, Blocks T7/T10/T17/T18, Blocked By T1.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:92`, `Hal-o-swarm_Product_Spec_v1.1.md:126`, `Hal-o-swarm_Product_Spec_v1.1.md:720`.
  - **Acceptance Criteria**: project definitions load and invalid paths fail deterministically.
  - **QA Scenario (happy)**: Bash `go test ./internal/agent -run TestLoadProjects`; PASS; evidence `.sisyphus/evidence/task-5-agent-pass.txt`.
  - **QA Scenario (error)**: Bash fixture with nonexistent project directory fails with clear message; evidence `.sisyphus/evidence/task-5-agent-fail.txt`.

- [ ] 6. Supervisor WebSocket hub with handshake auth and heartbeat
  - **What to do**: implement authenticated upgrade, origin checks, ping/pong deadlines, heartbeat timeout marking offline.
  - **Must NOT do**: accept unauthenticated upgrades.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 2, Blocks T8/T9/T11/T12/T14, Blocked By T2/T4.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:80`, `Hal-o-swarm_Product_Spec_v1.1.md:86`, `Hal-o-swarm_Product_Spec_v1.1.md:151`.
  - **Acceptance Criteria**: 3 missed heartbeats => node offline event emitted.
  - **QA Scenario (happy)**: Bash integration test connects mock agent with valid token and heartbeat; expect `online`; evidence `.sisyphus/evidence/task-6-ws-pass.txt`.
  - **QA Scenario (error)**: Bash invalid token connect attempt returns 401/close; evidence `.sisyphus/evidence/task-6-ws-fail.txt`.

- [ ] 7. Agent WebSocket client reconnect/backoff/resync
  - **What to do**: implement jittered exponential backoff reconnect, sequence-aware event resend, full snapshot on reconnect.
  - **Must NOT do**: tight reconnect loops without jitter.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 2, Blocks T9/T10/T14, Blocked By T2/T5.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:102`, `Hal-o-swarm_Product_Spec_v1.1.md:828`.
  - **Acceptance Criteria**: reconnect after forced supervisor outage within bounded backoff; session state resynced.
  - **QA Scenario (happy)**: Bash kill/restart supervisor in test; agent reconnects and reports snapshot; evidence `.sisyphus/evidence/task-7-reconnect-pass.txt`.
  - **QA Scenario (error)**: Bash unreachable supervisor for 3 retries logs capped backoff and no crash; evidence `.sisyphus/evidence/task-7-reconnect-fail.txt`.

- [ ] 8. Node registry + session tracker persistence
  - **What to do**: implement registry/tracker stores, status transitions, DB persistence and recovery.
  - **Must NOT do**: in-memory-only state for authoritative session map.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 2, Blocks T11/T13/T14/T19, Blocked By T2/T3/T6.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:149`, `Hal-o-swarm_Product_Spec_v1.1.md:167`.
  - **Acceptance Criteria**: supervisor restart restores previous known sessions as unreachable then live on reconnection.
  - **QA Scenario (happy)**: Bash integration test persists node/session then restarts service and reloads state; evidence `.sisyphus/evidence/task-8-tracker-pass.txt`.
  - **QA Scenario (error)**: Bash corrupted row fixture triggers guarded recovery path with error metric increment; evidence `.sisyphus/evidence/task-8-tracker-fail.txt`.

- [ ] 9. Event ingestion ordering and dedup pipeline
  - **What to do**: add per-agent sequence tracking, dedup cache by event id, gap detection and replay request path.
  - **Must NOT do**: unordered blind append.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 2, Blocks T12/T14/T22, Blocked By T2/T6/T7.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:84`, `Hal-o-swarm_Product_Spec_v1.1.md:187`.
  - **Acceptance Criteria**: duplicate events ignored; out-of-order gaps detected and logged.
  - **QA Scenario (happy)**: Bash feed ordered events 1..N; expect all processed once; evidence `.sisyphus/evidence/task-9-events-pass.txt`.
  - **QA Scenario (error)**: Bash send duplicate + missing seq; expect dedup and gap warning; evidence `.sisyphus/evidence/task-9-events-fail.txt`.

- [ ] 10. opencode SDK adapter and SSE demultiplexer
  - **What to do**: implement adapter around `opencode-sdk-go`; map spec operations to real SDK (`Prompt`, `Abort/Delete`, `ListStreaming`) and mockable interface.
  - **Must NOT do**: direct SDK calls outside adapter package.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 2, Blocks T11/T14/T23, Blocked By T2/T5/T7.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:104`, `Hal-o-swarm_Product_Spec_v1.1.md:98`.
  - **Acceptance Criteria**: all required session operations covered with adapter tests including failure mapping.
  - **QA Scenario (happy)**: Bash `go test ./internal/agent -run TestOpencodeAdapter`; PASS; evidence `.sisyphus/evidence/task-10-adapter-pass.txt`.
  - **QA Scenario (error)**: Bash simulate SSE disconnect and adapter returns recoverable error class; evidence `.sisyphus/evidence/task-10-adapter-fail.txt`.

- [ ] 11. Command dispatcher with idempotency and command result contracts
  - **What to do**: parse command intents, assign/validate `command_id`, enforce idempotency key TTL, dispatch to node/agent.
  - **Must NOT do**: fire-and-forget commands without completion status.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 3, Blocks T12/T13/T14/T20/T23, Blocked By T2/T6/T8/T10.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:221`, `Hal-o-swarm_Product_Spec_v1.1.md:225`.
  - **Acceptance Criteria**: duplicate command submissions return original result and no duplicate execution.
  - **QA Scenario (happy)**: Bash send same command with same idempotency key twice; second is cached response; evidence `.sisyphus/evidence/task-11-command-pass.txt`.
  - **QA Scenario (error)**: Bash command to offline node returns deterministic failure payload; evidence `.sisyphus/evidence/task-11-command-fail.txt`.

- [ ] 12. Discord slash command integration
  - **What to do**: implement command handlers for `/status /nodes /logs /resume /inject /restart /kill /start /cost` with embeds.
  - **Must NOT do**: interactive button workflow in v1.0.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 3, Blocks T23, Blocked By T6/T9/T11.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:223`, `Hal-o-swarm_Product_Spec_v1.1.md:287`.
  - **Acceptance Criteria**: each command mapped to command handler and returns structured response in under timeout window.
  - **QA Scenario (happy)**: Bash run mocked Discord interaction tests for all commands; evidence `.sisyphus/evidence/task-12-discord-pass.txt`.
  - **QA Scenario (error)**: Bash invalid project in `/resume` returns user-safe error embed; evidence `.sisyphus/evidence/task-12-discord-fail.txt`.

- [ ] 13. Supervisor HTTP API (sessions/nodes/events)
  - **What to do**: implement REST endpoints for status/control surface and event log retrieval.
  - **Must NOT do**: undocumented write endpoints that bypass command handler.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 3, Blocks T20/T23, Blocked By T2/T4/T8/T11.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:591`, `Hal-o-swarm_Product_Spec_v1.1.md:595`.
  - **Acceptance Criteria**: endpoint contracts match documented path + payload schemas.
  - **QA Scenario (happy)**: Bash `curl` smoke for `/api/v1/sessions` and `/api/v1/nodes` fields; evidence `.sisyphus/evidence/task-13-api-pass.json`.
  - **QA Scenario (error)**: Bash unauthorized request returns 401; evidence `.sisyphus/evidence/task-13-api-fail.json`.

- [ ] 14. Auto-intervention policy engine
  - **What to do**: implement config-driven resume-on-idle, restart-on-compaction, kill-on-cost switches with retry ceilings.
  - **Must NOT do**: intervention loops without retry cap.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 3, Blocks T23, Blocked By T6/T7/T8/T9/T10/T11.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:249`, `Hal-o-swarm_Product_Spec_v1.1.md:256`.
  - **Acceptance Criteria**: policy actions trigger exactly under configured thresholds and stop after max retries.
  - **QA Scenario (happy)**: Bash simulate idle > threshold and observe resume action count increment; evidence `.sisyphus/evidence/task-14-policy-pass.txt`.
  - **QA Scenario (error)**: Bash force repeated resume failure and verify capped retries + alert event; evidence `.sisyphus/evidence/task-14-policy-fail.txt`.

- [ ] 15. Dependency graph validator and manual trigger
  - **What to do**: implement DAG parser/validator and manual dependent-start command/API.
  - **Must NOT do**: automatic milestone-based triggers in v1.0.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 3, Blocks T20/T23, Blocked By T2/T8.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:374`, `Hal-o-swarm_Product_Spec_v1.1.md:381`.
  - **Acceptance Criteria**: cycle detection rejects invalid dependency config.
  - **QA Scenario (happy)**: Bash valid graph load and manual trigger returns queued downstream; evidence `.sisyphus/evidence/task-15-dep-pass.txt`.
  - **QA Scenario (error)**: Bash cycle fixture fails validation with cycle path; evidence `.sisyphus/evidence/task-15-dep-fail.txt`.

- [ ] 16. Env manifest parser and validator
  - **What to do**: parse `env-manifest.json`, validate schema and project entries.
  - **Must NOT do**: silently ignore unknown required fields.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 4, Blocks T17/T18, Blocked By T1.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:397`, `Hal-o-swarm_Product_Spec_v1.1.md:401`.
  - **Acceptance Criteria**: strict schema validation errors include project/key path.
  - **QA Scenario (happy)**: Bash `go test ./internal/shared -run TestManifestValid`; PASS; evidence `.sisyphus/evidence/task-16-manifest-pass.txt`.
  - **QA Scenario (error)**: Bash invalid runtime version token fails parse; evidence `.sisyphus/evidence/task-16-manifest-fail.txt`.

- [ ] 17. Environment checker implementation
  - **What to do**: implement runtime/tool/env_var/agent_config/context/git/docs checks and drift result payloads.
  - **Must NOT do**: mutate environment in check-only mode.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 4, Blocks T18/T20/T23, Blocked By T5/T16.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:479`, `Hal-o-swarm_Product_Spec_v1.1.md:510`.
  - **Acceptance Criteria**: all categories return pass/fail/warn with expected vs actual values.
  - **QA Scenario (happy)**: Bash fixture env with all requirements present returns `ready`; evidence `.sisyphus/evidence/task-17-envcheck-pass.json`.
  - **QA Scenario (error)**: Bash missing required docs/env var returns `degraded|missing` with drift items; evidence `.sisyphus/evidence/task-17-envcheck-fail.json`.

- [ ] 18. Safe auto-provisioner + approval-required flow
  - **What to do**: implement auto-fix for safe items (AGENT.md/context/docs/hooks/env injection) and manual-required event generation for risky fixes.
  - **Must NOT do**: auto-install packages/SDK without approval token.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 4, Blocks T20/T23, Blocked By T5/T16/T17.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:520`, `Hal-o-swarm_Product_Spec_v1.1.md:524`, `Hal-o-swarm_Product_Spec_v1.1.md:534`.
  - **Acceptance Criteria**: safe fixes apply idempotently; manual-fix items always require approval path.
  - **QA Scenario (happy)**: Bash provision on fixture with missing AGENT.md/context creates files; evidence `.sisyphus/evidence/task-18-provision-pass.txt`.
  - **QA Scenario (error)**: Bash fixture missing Java triggers `manual_required` only, no install action; evidence `.sisyphus/evidence/task-18-provision-fail.txt`.

- [ ] 19. Cost aggregator (Anthropic/OpenAI) and estimator
  - **What to do**: implement periodic polling with retry/backoff, store daily buckets, per-model/provider/project reports, and session estimate fallback.
  - **Must NOT do**: block scheduler loop on one provider failure.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 4, Blocks T20/T23, Blocked By T3/T8.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:205`, `Hal-o-swarm_Product_Spec_v1.1.md:209`, `Hal-o-swarm_Product_Spec_v1.1.md:215`.
  - **Acceptance Criteria**: `/cost` periods return deterministic aggregates and include provider/model splits.
  - **QA Scenario (happy)**: Bash mock provider endpoints + poll cycle -> rows inserted and report endpoint non-empty; evidence `.sisyphus/evidence/task-19-cost-pass.json`.
  - **QA Scenario (error)**: Bash one provider returns 429/500 and aggregator continues with degraded marker; evidence `.sisyphus/evidence/task-19-cost-fail.json`.

- [ ] 20. halctl remote-mode command suite
  - **What to do**: implement `halctl` commands mapped to supervisor APIs for sessions, nodes, cost, env, and agent-md diff/sync.
  - **Must NOT do**: local-mode direct agent invocation in v1.0.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 4, Blocks T23, Blocked By T1/T11/T13/T15/T17/T18/T19.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:543`, `Hal-o-swarm_Product_Spec_v1.1.md:575`, `Hal-o-swarm_Product_Spec_v1.1.md:591`.
  - **Acceptance Criteria**: all documented remote commands execute and print parseable output.
  - **QA Scenario (happy)**: Bash run `halctl status`, `halctl nodes`, `halctl env status` against test supervisor; evidence `.sisyphus/evidence/task-20-halctl-pass.txt`.
  - **QA Scenario (error)**: Bash invalid node/project args return non-zero with helpful message; evidence `.sisyphus/evidence/task-20-halctl-fail.txt`.

- [ ] 21. Security hardening
  - **What to do**: add TLS support path for WSS, strict origin/auth validation, token rotation hooks, and command audit log table.
  - **Must NOT do**: plaintext secret logging.
  - **Recommended Agent Profile**: Category `unspecified-high`, Skills `git-master`.
  - **Parallelization**: YES, Wave 5, Blocks T23/T24, Blocked By T6/T11/T13.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:683`, `Hal-o-swarm_Product_Spec_v1.1.md:689`.
  - **Acceptance Criteria**: insecure origin rejected; audit trail records command actor/action/result.
  - **QA Scenario (happy)**: Bash valid origin/token handshake and audited command entry created; evidence `.sisyphus/evidence/task-21-security-pass.txt`.
  - **QA Scenario (error)**: Bash forged origin/token denied with no state mutation; evidence `.sisyphus/evidence/task-21-security-fail.txt`.

- [ ] 22. Observability package
  - **What to do**: structured logs, Prometheus metrics, readiness/liveness endpoints, basic traces/correlation IDs.
  - **Must NOT do**: silent failures without counter/log context.
  - **Recommended Agent Profile**: Category `quick`, Skills `git-master`.
  - **Parallelization**: YES, Wave 5, Blocks T23/T24, Blocked By T4/T9/T13.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:56`, `Hal-o-swarm_Product_Spec_v1.1.md:846`.
  - **Acceptance Criteria**: health endpoints reflect component status and key metrics exposed.
  - **QA Scenario (happy)**: Bash scrape `/metrics` and `/healthz`; required metrics present; evidence `.sisyphus/evidence/task-22-observe-pass.txt`.
  - **QA Scenario (error)**: Bash force downstream failure and health endpoint degrades appropriately; evidence `.sisyphus/evidence/task-22-observe-fail.txt`.

- [ ] 23. End-to-end and chaos integration test suite
  - **What to do**: implement full lifecycle integration tests across supervisor+agent+mock opencode and failure cases.
  - **Must NOT do**: mark done on unit-only coverage.
  - **Recommended Agent Profile**: Category `deep`, Skills `git-master`.
  - **Parallelization**: YES, Wave 5, Blocks FINAL wave, Blocked By T10-T22.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:338`, `Hal-o-swarm_Product_Spec_v1.1.md:638`, `Hal-o-swarm_Product_Spec_v1.1.md:846`.
  - **Acceptance Criteria**: lifecycle, offline recovery, reconnect, and policy-trigger cases pass in CI.
  - **QA Scenario (happy)**: Bash `go test ./integration -run TestMultiNodeLifecycle`; PASS; evidence `.sisyphus/evidence/task-23-e2e-pass.txt`.
  - **QA Scenario (error)**: Bash `TestNetworkPartitionRecovery` demonstrates expected unreachable->online transition; evidence `.sisyphus/evidence/task-23-e2e-failcase.txt`.

- [ ] 24. Packaging, systemd, install script, runbook
  - **What to do**: provide systemd units, install script, deployment docs, rollback and incident runbook.
  - **Must NOT do**: deployment docs requiring undocumented manual steps.
  - **Recommended Agent Profile**: Category `writing`, Skills `git-master`.
  - **Parallelization**: YES, Wave 5, Blocks FINAL wave, Blocked By T4/T21/T22.
  - **References**: `Hal-o-swarm_Product_Spec_v1.1.md:793`, `Hal-o-swarm_Product_Spec_v1.1.md:807`.
  - **Acceptance Criteria**: clean-node install walkthrough reproducibly starts services and registers node.
  - **QA Scenario (happy)**: Bash run installation dry-run in container and verify generated unit files; evidence `.sisyphus/evidence/task-24-package-pass.txt`.
  - **QA Scenario (error)**: Bash missing auth token in install config fails preflight with actionable output; evidence `.sisyphus/evidence/task-24-package-fail.txt`.

---

## Final Verification Wave (MANDATORY)

- [ ] F1. **Plan Compliance Audit** — `oracle`
  - Verify each Must Have/Must NOT Have against implementation and evidence files.
  - Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT`.

- [ ] F2. **Code Quality Review** — `unspecified-high`
  - Run build/lint/tests and inspect for unsafe casts, dead code, silent catches, and slop patterns.
  - Output: `Build [PASS/FAIL] | Lint [PASS/FAIL] | Tests [N/N] | VERDICT`.

- [ ] F3. **Real QA Scenario Execution** — `unspecified-high`
  - Execute all task QA scenarios and verify evidence completeness under `.sisyphus/evidence/final-qa/`.
  - Output: `Scenarios [N/N] | Integration [N/N] | Edge Cases [N] | VERDICT`.

- [ ] F4. **Scope Fidelity Check** — `deep`
  - Ensure all implemented changes map 1:1 to task scope; flag any scope creep.
  - Output: `Tasks [N/N compliant] | Unaccounted [CLEAN/N] | VERDICT`.

---

## Commit Strategy

| After Task Group | Message Pattern | Verification |
|---|---|---|
| Wave 1 | `chore(scaffold): initialize foundation modules` | `go test ./...` |
| Wave 2 | `feat(core): add ws/state/adapter pipeline` | targeted integration tests |
| Wave 3 | `feat(control-plane): commands api discord policies` | command/API smoke suite |
| Wave 4 | `feat(env-cost-cli): env lifecycle cost halctl` | env/cost/cli suites |
| Wave 5 | `chore(hardening): security observability e2e packaging` | full CI + chaos tests |

---

## Success Criteria

### Verification Commands

```bash
go test ./...                                # Expected: PASS
go test ./integration -run TestMultiNodeLifecycle
curl -s http://localhost:8420/api/v1/nodes  # Expected: JSON node list
curl -s http://localhost:8420/api/v1/sessions
curl -s http://localhost:8420/api/v1/cost/week
halctl status                                # Expected: parseable status output
halctl env status
```

### Final Checklist
- [ ] All Must Have items implemented and verified.
- [ ] All Must NOT Have constraints remain absent.
- [ ] All QA scenarios executed with evidence files.
- [ ] Final review wave (F1-F4) all APPROVE.
- [ ] Release package installs and registers a node on a clean host.
