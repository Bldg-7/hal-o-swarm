#!/bin/bash
set -euo pipefail

REPO="Bldg-7/hal-o-swarm"
TAG="latest"

INSTALL_SUPERVISOR=false
INSTALL_AGENT=false
INSTALL_HALCTL=false

INSTALL_PREFIX="/usr/local"
CONFIG_DIR="/etc/hal-o-swarm"
DATA_DIR="/var/lib/hal-o-swarm"
LOG_DIR="/var/log/hal-o-swarm"

DRY_RUN=false
TMPDIR=""

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
  echo -e "${BLUE}[INFO]${NC} $*"
}

log_ok() {
  echo -e "${GREEN}[OK]${NC} $*"
}

log_warn() {
  echo -e "${YELLOW}[WARN]${NC} $*"
}

log_err() {
  echo -e "${RED}[ERROR]${NC} $*" >&2
}

usage() {
  cat <<EOF
HAL-O-SWARM Release Installer

Usage: sudo ./deploy/install-release.sh [OPTIONS]

Options:
  --supervisor          Install hal-supervisor
  --agent               Install hal-agent
  --halctl              Install halctl
  --all                 Install all components
  --tag TAG             Release tag (default: latest)
  --repo OWNER/REPO     GitHub repo (default: Bldg-7/hal-o-swarm)
  --prefix PATH         Install prefix for binaries (default: /usr/local)
  --config-dir PATH     Config directory (default: /etc/hal-o-swarm)
  --data-dir PATH       Data directory (default: /var/lib/hal-o-swarm)
  --log-dir PATH        Log directory (default: /var/log/hal-o-swarm)
  --dry-run             Print actions without making changes
  --help                Show this message

Examples:
  sudo ./deploy/install-release.sh --all
  sudo ./deploy/install-release.sh --supervisor --tag v1.0.0
EOF
}

require_root() {
  if [[ "$EUID" -ne 0 ]]; then
    log_err "This script must run as root"
    exit 1
  fi
}

require_linux_systemd() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    log_err "This installer supports Linux only"
    exit 1
  fi
  if ! command -v systemctl >/dev/null 2>&1; then
    log_err "systemctl not found; systemd is required"
    exit 1
  fi
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)
      echo "amd64"
      ;;
    aarch64|arm64)
      echo "arm64"
      ;;
    *)
      log_err "Unsupported architecture: $arch"
      exit 1
      ;;
  esac
}

resolve_tag() {
  if [[ "$TAG" != "latest" ]]; then
    echo "$TAG"
    return
  fi

  local effective
  effective="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  if [[ "$effective" != *"/tag/"* ]]; then
    log_err "Failed to resolve latest release tag"
    exit 1
  fi
  echo "${effective##*/}"
}

prepare_users_dirs() {
  if [[ "$INSTALL_SUPERVISOR" == "true" ]] && ! id hal-supervisor >/dev/null 2>&1; then
    if [[ "$DRY_RUN" == "true" ]]; then
      log_info "[dry-run] useradd hal-supervisor"
    else
      useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin hal-supervisor
      log_ok "Created user hal-supervisor"
    fi
  fi

  if [[ "$INSTALL_AGENT" == "true" ]] && ! id hal-agent >/dev/null 2>&1; then
    if [[ "$DRY_RUN" == "true" ]]; then
      log_info "[dry-run] useradd hal-agent"
    else
      useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin hal-agent
      log_ok "Created user hal-agent"
    fi
  fi

  if [[ "$DRY_RUN" == "true" ]]; then
    log_info "[dry-run] mkdir -p $CONFIG_DIR $DATA_DIR $LOG_DIR"
    return
  fi

  mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
  chmod 755 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"

  if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
    chown -R hal-supervisor:hal-supervisor "$DATA_DIR" "$LOG_DIR"
  fi
  if [[ "$INSTALL_AGENT" == "true" ]]; then
    chown -R hal-agent:hal-agent "$DATA_DIR" "$LOG_DIR"
  fi

  log_ok "Prepared users and directories"
}

install_configs() {
  if [[ "$DRY_RUN" == "true" ]]; then
    log_info "[dry-run] install config templates into $CONFIG_DIR"
    return
  fi

  if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
    if [[ ! -f "$CONFIG_DIR/supervisor.config.json" ]]; then
      cat >"$CONFIG_DIR/supervisor.config.json" <<'EOF'
{
  "server": {
    "port": 8420,
    "http_port": 8421,
    "auth_token": "CHANGE_ME_SHARED_TOKEN",
    "heartbeat_interval_sec": 30,
    "heartbeat_timeout_count": 3
  },
  "database": {
    "path": "/var/lib/hal-o-swarm/supervisor.db"
  },
  "channels": {
    "discord": {
      "bot_token": "",
      "guild_id": "",
      "channels": {}
    },
    "slack": {
      "bot_token": "",
      "channels": {}
    },
    "n8n": {
      "webhook_url": ""
    }
  },
  "cost": {
    "poll_interval_minutes": 60,
    "providers": {
      "anthropic": {
        "admin_api_key": ""
      },
      "openai": {
        "org_api_key": ""
      }
    }
  },
  "routes": [],
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
    },
    "kill_on_cost": {
      "enabled": false,
      "cost_threshold_usd": 10,
      "max_retries": 1,
      "retry_reset_seconds": 86400
    },
    "check_interval_seconds": 30
  },
  "dependencies": {},
  "security": {
    "tls": {
      "enabled": false,
      "cert_path": "",
      "key_path": ""
    },
    "origin_allowlist": [],
    "token_rotation": {
      "enabled": false,
      "check_interval_seconds": 300
    },
    "audit": {
      "enabled": true,
      "retention_days": 90
    }
  },
  "credentials": {
    "version": 1,
    "defaults": {
      "env": {}
    },
    "agents": {}
  }
}
EOF
      chmod 600 "$CONFIG_DIR/supervisor.config.json"
      log_ok "Installed supervisor config template"
    else
      log_warn "Supervisor config already exists, keeping current file"
    fi

    if [[ ! -f "$CONFIG_DIR/supervisor.env" ]]; then
      cat >"$CONFIG_DIR/supervisor.env" <<'EOF'
# HAL-O-SWARM Supervisor Environment Variables
# LOG_LEVEL=info
EOF
      chmod 600 "$CONFIG_DIR/supervisor.env"
      log_ok "Installed supervisor env file"
    fi
  fi

  if [[ "$INSTALL_AGENT" == "true" ]]; then
    if [[ ! -f "$CONFIG_DIR/agent.config.json" ]]; then
      cat >"$CONFIG_DIR/agent.config.json" <<'EOF'
{
  "supervisor_url": "ws://127.0.0.1:8420/ws/agent",
  "auth_token": "CHANGE_ME_SHARED_TOKEN",
  "opencode_port": 4096,
  "auth_report_interval_sec": 30,
  "projects": [
    {
      "name": "example-project",
      "directory": "/home/user/example-project"
    }
  ]
}
EOF
      chmod 600 "$CONFIG_DIR/agent.config.json"
      log_ok "Installed agent config template"
    else
      log_warn "Agent config already exists, keeping current file"
    fi

    if [[ ! -f "$CONFIG_DIR/agent.env" ]]; then
      cat >"$CONFIG_DIR/agent.env" <<'EOF'
# HAL-O-SWARM Agent Environment Variables
# LOG_LEVEL=info
EOF
      chmod 600 "$CONFIG_DIR/agent.env"
      log_ok "Installed agent env file"
    fi
  fi
}

install_systemd_units() {
  if [[ "$DRY_RUN" == "true" ]]; then
    log_info "[dry-run] install systemd units"
    return
  fi

  if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
    cat >/etc/systemd/system/hal-supervisor.service <<'EOF'
[Unit]
Description=HAL-O-SWARM Supervisor - Distributed LLM Agent Control Plane
Documentation=https://github.com/bldg-7/hal-o-swarm/blob/main/docs/DEPLOYMENT.md
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=hal-supervisor
Group=hal-supervisor
WorkingDirectory=/opt/hal-o-swarm
EnvironmentFile=/etc/hal-o-swarm/supervisor.env
ExecStart=/usr/local/bin/hal-supervisor --config /etc/hal-o-swarm/supervisor.config.json
Restart=on-failure
RestartSec=10
StartLimitInterval=300
StartLimitBurst=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/hal-o-swarm /var/log/hal-o-swarm
LimitNOFILE=65536
LimitNPROC=4096
MemoryLimit=2G
CPUQuota=80%
StandardOutput=journal
StandardError=journal
SyslogIdentifier=hal-supervisor
TimeoutStopSec=30
KillMode=mixed
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
EOF
    log_ok "Installed hal-supervisor.service"
  fi

  if [[ "$INSTALL_AGENT" == "true" ]]; then
    cat >/etc/systemd/system/hal-agent.service <<'EOF'
[Unit]
Description=HAL-O-SWARM Agent - Local LLM Session Manager
Documentation=https://github.com/bldg-7/hal-o-swarm/blob/main/docs/DEPLOYMENT.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=hal-agent
Group=hal-agent
WorkingDirectory=/opt/hal-o-swarm
EnvironmentFile=/etc/hal-o-swarm/agent.env
ExecStart=/usr/local/bin/hal-agent --config /etc/hal-o-swarm/agent.config.json
Restart=on-failure
RestartSec=10
StartLimitInterval=300
StartLimitBurst=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/hal-o-swarm /var/log/hal-o-swarm /home
LimitNOFILE=65536
LimitNPROC=4096
MemoryLimit=4G
CPUQuota=90%
StandardOutput=journal
StandardError=journal
SyslogIdentifier=hal-agent
TimeoutStopSec=30
KillMode=mixed
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
EOF
    log_ok "Installed hal-agent.service"
  fi

  systemctl daemon-reload
}

enable_services() {
  if [[ "$DRY_RUN" == "true" ]]; then
    log_info "[dry-run] enable hal services"
    return
  fi

  if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
    systemctl enable hal-supervisor >/dev/null 2>&1 || true
  fi
  if [[ "$INSTALL_AGENT" == "true" ]]; then
    systemctl enable hal-agent >/dev/null 2>&1 || true
  fi
  log_ok "Enabled systemd units"
}

cleanup() {
  if [[ -n "$TMPDIR" && -d "$TMPDIR" ]]; then
    rm -rf "$TMPDIR"
  fi
}

main() {
  trap cleanup EXIT

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --supervisor)
        INSTALL_SUPERVISOR=true
        shift
        ;;
      --agent)
        INSTALL_AGENT=true
        shift
        ;;
      --halctl)
        INSTALL_HALCTL=true
        shift
        ;;
      --all)
        INSTALL_SUPERVISOR=true
        INSTALL_AGENT=true
        INSTALL_HALCTL=true
        shift
        ;;
      --tag)
        TAG="$2"
        shift 2
        ;;
      --repo)
        REPO="$2"
        shift 2
        ;;
      --prefix)
        INSTALL_PREFIX="$2"
        shift 2
        ;;
      --config-dir)
        CONFIG_DIR="$2"
        shift 2
        ;;
      --data-dir)
        DATA_DIR="$2"
        shift 2
        ;;
      --log-dir)
        LOG_DIR="$2"
        shift 2
        ;;
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --help)
        usage
        exit 0
        ;;
      *)
        log_err "Unknown option: $1"
        usage
        exit 1
        ;;
    esac
  done

  if [[ "$INSTALL_SUPERVISOR" == "false" && "$INSTALL_AGENT" == "false" && "$INSTALL_HALCTL" == "false" ]]; then
    log_err "No components selected. Use --supervisor, --agent, --halctl, or --all"
    usage
    exit 1
  fi

  require_root
  require_linux_systemd

  if ! command -v curl >/dev/null 2>&1; then
    log_err "curl is required"
    exit 1
  fi

  local arch
  arch="$(detect_arch)"

  local resolved_tag
  resolved_tag="$(resolve_tag)"

  local asset
  asset="hal-o-swarm_${resolved_tag}_linux_${arch}.tar.gz"

  local url
  url="https://github.com/${REPO}/releases/download/${resolved_tag}/${asset}"

  log_info "Installing from release ${resolved_tag} (${arch})"
  log_info "Asset URL: ${url}"

  prepare_users_dirs

  if [[ "$DRY_RUN" != "true" ]]; then
    TMPDIR="$(mktemp -d)"

    curl -fL "$url" -o "$TMPDIR/$asset"
    tar -xzf "$TMPDIR/$asset" -C "$TMPDIR"

    mkdir -p "$INSTALL_PREFIX/bin"

    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
      install -m 0755 "$TMPDIR/hal-supervisor" "$INSTALL_PREFIX/bin/hal-supervisor"
      log_ok "Installed hal-supervisor"
    fi
    if [[ "$INSTALL_AGENT" == "true" ]]; then
      install -m 0755 "$TMPDIR/hal-agent" "$INSTALL_PREFIX/bin/hal-agent"
      log_ok "Installed hal-agent"
    fi
    if [[ "$INSTALL_HALCTL" == "true" ]]; then
      install -m 0755 "$TMPDIR/halctl" "$INSTALL_PREFIX/bin/halctl"
      log_ok "Installed halctl"
    fi
  else
    log_info "[dry-run] download $url"
    log_info "[dry-run] install binaries to $INSTALL_PREFIX/bin"
  fi

  install_configs
  install_systemd_units
  enable_services

  echo
  log_ok "Release install complete"
  echo "Next steps:"
  if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
    echo "  - Edit: $CONFIG_DIR/supervisor.config.json"
    echo "  - Start: sudo systemctl start hal-supervisor"
  fi
  if [[ "$INSTALL_AGENT" == "true" ]]; then
    echo "  - Edit: $CONFIG_DIR/agent.config.json"
    echo "  - Start: sudo systemctl start hal-agent"
  fi
}

main "$@"
