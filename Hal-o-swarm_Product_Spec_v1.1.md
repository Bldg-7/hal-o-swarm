# HAL-O-SWARM

**Distributed LLM Agent Supervisor**

Product Specification Document — Version 1.1 — February 2026

**DRAFT**

*Multi-server, real-time monitoring and control plane for autonomous LLM coding agents*

---

## 1. Executive Summary

Hal-o-swarm is a distributed supervisor daemon that provides real-time monitoring, control, and cost tracking for autonomous LLM coding agent sessions running across multiple servers. It acts as a central control plane that connects to lightweight agent processes deployed on each worker node, aggregating session state, forwarding events to external channels (Discord, Slack), and enabling remote intervention through chat commands.

The system follows a 2-tier hub-and-spoke architecture: a central supervisor process communicates with per-node agent processes via persistent WebSocket connections. This design decouples session execution (which requires local filesystem access) from orchestration (which requires a unified view across all nodes).

---

## 2. Problem Statement

When running multiple long-lived LLM coding agent sessions in parallel across different servers, several operational challenges emerge:

- No unified visibility into what each agent session is doing across multiple machines
- Session failures, context window saturation, and idle states go unnoticed until manually checked
- No way to intervene remotely (e.g., resume a stuck session, inject a corrective prompt)
- LLM API costs across multiple providers (Anthropic, OpenAI, Google) are tracked in separate dashboards with no unified view
- Adding a new worker server requires manual setup of monitoring and alerting

Existing tools (opencode-orchestrator, agent-of-empires) are either GitHub-issue-centric or tmux-based, and none provide a unified multi-server control plane with external channel integration.

---

## 3. System Architecture

### 3.1 2-Tier Hub-and-Spoke Model

The architecture separates concerns into two tiers:

| Tier  | Component      | Location                          | Responsibility                                                |
|-------|----------------|-----------------------------------|---------------------------------------------------------------|
| Hub   | hal-supervisor | Central server (LXC or dedicated) | Aggregation, routing, command handling, cost tracking          |
| Spoke | hal-agent      | Each worker node                  | Local session management, event forwarding, command execution  |

### 3.2 Architecture Diagram

```
┌────────────────────────────────────────────────┐
│        Discord / Slack (User Interface)        │
│      Commands in, Alerts out, Cost reports     │
└───────────────────┬────────────────────────────┘
                    │ Webhook / Bot API
                    ▼
┌────────────────────────────────────────────────┐
│       hal-supervisor (Central Daemon)          │
│                                                │
│  ┌───────────┐ ┌───────────┐ ┌───────────┐    │
│  │  Session   │ │   Event   │ │    Cost   │    │
│  │  Tracker   │ │   Router  │ │ Aggregator│    │
│  └───────────┘ └───────────┘ └───────────┘    │
│  ┌───────────┐ ┌───────────┐                   │
│  │  Command  │ │    Node   │                   │
│  │  Handler  │ │  Registry │                   │
│  └───────────┘ └─────┬─────┘                   │
└───────────────────────┼────────────────────────┘
                        │ WebSocket (outbound)
              ┌─────────┴──────────┐
              ▼                    ▼
┌────────────────────┐ ┌────────────────────┐
│      Node A        │ │      Node B        │
│    hal-agent       │ │    hal-agent       │
│  opencode serve    │ │  opencode serve    │
│  [P1] [P3] [P4]   │ │  [P6 ROM build]   │
└────────────────────┘ └────────────────────┘
```

### 3.3 Communication Protocol

Agent-to-supervisor communication uses WebSocket with outbound connections from agents. This eliminates firewall configuration on worker nodes. Ephemeral servers (e.g., Hetzner build instances) connect automatically on creation by knowing only the supervisor's address.

The protocol carries three message types:

- **Event Stream (agent → supervisor):** SSE events from opencode serve are forwarded in real-time. Includes `session.idle`, `session.compacted`, `session.error`, `tool.execute.after`, and custom events.
- **Commands (supervisor → agent):** Session control operations such as create, prompt, kill, and restart. Agent executes locally via opencode SDK.
- **Heartbeat (bidirectional):** 30-second interval. If supervisor misses 3 consecutive heartbeats, the node is marked offline and an alert fires.

---

## 4. Component Specifications

### 4.1 hal-agent (Per-Node Process)

A lightweight process deployed on each worker server. It wraps opencode serve and bridges the gap between the local opencode SDK and the remote supervisor.

#### 4.1.1 Responsibilities

- Start and manage opencode serve on the local machine
- Subscribe to SSE events from all local sessions and forward to supervisor via WebSocket
- Execute commands received from supervisor (create session, inject prompt, kill session)
- Report node metadata: hostname, available projects, resource usage (CPU, RAM, disk)
- Auto-reconnect to supervisor on connection loss with exponential backoff

#### 4.1.2 API Surface

```go
type AgentAPI interface {
    // Session management (proxied to opencode SDK)
    ListSessions() []SessionInfo
    CreateSession(project string, prompt string) SessionID
    PromptSession(sessionID string, message string) error
    KillSession(sessionID string) error
    SessionStatus(sessionID string) SessionStatus

    // Event subscription
    Subscribe(filter *EventFilter) <-chan SessionEvent

    // Node info
    NodeInfo() NodeMetadata
    HealthCheck() HealthStatus
}
```

#### 4.1.3 Deployment

The agent is a single binary (compiled Go) managed by systemd. Installation on a new node requires only the binary and the supervisor's WebSocket URL.

```bash
# Install on a new worker node
curl -fsSL https://hal-o-swarm.dev/install-agent | bash
hal-agent --supervisor ws://supervisor:8420 --token <auth-token>
```

### 4.2 hal-supervisor (Central Daemon)

The central process that maintains a unified view of all nodes, sessions, and events. It does not run opencode sessions directly.

#### 4.2.1 Module Breakdown

| Module           | Responsibility                                               | Key Interfaces                                     |
|------------------|--------------------------------------------------------------|----------------------------------------------------|
| Node Registry    | Track connected agents, health status, available projects    | Register(), Deregister(), GetNode(), ListNodes()   |
| Session Tracker  | Unified view of all sessions across all nodes                | GetAllSessions(), GetSession(), OnSessionChange()  |
| Event Router     | Route events from agents to external channels based on rules | AddRule(), RemoveRule(), Evaluate()                 |
| Cost Aggregator  | Poll LLM provider APIs, aggregate by project/model/period    | GetReport(), GetDailyCost(), GetProviderUsage()    |
| Command Handler  | Parse Discord/Slack commands, dispatch to correct agent      | RegisterCommand(), Dispatch()                      |
| Dependency Graph | Track inter-project dependencies, auto-trigger downstream    | AddEdge(), GetReady(), OnProjectComplete()         |

#### 4.2.2 Node Registry

When an agent connects via WebSocket, it sends a registration message containing its hostname, project list, and capabilities. The registry maintains the connection and monitors heartbeats. If a node goes offline, all its sessions are marked as unreachable and an alert fires.

```go
type NodeEntry struct {
    ID            string                    // auto-generated UUID
    Hostname      string
    Address       string                    // WebSocket remote address
    Projects      []string                  // e.g., ["ai-os-interfaces", "ai-os-l0"]
    Sessions      map[string]SessionState
    Resources     ResourceUsage             // CPUPercent, RAMUsedMB, DiskUsedGB
    Status        NodeStatus                // online | offline | degraded
    LastHeartbeat time.Time
    ConnectedAt   time.Time
}
```

#### 4.2.3 Session Tracker

Aggregates session state from all connected agents into a single queryable store. Each session entry includes:

```go
type TrackedSession struct {
    SessionID       string
    NodeID          string
    Project         string
    Status          SessionStatus   // busy | idle | error | unreachable
    TokenUsage      TokenUsage      // Prompt, Completion, Total
    CompactionCount int
    CurrentTask     string          // extracted from .context/CURRENT_TASK.md
    LastActivity    time.Time
    SessionCost     float64         // estimated USD
    Model           string          // e.g., "claude-sonnet-4-5"
    StartedAt       time.Time
}
```

#### 4.2.4 Event Router

A rule-based engine that matches incoming events against configurable patterns and routes them to external channels. Rules are defined in the supervisor configuration file.

```json
// Example routing rules (supervisor.config.json)
{
  "routes": [
    { "match": "session.error", "target": "discord#alerts" },
    { "match": "session.compacted", "target": "discord#dev-log" },
    { "match": "session.idle && stuck > 5m", "target": "discord#alerts" },
    { "match": "task.completed", "target": "discord#dev-log" },
    { "match": "node.offline", "target": "discord#alerts" },
    { "match": "cost.daily > 20", "target": "discord#alerts" }
  ]
}
```

#### 4.2.5 Cost Aggregator

Polls LLM provider Admin APIs at configurable intervals (default: 1 hour) and stores aggregated cost data in a local SQLite database.

| Provider        | API Endpoint                            | Auth Method          | Granularity            |
|-----------------|-----------------------------------------|----------------------|------------------------|
| Anthropic       | /v1/organizations/usage_report/messages | Admin API Key        | Daily buckets, per-model |
| OpenAI          | /v1/organization/usage/completions      | Organization API Key | Daily buckets, per-model |
| Google (Gemini) | Cloud Billing API                       | Service Account      | Daily, per-SKU         |

Cost data is queryable by time range, provider, model, and project. The aggregator also computes estimated per-session costs based on token usage reported by opencode's session metadata.

---

## 5. Intervention Mechanisms

A core differentiator of Hal-o-swarm is the ability to intervene in running agent sessions remotely via chat commands. All commands are dispatched through the Command Handler, which resolves the target node and delegates execution to the appropriate agent.

### 5.1 Command Reference

| Command                  | Description                                        | Behavior                                                    |
|--------------------------|----------------------------------------------------|-------------------------------------------------------------|
| `/status`                | Show all active sessions across all nodes          | Queries Session Tracker, returns formatted embed            |
| `/status <project>`     | Show detailed status for a specific project        | Includes token usage, current task, compaction count        |
| `/resume <project>`     | Resume an idle session                             | Injects continue prompt via client.session.prompt()         |
| `/inject <project> <msg>` | Send arbitrary prompt to a running session       | Forwarded to agent, injected into active session            |
| `/restart <project>`    | Graceful restart: save state, end session, start new | Agent updates .context/, kills session, creates new one   |
| `/kill <project>`       | Force-kill a session without saving                | Immediate session termination                               |
| `/start <project>`      | Start a new session for a project                  | Agent creates session with standard init prompt             |
| `/cost [period]`        | Show LLM cost report                              | Queries Cost Aggregator for today/week/month                |
| `/nodes`                | List all connected nodes and their status          | Queries Node Registry                                       |
| `/logs <project> [n]`   | Show recent events for a project                   | Returns last n events from event log                        |

### 5.2 Intervention Scenarios

| Scenario                       | Detection                                    | Response                                                      |
|--------------------------------|----------------------------------------------|---------------------------------------------------------------|
| Session stuck in idle          | session.idle event + no activity for >5 min  | Auto-resume or alert for manual /resume                       |
| Agent in error loop            | 3+ consecutive session.error events          | Alert + option to /restart or /inject corrective prompt       |
| Context window saturated       | compactionCount >= 2                         | Alert + auto /restart (new session with .context/ handover)   |
| Node goes offline              | 3 missed heartbeats (90 seconds)             | Mark sessions unreachable, alert, attempt reconnect           |
| Cost threshold exceeded        | Daily cost > configured limit                | Alert + option to /kill non-critical sessions                 |
| Wrong implementation direction | Manual observation                           | User sends /inject with corrective instructions               |

### 5.3 Auto-Intervention Policies

Certain interventions can be configured to run automatically without human approval. These are defined in the supervisor configuration:

```json
// supervisor.config.json
{
  "auto_intervention": {
    "resume_on_idle": {
      "enabled": true,
      "idle_threshold_minutes": 5,
      "max_retries": 3
    },
    "restart_on_compaction": {
      "enabled": true,
      "compaction_threshold": 2
    },
    "kill_on_cost": {
      "enabled": false,
      "daily_limit_usd": 50
    }
  }
}
```

---

## 6. External Channel Integration

### 6.1 Supported Channels

| Channel            | Integration Method | Capabilities                                            |
|--------------------|--------------------|---------------------------------------------------------|
| Discord            | discordgo bot      | Commands, rich embeds, button interactions, threads     |
| Slack              | slack-go SDK       | Commands, Block Kit messages, interactive actions       |
| n8n Webhook        | HTTP POST          | Event forwarding for custom workflow automation         |
| Telegram (future)  | telebot (Go)       | Commands, inline keyboards                              |

### 6.2 Event Message Format

Events are rendered as rich embeds (Discord) or Block Kit messages (Slack) with contextual information and action buttons.

```json
// Example: session.idle event → Discord embed
{
  "title": "🟡 P1 Session Idle",
  "fields": [
    { "name": "Node", "value": "dev-opencode" },
    { "name": "Task", "value": "Implementing IEventCollector AIDL" },
    { "name": "Tokens", "value": "45,231 / 200,000 (22%)" },
    { "name": "Compaction", "value": "0" },
    { "name": "Duration", "value": "12m 34s" }
  ],
  "buttons": [
    { "label": "▶ Resume", "action": "/resume P1" },
    { "label": "🔄 Restart", "action": "/restart P1" },
    { "label": "⏹ Kill", "action": "/kill P1" }
  ]
}
```

### 6.3 Cost Report Format

```json
// Example: /cost week response
{
  "title": "📊 Weekly LLM Cost Report (Feb 10-16)",
  "sections": [
    {
      "provider": "Anthropic",
      "models": [
        { "name": "Sonnet 4.5", "tokens": "2.1M", "cost": "$8.40" },
        { "name": "Haiku 4.5", "tokens": "890K", "cost": "$1.34" }
      ]
    },
    {
      "provider": "OpenAI",
      "models": [
        { "name": "o3", "tokens": "320K", "cost": "$4.80" }
      ]
    }
  ],
  "total": "$14.54",
  "dailyAvg": "$2.08"
}
```

---

## 7. Session Lifecycle Management

### 7.1 Session State Machine

```
/start
  │
  ▼
┌─────────────┐  auto-continue  ┌──────────┐
│   RUNNING   │────────────────→│   IDLE   │
│   (busy)    │←── /resume ─────│          │
└─────┬───────┘                 └────┬─────┘
      │                              │
      │ compaction >= 2              │ stuck > threshold
      ▼                              ▼
┌─────────────┐  .context/ saved ┌──────────┐
│  HANDOVER   │─────────────────→│ RESTART  │
└─────────────┘                  └────┬─────┘
                                      │
                                      ▼  new session + init prompt
                                ┌─────────────┐
                                │   RUNNING   │ (reads .context/CURRENT_TASK.md)
                                └─────────────┘
```

### 7.2 Context Handover Protocol

When a session reaches the handover state (triggered by compaction count, manual /restart, or task completion), the following sequence executes:

1. Supervisor sends HANDOVER command to agent
2. Agent injects handover prompt into active session: update `.context/PROGRESS.md`, `CURRENT_TASK.md` with exact stop point, git commit
3. Agent waits for session to reach idle state (max 60 seconds)
4. Agent kills the session
5. Agent creates a new session with init prompt: read `.context/PROGRESS.md` and `CURRENT_TASK.md`, continue from the documented stop point
6. Supervisor updates Session Tracker with new session ID

### 7.3 Dependency-Aware Scheduling

The supervisor maintains a project dependency graph. When a project completes a milestone (detected via `.context/PROGRESS.md` status change), it can automatically trigger dependent projects.

```json
// supervisor.config.json
{
  "dependencies": {
    "ai-os-l0": { "depends_on": ["ai-os-interfaces"] },
    "ai-os-l1": { "depends_on": ["ai-os-interfaces"] },
    "ai-os-l2": { "depends_on": ["ai-os-interfaces"] },
    "ai-os-launcher": { "depends_on": ["ai-os-interfaces"] },
    "ai-os-rom": { "depends_on": ["ai-os-l0", "ai-os-l1", "ai-os-l2", "ai-os-launcher"] }
  }
}
```

---

## 8. Environment Provisioning & Standardization

현재 스펙에서 agent는 "이미 세팅된 환경에서 opencode serve를 감싸는 것"만 담당한다. 하지만 실제 운영에서는 새 노드 투입, 프로젝트 추가, 환경 드리프트 감지 등 **환경 자체의 lifecycle**을 관리해야 한다. 이 섹션은 supervisor가 환경 상태를 확인하고, agent를 통해 환경을 표준화하는 메커니즘을 정의한다.

### 8.1 Environment Manifest (`env-manifest.json`)

각 프로젝트의 기대 환경을 선언적으로 정의하는 매니페스트. supervisor가 관리하며 agent가 실행 시 이를 기준으로 환경을 검증/프로비저닝한다.

```json
// env-manifest.json (supervisor에서 관리, 프로젝트별 정의)
{
  "version": "1.0",
  "projects": {
    "ai-os-interfaces": {
      "runtime": {
        "java": ">=17",
        "gradle": ">=8.4",
        "android_sdk": { "compile_sdk": 34, "build_tools": "34.0.0" }
      },
      "tools": ["protoc", "aidl"],
      "env_vars": {
        "ANDROID_HOME": "/opt/android-sdk",
        "JAVA_HOME": "/usr/lib/jvm/java-17"
      },
      "agent_config": {
        "AGENT.md": "templates/ai-os-interfaces/AGENT.md",
        "context_dir": ".context/",
        "required_docs": ["INTERFACE_l0_l1.md", "INTERFACE_l1_l2.md"]
      },
      "git": {
        "remote": "git@proxmox:ai-os-interfaces.git",
        "branch": "main",
        "hooks": ["pre-commit"]
      }
    },
    "ai-os-l1": {
      "runtime": {
        "java": ">=17",
        "gradle": ">=8.4",
        "ndk": ">=26.1"
      },
      "tools": ["protoc", "cmake"],
      "native_libs": ["faiss"],
      "agent_config": {
        "AGENT.md": "templates/ai-os-l1/AGENT.md",
        "context_dir": ".context/",
        "required_docs": ["INTERFACE_l1_l2.md", "system_spec.md"]
      }
    }
  }
}
```

### 8.2 AGENT.md — 표준화된 에이전트 지침 템플릿

기존 실행 전략의 `CLAUDE.md`를 Hal-o-swarm 체계에 통합한다. supervisor가 **AGENT.md 템플릿을 중앙 관리**하고, agent가 프로젝트 초기화 시 자동 배포한다. 이를 통해 모든 노드에서 동일한 에이전트 행동을 보장한다.

```markdown
# AGENT.md Template (supervisor가 관리)

# {{project_name}} — {{project_description}}

## Scope
{{scope_description}}

## Rules
1. 세션 시작 시 .context/PROGRESS.md 먼저 읽기
2. .context/CURRENT_TASK.md에서 중단 지점 확인 후 이어서 작업
3. 이 프로젝트 범위 밖 작업 금지: {{excluded_projects}}
4. 인터페이스 변경 시 반드시 .context/DECISIONS.md에 기록

## Interface Contracts
{{#each interface_docs}}
- {{this.name}}: {{this.description}}
{{/each}}

## Tech Stack
{{tech_stack}}

## Session Protocol
- 시작: .context/PROGRESS.md + CURRENT_TASK.md 읽기
- 종료: PROGRESS.md 갱신 → CURRENT_TASK.md 갱신 → SESSION_LOG/ 작성 → git commit
```

supervisor는 프로젝트별 변수를 주입하여 최종 AGENT.md를 생성한다. AGENT.md 버전은 git으로 추적되며, 변경 시 해당 프로젝트의 다음 세션부터 자동 적용된다.

### 8.3 Environment Check Protocol

agent가 노드 등록 시 또는 주기적으로 환경 상태를 검증하는 프로토콜.

```go
type EnvCheckResult struct {
    Project     string
    NodeID      string
    Timestamp   time.Time
    Status      EnvStatus          // ready | degraded | missing
    Checks      []CheckItem
    DriftItems  []DriftItem        // manifest와 실제 환경의 차이
}

type CheckItem struct {
    Category    string             // runtime | tool | env_var | agent_config | git
    Name        string             // e.g., "java", "gradle", "AGENT.md"
    Expected    string             // manifest에 정의된 기대값
    Actual      string             // 실제 감지된 값
    Status      CheckStatus        // pass | fail | warn
}

type DriftItem struct {
    File        string             // e.g., "AGENT.md", ".context/PROGRESS.md"
    Type        DriftType          // missing | outdated | modified
    Detail      string
}
```

#### 검증 항목

| Category     | 검증 대상                           | 방법                                            |
|-------------|-------------------------------------|-------------------------------------------------|
| runtime     | Java, Gradle, NDK, Android SDK 버전  | `java -version`, `gradle --version` 파싱         |
| tool        | protoc, cmake, aidl 등              | `which` + `--version`                            |
| env_var     | ANDROID_HOME, JAVA_HOME 등          | 환경변수 존재 + 경로 유효성                        |
| agent_config| AGENT.md 존재 + 버전 일치            | SHA-256 해시 비교 (supervisor 템플릿 vs 로컬)      |
| context     | .context/ 디렉토리 구조              | PROGRESS.md, CURRENT_TASK.md, DECISIONS.md 존재   |
| git         | 리모트 설정, 브랜치, 훅              | `git remote -v`, `git branch`, 훅 파일 존재       |
| docs        | 필수 설계 문서 존재                   | required_docs 목록 대조                           |

### 8.4 Auto-Provisioning

환경 검증에서 실패 항목이 발견되면 agent가 자동으로 수정을 시도한다. 수정 범위는 안전한 작업(파일 생성, 환경변수 설정)으로 제한하고, 위험한 작업(패키지 설치, SDK 다운로드)은 승인 후 실행한다.

#### Auto-fix (승인 불필요)

| 항목                  | 자동 수정 내용                                               |
|-----------------------|-------------------------------------------------------------|
| AGENT.md 누락/구버전   | supervisor 템플릿에서 생성/업데이트                            |
| .context/ 디렉토리 없음 | PROGRESS.md, CURRENT_TASK.md, DECISIONS.md 스캐폴딩 생성      |
| 필수 docs 누락         | supervisor의 docs 저장소에서 복사                              |
| git hook 누락          | 정의된 hook 스크립트 설치                                     |
| 환경변수 미설정         | agent 프로세스 환경에 주입 (시스템 수준 변경 아님)               |

#### Manual-fix (Discord/Slack 승인 필요)

| 항목                   | 알림 내용                                                    |
|------------------------|-------------------------------------------------------------|
| Java/Gradle/NDK 미설치  | 설치 명령어 제시 + `/approve provision <node> <package>` 대기 |
| Android SDK 버전 불일치  | sdkmanager 명령어 제시 + 승인 대기                            |
| native lib 누락 (faiss) | 빌드/설치 스크립트 제시 + 승인 대기                            |
| 디스크 공간 부족         | 정리 대상 제시 + 승인 대기                                    |

### 8.5 CLI: `halctl` — 개발/운영용 CLI 도구

supervisor API에 직접 접근하는 CLI 도구. Discord/Slack 없이도 환경 관리가 가능하다.

```bash
# 환경 검증
halctl env check                          # 현재 노드의 모든 프로젝트 환경 검증
halctl env check --project ai-os-l1       # 특정 프로젝트만
halctl env check --node dev-opencode      # 특정 노드 (supervisor에서 실행)

# 환경 프로비저닝
halctl env provision                      # 자동 수정 가능한 항목 모두 적용
halctl env provision --project ai-os-l1   # 특정 프로젝트만
halctl env provision --dry-run            # 변경 예정 사항만 출력

# AGENT.md 관리
halctl agent-md show ai-os-l1             # 현재 AGENT.md 내용 확인
halctl agent-md diff ai-os-l1             # supervisor 템플릿과 로컬 차이
halctl agent-md sync                      # 모든 프로젝트의 AGENT.md를 최신화
halctl agent-md sync --project ai-os-l1   # 특정 프로젝트만

# 환경 상태 조회 (supervisor 연결)
halctl env status                         # 전체 노드 환경 상태 대시보드
halctl env drift                          # manifest와 실제 환경 차이 리포트

# 세션 + 기존 명령어도 통합
halctl status                             # = Discord /status
halctl resume ai-os-l1                    # = Discord /resume
halctl cost week                          # = Discord /cost week
halctl nodes                              # = Discord /nodes
```

#### halctl 아키텍처

```
halctl (CLI)
  │
  ├── 로컬 모드 (agent 직접 호출)
  │   └── halctl env check        → agent의 EnvChecker 직접 실행
  │   └── halctl env provision    → agent의 Provisioner 직접 실행
  │
  └── 원격 모드 (supervisor API 호출)
      └── halctl env status       → GET /api/v1/env/status
      └── halctl env drift        → GET /api/v1/env/drift
      └── halctl status           → GET /api/v1/sessions
      └── halctl resume <proj>    → POST /api/v1/sessions/{proj}/resume
```

### 8.6 Supervisor API 확장

환경 관리를 위한 API 엔드포인트를 추가한다.

| Endpoint                              | Method | Description                                          |
|---------------------------------------|--------|------------------------------------------------------|
| `/api/v1/env/manifest`               | GET    | 현재 env-manifest.json 조회                           |
| `/api/v1/env/manifest`               | PUT    | manifest 업데이트 (AGENT.md 템플릿 변경 포함)          |
| `/api/v1/env/status`                 | GET    | 전체 노드 환경 상태 조회                               |
| `/api/v1/env/status/{node}`          | GET    | 특정 노드 환경 상태                                    |
| `/api/v1/env/check/{node}`           | POST   | 특정 노드에 환경 검증 트리거                            |
| `/api/v1/env/provision/{node}`       | POST   | 특정 노드에 auto-fix 프로비저닝 실행                    |
| `/api/v1/env/drift`                  | GET    | 전체 drift 리포트                                     |
| `/api/v1/agent-md/{project}`         | GET    | 프로젝트 AGENT.md 현재 내용                            |
| `/api/v1/agent-md/{project}/diff`    | GET    | 템플릿 vs 로컬 diff                                   |
| `/api/v1/agent-md/sync`             | POST   | 전체 또는 특정 프로젝트 AGENT.md 동기화                  |

### 8.7 Event Router 확장

환경 관련 이벤트를 기존 Event Router에 통합한다.

```json
// supervisor.config.json — routes 추가
{
  "routes": [
    { "match": "env.check.fail", "target": "discord#alerts" },
    { "match": "env.drift.detected", "target": "discord#dev-log" },
    { "match": "env.provision.complete", "target": "discord#dev-log" },
    { "match": "env.provision.manual_required", "target": "discord#alerts" },
    { "match": "agent-md.updated", "target": "discord#dev-log" }
  ]
}
```

### 8.8 Discord/Slack 명령어 확장

| Command                            | Description                                    |
|------------------------------------|------------------------------------------------|
| `/env status`                      | 전체 노드 환경 상태 요약                         |
| `/env check <node>`               | 특정 노드 환경 검증 트리거                        |
| `/env provision <node>`           | auto-fix 프로비저닝 실행                          |
| `/approve provision <node> <pkg>` | manual-fix 항목 승인                              |
| `/agent-md diff <project>`        | AGENT.md 템플릿 vs 로컬 차이 확인                 |
| `/agent-md sync [project]`        | AGENT.md 동기화 (전체 또는 특정 프로젝트)          |

### 8.9 프로비저닝 시퀀스 (새 노드 투입)

```
1. curl install-agent → hal-agent 바이너리 설치
2. agent.config.json 작성 (projects 목록 포함)
3. systemctl start hal-agent
4. agent → supervisor 접속 (WebSocket)
5. supervisor → agent: env-manifest.json 전송
6. agent: EnvChecker 실행 → EnvCheckResult 리포트
7. agent: auto-fix 항목 자동 프로비저닝
   ├── AGENT.md 생성
   ├── .context/ 스캐폴딩
   ├── docs/ 복사
   └── git hooks 설치
8. agent → supervisor: provision 완료 리포트
9. supervisor: manual-fix 필요 항목 → Discord 알림
10. 사용자 /approve → agent: 패키지 설치 등 실행
11. 환경 ready → 세션 시작 가능
```

---

## 9. Technology Stack

| Layer                | Technology                  | Rationale                                                                   |
|----------------------|-----------------------------|-----------------------------------------------------------------------------|
| Language             | Go                          | High performance, single binary, strong concurrency, native opencode SDK integration |
| Runtime              | Go runtime                  | Fast startup, goroutine-based concurrency, single static binary             |
| Agent ↔ Supervisor   | WebSocket (gorilla/websocket) | Bidirectional real-time, outbound connection (no firewall issues)          |
| opencode Integration | opencode-sdk-go             | Go SDK for session CRUD, SSE event subscription                             |
| Database             | SQLite (mattn/go-sqlite3)   | Lightweight, embedded, CGO binding, sufficient for event logs and cost data |
| Discord              | discordgo                   | Go Discord library with slash commands and interactions                      |
| Slack                | slack-go                    | Go Slack library with Socket Mode and interactive actions                   |
| Process Manager      | systemd                     | Native Linux service management, journald logging, auto-restart             |
| oh-my-opencode       | Plugin (bundled)            | Session-level autonomy: Sisyphus orchestrator, context hooks, auto-continue |

---

## 10. Configuration Reference

### 10.1 Supervisor Configuration

```json
// supervisor.config.json
{
  "server": {
    "port": 8420,
    "auth_token": "...",
    "heartbeat_interval_sec": 30,
    "heartbeat_timeout_count": 3
  },
  "channels": {
    "discord": {
      "bot_token": "...",
      "guild_id": "...",
      "channels": {
        "alerts": "channel-id",
        "dev-log": "channel-id",
        "build-log": "channel-id"
      }
    },
    "slack": {
      "bot_token": "...",
      "channels": { "alerts": "C...", "dev-log": "C..." }
    },
    "n8n": {
      "webhook_url": "https://n8n.local/webhook/hal-o-swarm"
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": { "admin_api_key": "..." },
      "openai": { "org_api_key": "..." }
    }
  },
  "routes": [ "..." ],
  "auto_intervention": { "..." },
  "dependencies": { "..." }
}
```

### 10.2 Agent Configuration

```json
// agent.config.json (per worker node)
{
  "supervisor_url": "ws://192.168.10.x:8420",
  "auth_token": "...",
  "opencode_port": 4096,
  "projects": [
    { "name": "ai-os-interfaces", "directory": "/home/user/ai-os-interfaces" },
    { "name": "ai-os-l0", "directory": "/home/user/ai-os-l0" }
  ]
}
```

---

## 11. Project Structure

```
hal-o-swarm/
├── cmd/
│   ├── supervisor/           # Supervisor entry point
│   │   └── main.go
│   ├── agent/                # Agent entry point
│   │   └── main.go
│   └── halctl/               # CLI tool entry point
│       └── main.go
│
├── internal/
│   ├── supervisor/           # Central daemon
│   │   ├── registry.go       # Agent connection management
│   │   ├── tracker.go        # Unified session state
│   │   ├── router.go         # Rule-based event routing
│   │   ├── cost.go           # LLM provider cost polling
│   │   ├── commands.go       # Chat command dispatch
│   │   ├── depgraph.go       # Project dependency DAG
│   │   ├── envapi.go         # Environment provisioning API handlers
│   │   └── channels/         # Discord, Slack, n8n adapters
│   │
│   ├── agent/                # Per-node lightweight process
│   │   ├── proxy.go          # opencode SDK wrapper
│   │   ├── forwarder.go      # SSE → WebSocket bridge
│   │   ├── wsclient.go       # Supervisor connection + reconnect
│   │   ├── envcheck.go       # Environment checker
│   │   └── provision.go      # Auto-provisioner
│   │
│   ├── halctl/               # CLI logic
│   │   ├── env.go            # env check/provision/status/drift commands
│   │   ├── agentmd.go        # agent-md show/diff/sync commands
│   │   └── session.go        # status/resume/kill/cost/nodes commands
│   │
│   └── shared/               # Shared types and protocol
│       ├── types.go
│       ├── protocol.go       # WebSocket message schemas
│       └── envtypes.go       # EnvCheckResult, DriftItem, etc.
│
├── templates/                # AGENT.md templates (per-project)
│   ├── ai-os-interfaces/
│   │   └── AGENT.md
│   ├── ai-os-l0/
│   │   └── AGENT.md
│   ├── ai-os-l1/
│   │   └── AGENT.md
│   └── ...
│
├── env-manifest.json         # Environment manifest (project requirements)
├── supervisor.config.json    # Supervisor configuration
├── go.mod                    # Go module definition
├── go.sum
└── README.md
```

---

## 12. Deployment Guide

### 12.1 Initial Deployment (AI OS Project)

| Step | Location              | Action                                                                   |
|------|-----------------------|--------------------------------------------------------------------------|
| 1    | Proxmox (lab)         | Create LXC for supervisor or use n8n-server (LXC 105)                   |
| 2    | Supervisor LXC        | Clone hal-o-swarm, build Go binaries, configure supervisor.config.json   |
| 3    | Supervisor LXC        | Start supervisor: `systemctl start hal-supervisor`                       |
| 4    | dev-opencode (VM 102) | Install agent: configure agent.config.json with project paths            |
| 5    | dev-opencode (VM 102) | Start agent: `systemctl start hal-agent`                                 |
| 6    | Discord               | Add bot to server, configure slash commands                              |
| 7    | Verify                | Run /nodes and /status from Discord to confirm connectivity              |

### 12.2 Adding a New Node

When a new worker server comes online (e.g., a Hetzner build instance), adding it to the swarm requires minimal setup:

```bash
# On the new server
curl -fsSL https://hal-o-swarm.dev/install-agent | bash

cat > agent.config.json << 'EOF'
{
  "supervisor_url": "ws://192.168.10.x:8420",
  "auth_token": "shared-secret",
  "projects": [
    { "name": "ai-os-rom", "directory": "/aosp/vendor/hal-rom" }
  ]
}
EOF

systemctl start hal-agent
```

The agent connects outbound to the supervisor. No firewall changes needed on either side. The supervisor's Node Registry auto-registers the new node and its sessions become visible in /status.

---

## 13. Future Roadmap

| Phase | Feature                    | Description                                                            |
|-------|----------------------------|------------------------------------------------------------------------|
| v1.0  | Core supervisor + agent    | Session tracking, event routing, Discord commands, cost reports        |
| v1.1  | Auto-intervention policies | Configurable auto-resume, auto-restart, cost kill-switch               |
| v1.2  | Web dashboard              | Real-time session visualization, cost charts, node topology            |
| v1.3  | Slack integration          | Full parity with Discord: commands, alerts, interactive actions        |
| v2.0  | Multi-tenant               | Support multiple users/orgs with isolated projects and cost tracking   |
| v2.1  | Agent marketplace          | Custom verification agents, domain-specific review agents              |
| v2.2  | Predictive cost alerts     | ML-based cost forecasting from token usage patterns                    |

---

## 14. Known Constraints and Risks

| Constraint                                              | Impact                                                          | Mitigation                                                                        |
|---------------------------------------------------------|-----------------------------------------------------------------|-----------------------------------------------------------------------------------|
| opencode serve subagent hang bug (Issue #6573)          | REST API sessions hang when Task tool spawns subagents          | Use opencode run -p (CLI mode) for execution, serve API for monitoring only       |
| Concurrent session interference (Issue #4251)           | Multiple sessions on same repo can conflict                     | Project isolation via separate repos (already in place for AI OS)                 |
| oh-my-opencode 200K context hardcode (Issue #1753)      | Premature compaction with 1M-capable models                     | Set ANTHROPIC_1M_CONTEXT=true environment variable                                |
| Compaction overflow on large tool output (Issue #10634) | Unexpected context limit after subagent returns large result    | Preemptive compaction threshold at 70%, dynamic truncator hook                    |
| Anthropic third-party OAuth restrictions                | Anthropic may restrict API access for third-party tools         | Direct API key authentication (not OAuth), multiple provider support as fallback  |
