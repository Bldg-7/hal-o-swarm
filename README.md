# HAL-O-SWARM

**Distributed LLM Agent Supervisor** — A real-time monitoring and control plane for autonomous LLM coding agents running across multiple servers.

![Version](https://img.shields.io/badge/version-1.0.0-blue)
![License](https://img.shields.io/badge/license-MIT-green)
![Status](https://img.shields.io/badge/status-production-brightgreen)

## Overview

HAL-O-SWARM is a distributed supervisor daemon that provides:

- **Real-time Monitoring**: Track LLM agent sessions across multiple servers
- **Unified Control Plane**: Remote intervention via Discord/Slack commands
- **Cost Tracking**: Aggregate LLM API costs from Anthropic, OpenAI, and other providers
- **Auto-Intervention**: Automatic session recovery, restart, and cost management policies
- **Event Streaming**: Real-time event pipeline with filtering and routing
- **Audit Logging**: Complete audit trail for compliance and debugging

### Architecture

```
┌─────────────────────────────────────────┐
│    Discord / Slack (User Interface)     │
│  Commands in, Alerts out, Cost reports  │
└──────────────────┬──────────────────────┘
                   │ Webhook / Bot API
┌──────────────────▼──────────────────────┐
│   hal-supervisor (Central Daemon)       │
│  - Session tracking                     │
│  - Event routing                        │
│  - Cost aggregation                     │
│  - Command dispatch                     │
│  - Policy engine                        │
└──────────────────┬──────────────────────┘
                   │ WebSocket (outbound)
        ┌──────────┴──────────┐
        ▼                     ▼
┌──────────────────┐  ┌──────────────────┐
│  hal-agent       │  │  hal-agent       │
│  (Node A)        │  │  (Node B)        │
│  opencode serve  │  │  opencode serve  │
│  [P1] [P3] [P4]  │  │  [P6] [P7]       │
└──────────────────┘  └──────────────────┘
```

## Quick Start

### Installation

```bash
# Install all components from GitHub release binaries (no local build, no clone)
curl -fsSL https://raw.githubusercontent.com/Bldg-7/hal-o-swarm/main/deploy/install-release.sh | sudo bash -s -- --all

# Edit configuration
sudo nano /etc/hal-o-swarm/supervisor.config.json
sudo nano /etc/hal-o-swarm/agent.config.json

# Start services
sudo systemctl start hal-supervisor
sudo systemctl start hal-agent

# Enable on boot
sudo systemctl enable hal-supervisor
sudo systemctl enable hal-agent
```

### Verify Installation

```bash
# Check supervisor status
sudo systemctl status hal-supervisor

# Check agent status
sudo systemctl status hal-agent

# List nodes
halctl --supervisor-url ws://localhost:8420 --auth-token <token> nodes list

# List sessions
halctl --supervisor-url ws://localhost:8420 --auth-token <token> sessions list
```

## Documentation

- **[DEPLOYMENT.md](docs/DEPLOYMENT.md)** - Complete deployment guide with configuration options
- **[RUNBOOK.md](docs/RUNBOOK.md)** - Incident response procedures for common issues
- **[ROLLBACK.md](docs/ROLLBACK.md)** - Safe rollback and recovery procedures
- **[Product Spec](Hal-o-swarm_Product_Spec_v1.1.md)** - Detailed system specification

## Features

### Session Management

- Create, monitor, and control LLM agent sessions
- Track session state: running, idle, compacted, error, completed
- View session logs and event history
- Remote session intervention (restart, kill, inject prompt)

### Event Pipeline

- Real-time event streaming from agents
- Event filtering and routing
- Event persistence with SQLite
- Event replay and audit trail

### Cost Tracking

- Aggregate costs from multiple LLM providers
- Daily cost bucketing by provider and model
- Project-level cost visibility
- Cost-based auto-intervention policies

### Auto-Intervention Policies

- **Resume on Idle**: Automatically resume idle sessions
- **Restart on Compaction**: Restart sessions when context window fills
- **Kill on Cost**: Terminate sessions exceeding cost threshold
- Configurable retry limits and reset windows

### Discord Integration

- Slash commands for session management
- Real-time alerts and notifications
- Cost reports and status queries
- Command audit logging

### HTTP API

- RESTful API for programmatic access
- Bearer token authentication
- JSON response envelopes
- Comprehensive error handling

### CLI Tool (halctl)

```bash
# List nodes
halctl nodes list

# Get node details
halctl nodes get <node-id>

# List sessions
halctl sessions list

# Get session details
halctl sessions get <session-id>

# View session logs
halctl sessions logs <session-id>

# Get cost report
halctl cost today
halctl cost week
halctl cost month

# Check environment
halctl env status <project>

# Provision environment
halctl env provision <project>
```

## Configuration

### Supervisor Configuration

```json
{
  "server": {
    "port": 8420,
    "http_port": 8421,
    "auth_token": "your-shared-secret-here",
    "heartbeat_interval_sec": 30,
    "heartbeat_timeout_count": 3
  },
  "channels": {
    "discord": {
      "bot_token": "your-discord-bot-token",
      "guild_id": "your-guild-id",
      "channels": {
        "alerts": "channel-id-for-alerts",
        "dev-log": "channel-id-for-dev-log"
      }
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": "your-anthropic-admin-api-key"
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

### Agent Configuration

```json
{
  "supervisor_url": "ws://supervisor-host:8420",
  "auth_token": "your-shared-secret-here",
  "opencode_port": 4096,
  "projects": [
    {
      "name": "project-1",
      "directory": "/home/user/project-1"
    }
  ]
}
```

## Development

### Project Structure

```
hal-o-swarm/
├── cmd/
│   ├── supervisor/      # Supervisor entry point
│   ├── agent/           # Agent entry point
│   └── halctl/          # CLI tool entry point
├── internal/
│   ├── supervisor/      # Supervisor implementation
│   │   ├── registry.go  # Node registry
│   │   ├── tracker.go   # Session tracker
│   │   ├── router.go    # Event router
│   │   ├── cost.go      # Cost aggregator
│   │   ├── commands.go  # Command dispatcher
│   │   └── policy.go    # Policy engine
│   ├── agent/           # Agent implementation
│   │   ├── proxy.go     # Session proxy
│   │   ├── wsclient.go  # WebSocket client
│   │   └── forwarder.go # Event forwarder
│   ├── halctl/          # CLI implementation
│   ├── shared/          # Shared types and protocol
│   └── config/          # Configuration validation
├── deploy/
│   ├── systemd/         # Systemd units
│   ├── install.sh       # Installation script
│   └── uninstall.sh     # Uninstallation script
├── docs/
│   ├── DEPLOYMENT.md    # Deployment guide
│   ├── RUNBOOK.md       # Incident response
│   └── ROLLBACK.md      # Rollback procedures
└── integration/         # Integration tests
```

### Building from Source

```bash
# Build supervisor
go build -o supervisor ./cmd/supervisor

# Build agent
go build -o agent ./cmd/agent

# Build CLI tool
go build -o halctl ./cmd/halctl

# Run tests
go test -race ./...

# Run with coverage
go test -race -cover ./...
```

### Running Tests

```bash
# Run all tests
go test -race ./...

# Run specific package tests
go test -race ./internal/supervisor/...

# Run with verbose output
go test -race -v ./...

# Run integration tests
go test -race ./integration/...
```

## Monitoring

### Health Checks

```bash
# Liveness probe (always healthy if running)
curl http://localhost:8421/healthz

# Readiness probe (checks all components)
curl http://localhost:8421/readyz

# Prometheus metrics
curl http://localhost:8421/metrics
```

### Systemd Monitoring

```bash
# Check service status
sudo systemctl status hal-supervisor

# View logs
sudo journalctl -u hal-supervisor -f

# Check restart count
sudo systemctl show hal-supervisor -p NRestarts
```

### Key Metrics

- `hal_o_swarm_commands_total` - Total commands executed
- `hal_o_swarm_events_total` - Total events processed
- `hal_o_swarm_connections_active` - Current active connections
- `hal_o_swarm_sessions_active` - Current sessions by status
- `hal_o_swarm_nodes_online` - Current online nodes
- `hal_o_swarm_command_duration_seconds` - Command execution duration

## Troubleshooting

### Supervisor Won't Start

```bash
# Check configuration
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# View logs
sudo journalctl -u hal-supervisor -n 50

# Check port availability
sudo lsof -i :8420
```

### Agent Can't Connect

```bash
# Check supervisor is running
sudo systemctl status hal-supervisor

# Verify auth token matches
grep auth_token /etc/hal-o-swarm/supervisor.config.json
grep auth_token /etc/hal-o-swarm/agent.config.json

# Test connectivity
curl -v ws://supervisor-host:8420

# Check agent logs
sudo journalctl -u hal-agent -n 50
```

### High Memory Usage

```bash
# Check memory usage
ps aux | grep hal-supervisor

# Increase memory limit
sudo systemctl edit hal-supervisor
# Change MemoryLimit=2G to MemoryLimit=4G

# Archive old data
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "DELETE FROM events WHERE timestamp < datetime('now', '-30 days');"
```

See [RUNBOOK.md](docs/RUNBOOK.md) for more troubleshooting procedures.

## Security

### TLS Configuration

Enable TLS for production deployments:

```json
{
  "security": {
    "tls": {
      "enabled": true,
      "cert_path": "/etc/hal-o-swarm/cert.pem",
      "key_path": "/etc/hal-o-swarm/key.pem"
    }
  }
}
```

### Origin Allowlist

Restrict WebSocket connections to known origins:

```json
{
  "security": {
    "origin_allowlist": [
      "http://localhost:*",
      "https://internal.example.com:*"
    ]
  }
}
```

### Audit Logging

Enable audit logging for compliance:

```json
{
  "security": {
    "audit": {
      "enabled": true,
      "retention_days": 90
    }
  }
}
```

## Performance

### Recommended Hardware

| Component | CPU | Memory | Disk | Network |
|-----------|-----|--------|------|---------|
| Supervisor | 2+ cores | 2GB+ | 10GB+ | 100Mbps+ |
| Agent | 2+ cores | 4GB+ | 20GB+ | 100Mbps+ |

### Optimization Tips

- Archive old events regularly: `DELETE FROM events WHERE timestamp < datetime('now', '-30 days')`
- Vacuum database: `VACUUM;`
- Analyze database: `ANALYZE;`
- Monitor query performance: `sqlite3 /var/lib/hal-o-swarm/supervisor.db ".timer on"`

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

MIT License - see LICENSE file for details

## Support

- **Documentation**: See [docs/](docs/) directory
- **Issues**: GitHub Issues
- **Discord**: #hal-o-swarm channel
- **Email**: support@example.com

## Changelog

### Version 1.0.0 (February 2026)

- Initial release
- Supervisor with session tracking and event routing
- Agent with WebSocket reconnection and event forwarding
- Discord slash command integration
- HTTP API with bearer token auth
- Cost aggregation from Anthropic and OpenAI
- Auto-intervention policy engine
- CLI tool (halctl) for remote management
- Comprehensive deployment and runbook documentation

## Roadmap

- [ ] Slack integration
- [ ] Kubernetes deployment support
- [ ] Multi-region federation
- [ ] Advanced analytics and reporting
- [ ] Custom policy scripting
- [ ] Web UI dashboard

---

**Made with ❤️ by the HAL-O-SWARM team**
