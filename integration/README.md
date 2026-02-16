# Integration Test Suite

This package contains full lifecycle integration coverage across supervisor components, WebSocket agent transport, and the mock OpenCode adapter.

## Scope

- End-to-end lifecycle:
  - supervisor harness startup
  - agent connection and registration
  - command dispatch (`create_session`, `prompt_session`, `session_status`, `kill_session`)
  - graceful shutdown and offline state transitions
- Multi-node concurrency:
  - multiple agents connect and execute commands in parallel projects
- Recovery and chaos scenarios:
  - network partition and reconnect with snapshot resend
  - supervisor crash/restart with DB-backed state reload
  - agent crash/restart with node offline/online transitions
  - heartbeat timeout detection
  - high-volume event ordering and dedup safety checks
  - DB corruption tolerance on startup recovery
- Policy integration:
  - resume-on-idle and restart-on-compaction trigger command dispatch
  - `policy.action` events persist through event pipeline

## Design Notes

- Uses `agent.MockOpencodeAdapter` for deterministic session/event behavior.
- Uses temporary SQLite DB per test and runs storage migrations.
- Uses `httptest` WebSocket server for hermetic agent/supervisor communication.
- Every test cleans up resources through `t.Cleanup()` or explicit harness shutdown.

## Running Targeted Scenarios

```bash
go test ./integration -run TestMultiNodeLifecycle -race
go test ./integration -run TestNetworkPartitionRecovery -race
```

## Full Package

```bash
go test ./integration -race
```
