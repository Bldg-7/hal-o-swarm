# HAL-O-SWARM

**Distributed LLM Agent Supervisor** — Real-time monitoring and control plane for autonomous LLM coding agents across multiple servers.

![Version](https://img.shields.io/badge/version-1.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Status](https://img.shields.io/badge/status-production-brightgreen)

## Overview

HAL-O-SWARM provides centralized supervision for distributed LLM coding agents, enabling real-time monitoring, remote control, cost tracking, and automated intervention across your infrastructure.

**Key Capabilities:**
- 🔍 **Real-time Monitoring** — Track agent sessions, events, and resource usage across all nodes
- 🎮 **Unified Control** — Manage sessions via Discord commands, HTTP API, or CLI
- 💰 **Cost Tracking** — Aggregate and analyze LLM API costs from multiple providers
- 🤖 **Auto-Intervention** — Automated session recovery, restart, and cost management
- 📊 **Event Streaming** — Real-time event pipeline with filtering and persistence
- 🔒 **Security & Audit** — TLS support, origin validation, and complete audit trail

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Control Interfaces                        │
│  Discord Bot  │  HTTP API  │  CLI (halctl)  │  Prometheus   │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                  hal-supervisor (Central Hub)                │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Session    │  │    Event     │  │   Command    │      │
│  │   Tracker    │  │   Pipeline   │  │  Dispatcher  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │     Cost     │  │    Policy    │  │     Node     │      │
│  │  Aggregator  │  │    Engine    │  │   Registry   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│                                                               │
│  Storage: SQLite (sessions, events, costs, audit)           │
└────────────────────────┬────────────────────────────────────┘
                         │ WebSocket (bidirectional)
          ┌──────────────┼──────────────┐
          │              │              │
┌─────────▼─────────┐ ┌─▼──────────────▼┐ ┌─────────────────┐
│   hal-agent       │ │   hal-agent     │ │   hal-agent     │
│   (Node A)        │ │   (Node B)      │ │   (Node C)      │
│                   │ │                 │ │                 │
│  opencode serve   │ │  opencode serve │ │  opencode serve │
│  ├─ project-1     │ │  ├─ project-3   │ │  ├─ project-5   │
│  ├─ project-2     │ │  └─ project-4   │ │  └─ project-6   │
│  └─ project-3     │ │                 │ │                 │
└───────────────────┘ └─────────────────┘ └─────────────────┘
```

### Component Responsibilities

#### Supervisor (Central Hub)
- **Session Tracker**: Maintains state of all agent sessions across nodes
- **Event Pipeline**: Processes, orders, and persists events with deduplication
- **Command Dispatcher**: Routes commands to agents with idempotency guarantees
- **Cost Aggregator**: Polls LLM provider APIs and aggregates usage data
- **Policy Engine**: Executes auto-intervention rules (resume, restart, kill)
- **Node Registry**: Tracks agent health via heartbeat monitoring

#### Agent (Node Process)
- **WebSocket Client**: Maintains persistent connection to supervisor with auto-reconnect
- **opencode Adapter**: Wraps opencode SDK for session management
- **Event Forwarder**: Streams session events to supervisor in real-time
- **Environment Checker**: Validates runtime, tools, and project requirements
- **Auto-Provisioner**: Creates missing files and configurations safely

#### CLI Tool (halctl)
- Remote session management
- Node status queries
- Cost reporting
- Environment validation

---

## Quick Start

### Installation

```bash
# Clone repository
git clone https://github.com/bldg-7/hal-o-swarm.git
cd hal-o-swarm

# Install all components (supervisor + agent + halctl)
sudo ./deploy/install.sh --all

# Or install individually
sudo ./deploy/install.sh --supervisor  # Central hub only
sudo ./deploy/install.sh --agent       # Agent only
sudo ./deploy/install.sh --halctl      # CLI tool only
```

### Configuration

#### Supervisor Configuration (`/etc/hal-o-swarm/supervisor.config.json`)

```json
{
  "server": {
    "port": 8420,
    "http_port": 8421,
    "auth_token": "your-shared-secret-token-here"
  },
  "database": {
    "path": "/var/lib/hal-o-swarm/supervisor.db"
  },
  "channels": {
    "discord": {
      "bot_token": "your-discord-bot-token",
      "guild_id": "your-guild-id"
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": "sk-ant-..."
      },
      "openai": {
        "org_api_key": "sk-..."
      }
    }
  },
  "policies": {
    "resume_on_idle": {
      "enabled": true,
      "idle_threshold_seconds": 300,
      "max_retries": 3
    }
  }
}
```

#### Agent Configuration (`/etc/hal-o-swarm/agent.config.json`)

```json
{
  "supervisor_url": "ws://supervisor-host:8420",
  "auth_token": "your-shared-secret-token-here",
  "opencode_port": 4096,
  "projects": [
    {
      "name": "my-project",
      "directory": "/home/user/my-project"
    }
  ]
}
```

### Start Services

```bash
# Start supervisor
sudo systemctl start hal-supervisor
sudo systemctl enable hal-supervisor

# Start agent on each node
sudo systemctl start hal-agent
sudo systemctl enable hal-agent

# Verify status
sudo systemctl status hal-supervisor
sudo systemctl status hal-agent
```

### Verify Installation

```bash
# Check nodes are connected
halctl nodes list

# Check sessions
halctl sessions list

# Check health
curl http://localhost:8421/healthz
curl http://localhost:8421/readyz
```

---

## Usage

### Discord Commands

Once configured, use these slash commands in Discord:

```
/status <project>          # Get session status
/nodes                     # List all connected nodes
/logs <session-id>         # View session logs
/resume <project>          # Resume idle session
/restart <session-id>      # Restart session
/kill <session-id>         # Terminate session
/start <project>           # Create new session
/cost [today|week|month]   # View cost report
```

### CLI Tool (halctl)

```bash
# Session management
halctl sessions list
halctl sessions get <session-id>
halctl sessions logs <session-id> --limit 100

# Node management
halctl nodes list
halctl nodes get <node-id>

# Cost reporting
halctl cost today
halctl cost week
halctl cost month

# Environment management
halctl env status <project>
halctl env check <project>
halctl env provision <project>
```

### HTTP API

```bash
# List sessions
curl -H "Authorization: Bearer <token>" \
  http://localhost:8421/api/v1/sessions

# Get session details
curl -H "Authorization: Bearer <token>" \
  http://localhost:8421/api/v1/sessions/<session-id>

# List nodes
curl -H "Authorization: Bearer <token>" \
  http://localhost:8421/api/v1/nodes

# Execute command
curl -X POST -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"type":"restart_session","target":"<session-id>"}' \
  http://localhost:8421/api/v1/commands

# Get cost report
curl -H "Authorization: Bearer <token>" \
  http://localhost:8421/api/v1/cost?period=week
```

---

## Features

### Session Management

**Track and control LLM agent sessions across your infrastructure:**

- **Real-time State Tracking**: Monitor session status (running, idle, error, completed)
- **Remote Intervention**: Restart, kill, or inject prompts into running sessions
- **Event History**: Complete audit trail of all session events
- **Multi-Project Support**: Each agent can manage multiple projects simultaneously

### Event Pipeline

**Reliable event streaming with ordering guarantees:**

- **Sequence Tracking**: Per-agent monotonic sequence numbers prevent event loss
- **Deduplication**: LRU cache prevents duplicate event processing
- **Gap Detection**: Automatic detection and replay of missing events
- **Persistence**: SQLite storage with efficient indexing for queries

### Cost Tracking

**Comprehensive cost visibility across LLM providers:**

- **Multi-Provider Support**: Anthropic, OpenAI, and extensible to others
- **Daily Bucketing**: Costs aggregated by date, provider, and model
- **Project Attribution**: Track costs per project for chargeback
- **Trend Analysis**: Historical cost data for budgeting and forecasting

### Auto-Intervention Policies

**Automated session management based on configurable rules:**

- **Resume on Idle**: Automatically resume sessions idle beyond threshold
- **Restart on Compaction**: Restart sessions when context window fills
- **Kill on Cost**: Terminate sessions exceeding cost limits
- **Retry Limits**: Configurable retry caps prevent infinite loops
- **Reset Windows**: Time-based retry counter resets for transient failures

### Security

**Production-grade security features:**

- **TLS Support**: Optional WSS encryption for WebSocket connections
- **Origin Validation**: Allowlist-based origin checking
- **Token Authentication**: Shared secret authentication for all connections
- **Audit Logging**: Complete command audit trail with actor tracking
- **Secret Sanitization**: Automatic redaction of secrets in logs

### Observability

**Built-in monitoring and diagnostics:**

- **Prometheus Metrics**: Counters, gauges, and histograms for all operations
- **Health Endpoints**: Liveness and readiness probes for orchestration
- **Structured Logging**: JSON logs with correlation IDs
- **Correlation Tracking**: Request tracing across components

---

## Monitoring

### Health Checks

```bash
# Liveness probe (always returns 200 if running)
curl http://localhost:8421/healthz

# Readiness probe (checks all components)
curl http://localhost:8421/readyz
# Returns: {"status":"healthy","components":{"database":"ok",...}}
```

### Prometheus Metrics

```bash
# Scrape metrics endpoint
curl http://localhost:8421/metrics

# Key metrics:
# - hal_o_swarm_commands_total{type,status}
# - hal_o_swarm_events_total{type}
# - hal_o_swarm_connections_active
# - hal_o_swarm_sessions_active{status}
# - hal_o_swarm_nodes_online
# - hal_o_swarm_command_duration_seconds{type}
```

### Logs

```bash
# Supervisor logs
sudo journalctl -u hal-supervisor -f

# Agent logs
sudo journalctl -u hal-agent -f

# Filter by correlation ID
sudo journalctl -u hal-supervisor | grep "correlation_id=abc123"
```

---

## Configuration Reference

### Supervisor Configuration

| Section | Key | Description | Default |
|---------|-----|-------------|---------|
| `server` | `port` | WebSocket server port | 8420 |
| `server` | `http_port` | HTTP API port | 8421 |
| `server` | `auth_token` | Shared secret for authentication | (required) |
| `server` | `heartbeat_interval_sec` | Heartbeat interval | 30 |
| `server` | `heartbeat_timeout_count` | Missed heartbeats before offline | 3 |
| `database` | `path` | SQLite database path | `/var/lib/hal-o-swarm/supervisor.db` |
| `security.tls` | `enabled` | Enable TLS/WSS | false |
| `security.tls` | `cert_path` | TLS certificate path | - |
| `security.tls` | `key_path` | TLS private key path | - |
| `security` | `origin_allowlist` | Allowed WebSocket origins | `["*"]` |
| `policies.resume_on_idle` | `enabled` | Enable auto-resume | false |
| `policies.resume_on_idle` | `idle_threshold_seconds` | Idle threshold | 300 |
| `policies.resume_on_idle` | `max_retries` | Max retry attempts | 3 |

### Agent Configuration

| Section | Key | Description | Default |
|---------|-----|-------------|---------|
| - | `supervisor_url` | Supervisor WebSocket URL | (required) |
| - | `auth_token` | Shared secret for authentication | (required) |
| - | `opencode_port` | opencode serve port | 4096 |
| `projects[]` | `name` | Project name | (required) |
| `projects[]` | `directory` | Project directory path | (required) |

---

## Documentation

- **[DEPLOYMENT.md](docs/DEPLOYMENT.md)** — Complete deployment guide with production best practices
- **[RUNBOOK.md](docs/RUNBOOK.md)** — Incident response procedures and troubleshooting
- **[ROLLBACK.md](docs/ROLLBACK.md)** — Safe rollback and recovery procedures
- **[DEVELOPMENT.md](docs/DEVELOPMENT.md)** — Development guide for contributors
- **[Product Spec](Hal-o-swarm_Product_Spec_v1.1.md)** — Detailed system specification

---

## Performance

### Recommended Hardware

| Component | CPU | Memory | Disk | Network |
|-----------|-----|--------|------|---------|
| Supervisor | 2+ cores | 2GB+ | 10GB+ SSD | 100Mbps+ |
| Agent | 2+ cores | 4GB+ | 20GB+ SSD | 100Mbps+ |

### Scalability

- **Agents**: Tested with 50+ concurrent agents
- **Sessions**: 100+ concurrent sessions per supervisor
- **Events**: 10,000+ events/second throughput
- **Database**: SQLite handles millions of events efficiently

### Optimization

```bash
# Archive old events (run monthly)
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"

# Vacuum database (run after archival)
sqlite3 /var/lib/hal-o-swarm/supervisor.db "VACUUM;"

# Analyze for query optimization
sqlite3 /var/lib/hal-o-swarm/supervisor.db "ANALYZE;"
```

---

## Troubleshooting

### Common Issues

#### Supervisor Won't Start

```bash
# Check configuration
sudo journalctl -u hal-supervisor -n 50

# Verify port availability
sudo lsof -i :8420

# Test configuration
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate
```

#### Agent Can't Connect

```bash
# Verify supervisor is running
sudo systemctl status hal-supervisor

# Check auth token matches
grep auth_token /etc/hal-o-swarm/supervisor.config.json
grep auth_token /etc/hal-o-swarm/agent.config.json

# Test network connectivity
curl -v ws://supervisor-host:8420
```

#### High Memory Usage

```bash
# Check current usage
ps aux | grep hal-supervisor

# Increase systemd memory limit
sudo systemctl edit hal-supervisor
# Add: MemoryMax=4G

# Archive old data
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-7 days');"
```

See [RUNBOOK.md](docs/RUNBOOK.md) for comprehensive troubleshooting procedures.

---

## Contributing

We welcome contributions! Please see [DEVELOPMENT.md](docs/DEVELOPMENT.md) for:

- Development environment setup
- Code style guidelines
- Testing requirements
- Pull request process

---

## License

MIT License - see [LICENSE](LICENSE) file for details.

---

## Support

- **Documentation**: [docs/](docs/) directory
- **Issues**: [GitHub Issues](https://github.com/bldg-7/hal-o-swarm/issues)
- **Discord**: #hal-o-swarm channel
- **Email**: support@example.com

---

## Changelog

### Version 1.0.0 (February 2026)

**Initial Release**

- Supervisor with session tracking and event routing
- Agent with WebSocket reconnection and event forwarding
- Discord slash command integration (9 commands)
- HTTP API with bearer token authentication
- Cost aggregation from Anthropic and OpenAI
- Auto-intervention policy engine
- CLI tool (halctl) for remote management
- TLS support and security hardening
- Prometheus metrics and health checks
- Comprehensive deployment documentation

---

**Made with ❤️ by the HAL-O-SWARM team**
