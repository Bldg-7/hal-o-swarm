# HAL-O-SWARM Deployment Guide

**Version 1.0** | February 2026

This guide covers production deployment of HAL-O-SWARM, a distributed LLM agent supervisor system.

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Prerequisites](#prerequisites)
3. [Installation](#installation)
4. [Configuration](#configuration)
5. [Running Services](#running-services)
6. [Verification](#verification)
7. [Monitoring](#monitoring)
8. [Troubleshooting](#troubleshooting)

---

## Architecture Overview

HAL-O-SWARM follows a 2-tier hub-and-spoke architecture:

```
┌─────────────────────────────────────────┐
│    Discord / Slack (User Interface)     │
└──────────────────┬──────────────────────┘
                   │
┌──────────────────▼──────────────────────┐
│   hal-supervisor (Central Daemon)       │
│  - Session tracking                     │
│  - Event routing                        │
│  - Cost aggregation                     │
│  - Command dispatch                     │
└──────────────────┬──────────────────────┘
                   │
        ┌──────────┴──────────┐
        ▼                     ▼
┌──────────────────┐  ┌──────────────────┐
│  hal-agent       │  │  hal-agent       │
│  (Node A)        │  │  (Node B)        │
│  opencode serve  │  │  opencode serve  │
└──────────────────┘  └──────────────────┘
```

**Supervisor**: Central control plane running on a dedicated server or LXC container
**Agents**: Lightweight processes on each worker node, managing local opencode sessions

---

## Prerequisites

### System Requirements

- **OS**: Linux (Ubuntu 20.04+, Debian 11+, CentOS 8+, or equivalent)
- **systemd**: Required for service management
- **Go**: 1.21+ (if building from source)
- **Disk Space**: 
  - Supervisor: 2GB minimum
  - Agent: 4GB minimum (for session data)
- **Memory**:
  - Supervisor: 2GB minimum
  - Agent: 4GB minimum
- **Network**: Outbound HTTPS for LLM APIs (Anthropic, OpenAI, etc.)

### User Permissions

- Root access for installation
- Dedicated system users created during installation:
  - `hal-supervisor` (supervisor process)
  - `hal-agent` (agent process)

### Network Configuration

- **Supervisor**: Requires open port 8420 (WebSocket) and 8421 (HTTP API)
- **Agents**: Require outbound connectivity to supervisor
- **Firewall**: Configure to allow agent → supervisor connections

---

## Installation

### Quick Start (All Components)

```bash
# Clone repository
git clone https://github.com/bldg-7/hal-o-swarm.git
cd hal-o-swarm

# Install all components from GitHub release binaries (no local build)
sudo ./deploy/install-release.sh --all

# Edit configuration files
sudo nano /etc/hal-o-swarm/supervisor.config.json
sudo nano /etc/hal-o-swarm/agent.config.json

# Start services
sudo systemctl start hal-supervisor
sudo systemctl start hal-agent

# Enable on boot
sudo systemctl enable hal-supervisor
sudo systemctl enable hal-agent
```

### Component-Specific Installation

#### Supervisor Only

```bash
sudo ./deploy/install-release.sh --supervisor
```

Installs:
- `hal-supervisor` binary to `/usr/local/bin/`
- Systemd unit: `/etc/systemd/system/hal-supervisor.service`
- Configuration: `/etc/hal-o-swarm/supervisor.config.json`
- Data directory: `/var/lib/hal-o-swarm/`

#### Agent Only

```bash
sudo ./deploy/install-release.sh --agent
```

Installs:
- `hal-agent` binary to `/usr/local/bin/`
- Systemd unit: `/etc/systemd/system/hal-agent.service`
- Configuration: `/etc/hal-o-swarm/agent.config.json`
- Data directory: `/var/lib/hal-o-swarm/`

#### CLI Tool Only

```bash
sudo ./deploy/install-release.sh --halctl
```

Installs:
- `halctl` binary to `/usr/local/bin/`
- No systemd unit (CLI tool only)

### Custom Installation Paths

```bash
sudo ./deploy/install-release.sh --all \
  --prefix /opt/hal \
  --config-dir /etc/hal \
  --data-dir /data/hal \
  --log-dir /var/log/hal
```

### Dry Run (Preview Changes)

```bash
sudo ./deploy/install-release.sh --all --dry-run
```

Shows what would be installed without making changes.

---

## Configuration

### Supervisor Configuration

Edit `/etc/hal-o-swarm/supervisor.config.json`:

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
        "dev-log": "channel-id-for-dev-log",
        "build-log": "channel-id-for-build-log"
      }
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": "your-anthropic-admin-api-key"
      },
      "openai": {
        "org_api_key": "your-openai-org-api-key"
      }
    }
  },
  "policies": {
    "resume_on_idle": {
      "enabled": true,
      "idle_threshold_seconds": 300,
      "max_retries": 3,
      "retry_reset_seconds": 3600
    },
    "restart_on_compaction": {
      "enabled": true,
      "token_threshold": 180000,
      "max_retries": 2,
      "retry_reset_seconds": 3600
    }
  },
  "security": {
    "tls": {
      "enabled": false,
      "cert_path": "/path/to/cert.pem",
      "key_path": "/path/to/key.pem"
    },
    "origin_allowlist": [],
    "audit": {
      "enabled": true,
      "retention_days": 90
    }
  }
}
```

**Key Settings**:
- `auth_token`: Shared secret for agent authentication (32+ characters recommended)
- `heartbeat_interval_sec`: How often agents send heartbeats (30s default)
- `heartbeat_timeout_count`: Missed heartbeats before marking node offline (3 default)
- `http_port`: Set to 0 to disable HTTP API

### Agent Configuration

Edit `/etc/hal-o-swarm/agent.config.json`:

```json
{
  "supervisor_url": "ws://supervisor-host:8420",
  "auth_token": "your-shared-secret-here",
  "opencode_port": 4096,
  "projects": [
    {
      "name": "project-1",
      "directory": "/home/user/project-1"
    },
    {
      "name": "project-2",
      "directory": "/home/user/project-2"
    }
  ]
}
```

**Key Settings**:
- `supervisor_url`: WebSocket URL of supervisor (must match supervisor's listening address)
- `auth_token`: Must match supervisor's auth_token
- `opencode_port`: Port for local opencode serve (must be unique per agent)
- `projects`: List of projects this agent manages

### Environment Variables

Edit `/etc/hal-o-swarm/supervisor.env` and `/etc/hal-o-swarm/agent.env`:

```bash
# Logging level: debug, info, warn, error
LOG_LEVEL=info

# Rust logging (if using Rust components)
RUST_LOG=hal_o_swarm=debug

# Custom paths
CONFIG_PATH=/etc/hal-o-swarm/supervisor.config.json
DB_PATH=/var/lib/hal-o-swarm/supervisor.db
```

---

## Running Services

### Start Services

```bash
# Start supervisor
sudo systemctl start hal-supervisor

# Start agent
sudo systemctl start hal-agent

# Start both
sudo systemctl start hal-supervisor hal-agent
```

### Stop Services

```bash
# Stop supervisor (graceful shutdown, 30s timeout)
sudo systemctl stop hal-supervisor

# Stop agent
sudo systemctl stop hal-agent
```

### Restart Services

```bash
# Restart supervisor
sudo systemctl restart hal-supervisor

# Restart agent
sudo systemctl restart hal-agent
```

### Enable on Boot

```bash
# Enable supervisor to start on boot
sudo systemctl enable hal-supervisor

# Enable agent to start on boot
sudo systemctl enable hal-agent

# Check enabled services
sudo systemctl is-enabled hal-supervisor
sudo systemctl is-enabled hal-agent
```

### View Service Status

```bash
# Check supervisor status
sudo systemctl status hal-supervisor

# Check agent status
sudo systemctl status hal-agent

# View recent logs
sudo journalctl -u hal-supervisor -n 50
sudo journalctl -u hal-agent -n 50

# Follow logs in real-time
sudo journalctl -u hal-supervisor -f
sudo journalctl -u hal-agent -f
```

---

## Verification

### Health Checks

```bash
# Check supervisor health
curl http://localhost:8421/healthz

# Check supervisor readiness
curl http://localhost:8421/readyz

# View Prometheus metrics
curl http://localhost:8421/metrics
```

### CLI Verification

```bash
# List all nodes
halctl --supervisor-url ws://localhost:8420 --auth-token <token> nodes list

# List all sessions
halctl --supervisor-url ws://localhost:8420 --auth-token <token> sessions list

# Get cost report
halctl --supervisor-url ws://localhost:8420 --auth-token <token> cost today
```

### Log Verification

```bash
# Check supervisor startup
sudo journalctl -u hal-supervisor --since "5 minutes ago"

# Check agent startup
sudo journalctl -u hal-agent --since "5 minutes ago"

# Look for errors
sudo journalctl -u hal-supervisor -p err
sudo journalctl -u hal-agent -p err
```

---

## Monitoring

### Prometheus Metrics

HAL-O-SWARM exposes Prometheus metrics at `http://supervisor:8421/metrics`:

**Key Metrics**:
- `hal_o_swarm_commands_total{type, status}` - Total commands executed
- `hal_o_swarm_events_total{type}` - Total events processed
- `hal_o_swarm_connections_active` - Current active connections
- `hal_o_swarm_sessions_active{status}` - Current sessions by status
- `hal_o_swarm_nodes_online` - Current online nodes
- `hal_o_swarm_command_duration_seconds{type}` - Command execution duration

### Systemd Service Monitoring

```bash
# Monitor service restarts
sudo systemctl status hal-supervisor

# Check restart count
sudo systemctl show hal-supervisor -p NRestarts

# View service logs with timestamps
sudo journalctl -u hal-supervisor --output=short-iso
```

### Disk Space Monitoring

```bash
# Check data directory size
du -sh /var/lib/hal-o-swarm

# Check log directory size
du -sh /var/log/hal-o-swarm

# Monitor disk usage
df -h /var/lib/hal-o-swarm
```

### Network Monitoring

```bash
# Monitor WebSocket connections
sudo netstat -tlnp | grep hal-supervisor

# Monitor agent connections
sudo netstat -tlnp | grep hal-agent

# Check connection count
ss -tnp | grep hal-supervisor | wc -l
```

---

## Troubleshooting

### Service Won't Start

```bash
# Check service status
sudo systemctl status hal-supervisor

# View detailed logs
sudo journalctl -u hal-supervisor -n 100

# Check configuration validity
/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json --validate

# Check port availability
sudo lsof -i :8420
sudo lsof -i :8421
```

### Agent Can't Connect to Supervisor

```bash
# Verify supervisor is running
sudo systemctl status hal-supervisor

# Check supervisor is listening
sudo netstat -tlnp | grep 8420

# Test connectivity from agent
curl -v ws://supervisor-host:8420

# Check agent logs
sudo journalctl -u hal-agent -n 100

# Verify auth token matches
grep auth_token /etc/hal-o-swarm/supervisor.config.json
grep auth_token /etc/hal-o-swarm/agent.config.json
```

### High Memory Usage

```bash
# Check memory usage
ps aux | grep hal-supervisor
ps aux | grep hal-agent

# Check systemd limits
systemctl show hal-supervisor -p MemoryLimit
systemctl show hal-agent -p MemoryLimit

# Adjust limits in systemd unit
sudo systemctl edit hal-supervisor
# Change MemoryLimit=2G to desired value
sudo systemctl daemon-reload
sudo systemctl restart hal-supervisor
```

### Database Corruption

```bash
# Check database integrity
sqlite3 /var/lib/hal-o-swarm/supervisor.db "PRAGMA integrity_check;"

# Backup corrupted database
sudo cp /var/lib/hal-o-swarm/supervisor.db /var/lib/hal-o-swarm/supervisor.db.corrupt

# Restore from backup (if available)
sudo cp /var/lib/hal-o-swarm/supervisor.db.backup /var/lib/hal-o-swarm/supervisor.db

# Restart supervisor
sudo systemctl restart hal-supervisor
```

### Disk Space Issues

```bash
# Check disk usage
df -h /var/lib/hal-o-swarm

# Archive old logs
sudo journalctl --vacuum=30d

# Clean up old data (see ROLLBACK.md for safe procedures)
sudo find /var/lib/hal-o-swarm -mtime +90 -delete
```

---

## Security Considerations

### TLS Configuration

For production deployments, enable TLS:

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

Generate self-signed certificate:

```bash
openssl req -x509 -newkey rsa:4096 -keyout /etc/hal-o-swarm/key.pem \
  -out /etc/hal-o-swarm/cert.pem -days 365 -nodes
sudo chown hal-supervisor:hal-supervisor /etc/hal-o-swarm/{cert,key}.pem
sudo chmod 600 /etc/hal-o-swarm/{cert,key}.pem
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

View audit logs:

```bash
sqlite3 /var/lib/hal-o-swarm/supervisor.db \
  "SELECT * FROM audit_logs ORDER BY timestamp DESC LIMIT 100;"
```

---

## Next Steps

- See [RUNBOOK.md](RUNBOOK.md) for incident response procedures
- See [ROLLBACK.md](ROLLBACK.md) for rollback procedures
- See [README.md](../README.md) for architecture and development guide
