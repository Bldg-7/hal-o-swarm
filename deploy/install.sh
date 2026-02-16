#!/bin/bash
set -euo pipefail

# HAL-O-SWARM Installation Script
# Installs supervisor and/or agent with preflight checks and systemd integration

VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
INSTALL_SUPERVISOR=${INSTALL_SUPERVISOR:-false}
INSTALL_AGENT=${INSTALL_AGENT:-false}
INSTALL_HALCTL=${INSTALL_HALCTL:-false}
SKIP_PREFLIGHT=${SKIP_PREFLIGHT:-false}
DRY_RUN=${DRY_RUN:-false}

# Paths
INSTALL_PREFIX=${INSTALL_PREFIX:-/usr/local}
CONFIG_DIR=${CONFIG_DIR:-/etc/hal-o-swarm}
DATA_DIR=${DATA_DIR:-/var/lib/hal-o-swarm}
LOG_DIR=${LOG_DIR:-/var/log/hal-o-swarm}

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi
}

# Preflight checks
check_system_requirements() {
    log_info "Running preflight checks..."
    
    local failed=0
    
    # Check OS
    if [[ ! -f /etc/os-release ]]; then
        log_error "Cannot determine OS"
        return 1
    fi
    
    source /etc/os-release
    log_info "Detected OS: $PRETTY_NAME"
    
    # Check systemd
    if ! command -v systemctl &> /dev/null; then
        log_error "systemd not found. HAL-O-SWARM requires systemd"
        return 1
    fi
    log_success "systemd found"
    
    # Check Go (if building from source)
    if [[ ! -f "$PROJECT_ROOT/supervisor" ]] || [[ ! -f "$PROJECT_ROOT/agent" ]]; then
        if ! command -v go &> /dev/null; then
            log_error "Go not found. Required to build from source"
            return 1
        fi
        log_success "Go found: $(go version)"
    fi
    
    # Check disk space
    local available_space=$(df "$DATA_DIR" 2>/dev/null | awk 'NR==2 {print $4}')
    if [[ -z "$available_space" ]]; then
        available_space=$(df / | awk 'NR==2 {print $4}')
    fi
    
    if [[ $available_space -lt 1048576 ]]; then  # 1GB
        log_warn "Low disk space: $(numfmt --to=iec $((available_space * 1024)) 2>/dev/null || echo "$available_space KB")"
    else
        log_success "Disk space available: $(numfmt --to=iec $((available_space * 1024)) 2>/dev/null || echo "$available_space KB")"
    fi
    
    # Check network connectivity
    if ! timeout 2 bash -c "echo > /dev/tcp/8.8.8.8/53" 2>/dev/null; then
        log_warn "Network connectivity check failed (may be expected in isolated environments)"
    else
        log_success "Network connectivity verified"
    fi
    
    return $failed
}

# Build binaries
build_binaries() {
    log_info "Building binaries..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would build: supervisor, agent, halctl"
        return 0
    fi
    
    cd "$PROJECT_ROOT"
    
    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
        log_info "Building supervisor..."
        go build -o supervisor ./cmd/supervisor
        log_success "Supervisor built"
    fi
    
    if [[ "$INSTALL_AGENT" == "true" ]]; then
        log_info "Building agent..."
        go build -o agent ./cmd/agent
        log_success "Agent built"
    fi
    
    if [[ "$INSTALL_HALCTL" == "true" ]]; then
        log_info "Building halctl..."
        go build -o halctl ./cmd/halctl
        log_success "halctl built"
    fi
}

# Create system users and groups
create_users() {
    log_info "Creating system users and groups..."
    
    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
        if ! id "hal-supervisor" &>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would create user: hal-supervisor"
            else
                useradd --system --home /var/lib/hal-o-swarm --shell /usr/sbin/nologin hal-supervisor
                log_success "Created user: hal-supervisor"
            fi
        else
            log_info "User hal-supervisor already exists"
        fi
    fi
    
    if [[ "$INSTALL_AGENT" == "true" ]]; then
        if ! id "hal-agent" &>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would create user: hal-agent"
            else
                useradd --system --home /var/lib/hal-o-swarm --shell /usr/sbin/nologin hal-agent
                log_success "Created user: hal-agent"
            fi
        else
            log_info "User hal-agent already exists"
        fi
    fi
}

# Create directories
create_directories() {
    log_info "Creating directories..."
    
    local dirs=("$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR")
    
    for dir in "${dirs[@]}"; do
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would create directory: $dir"
        else
            mkdir -p "$dir"
            chmod 755 "$dir"
            log_success "Created directory: $dir"
        fi
    done
}

# Install binaries
install_binaries() {
    log_info "Installing binaries..."
    
    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install: $PROJECT_ROOT/supervisor -> $INSTALL_PREFIX/bin/hal-supervisor"
        else
            install -m 0755 "$PROJECT_ROOT/supervisor" "$INSTALL_PREFIX/bin/hal-supervisor"
            log_success "Installed: hal-supervisor"
        fi
    fi
    
    if [[ "$INSTALL_AGENT" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install: $PROJECT_ROOT/agent -> $INSTALL_PREFIX/bin/hal-agent"
        else
            install -m 0755 "$PROJECT_ROOT/agent" "$INSTALL_PREFIX/bin/hal-agent"
            log_success "Installed: hal-agent"
        fi
    fi
    
    if [[ "$INSTALL_HALCTL" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install: $PROJECT_ROOT/halctl -> $INSTALL_PREFIX/bin/halctl"
        else
            install -m 0755 "$PROJECT_ROOT/halctl" "$INSTALL_PREFIX/bin/halctl"
            log_success "Installed: halctl"
        fi
    fi
}

# Install configuration files
install_configs() {
    log_info "Installing configuration files..."
    
    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install supervisor config"
        else
            if [[ ! -f "$CONFIG_DIR/supervisor.config.json" ]]; then
                cp "$PROJECT_ROOT/supervisor.config.example.json" "$CONFIG_DIR/supervisor.config.json"
                chmod 600 "$CONFIG_DIR/supervisor.config.json"
                log_success "Installed supervisor config (EDIT REQUIRED: $CONFIG_DIR/supervisor.config.json)"
            else
                log_warn "Supervisor config already exists, skipping"
            fi
            
            if [[ ! -f "$CONFIG_DIR/supervisor.env" ]]; then
                cat > "$CONFIG_DIR/supervisor.env" << 'EOF'
# HAL-O-SWARM Supervisor Environment Variables
# Uncomment and set as needed

# LOG_LEVEL=info
# RUST_LOG=hal_o_swarm=debug
EOF
                chmod 600 "$CONFIG_DIR/supervisor.env"
                log_success "Installed supervisor env file"
            fi
        fi
    fi
    
    if [[ "$INSTALL_AGENT" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install agent config"
        else
            if [[ ! -f "$CONFIG_DIR/agent.config.json" ]]; then
                cp "$PROJECT_ROOT/agent.config.example.json" "$CONFIG_DIR/agent.config.json"
                chmod 600 "$CONFIG_DIR/agent.config.json"
                log_success "Installed agent config (EDIT REQUIRED: $CONFIG_DIR/agent.config.json)"
            else
                log_warn "Agent config already exists, skipping"
            fi
            
            if [[ ! -f "$CONFIG_DIR/agent.env" ]]; then
                cat > "$CONFIG_DIR/agent.env" << 'EOF'
# HAL-O-SWARM Agent Environment Variables
# Uncomment and set as needed

# LOG_LEVEL=info
# RUST_LOG=hal_o_swarm=debug
EOF
                chmod 600 "$CONFIG_DIR/agent.env"
                log_success "Installed agent env file"
            fi
        fi
    fi
}

# Install systemd units
install_systemd_units() {
    log_info "Installing systemd units..."
    
    if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install hal-supervisor.service"
        else
            install -m 0644 "$SCRIPT_DIR/systemd/hal-supervisor.service" /etc/systemd/system/
            systemctl daemon-reload
            log_success "Installed hal-supervisor.service"
        fi
    fi
    
    if [[ "$INSTALL_AGENT" == "true" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would install hal-agent.service"
        else
            install -m 0644 "$SCRIPT_DIR/systemd/hal-agent.service" /etc/systemd/system/
            systemctl daemon-reload
            log_success "Installed hal-agent.service"
        fi
    fi
}

# Set permissions
set_permissions() {
    log_info "Setting permissions..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "[DRY RUN] Would set permissions on $DATA_DIR and $LOG_DIR"
    else
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            chown -R hal-supervisor:hal-supervisor "$DATA_DIR" "$LOG_DIR"
            chmod 750 "$DATA_DIR" "$LOG_DIR"
            log_success "Set permissions for supervisor"
        fi
        
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            chown -R hal-agent:hal-agent "$DATA_DIR" "$LOG_DIR"
            chmod 750 "$DATA_DIR" "$LOG_DIR"
            log_success "Set permissions for agent"
        fi
    fi
}

# Print usage
usage() {
    cat << EOF
HAL-O-SWARM Installation Script v$VERSION

Usage: $0 [OPTIONS]

Options:
    --supervisor        Install supervisor component
    --agent             Install agent component
    --halctl            Install halctl CLI tool
    --all               Install all components
    --skip-preflight    Skip preflight checks
    --dry-run           Show what would be installed without making changes
    --prefix PATH       Installation prefix (default: /usr/local)
    --config-dir PATH   Configuration directory (default: /etc/hal-o-swarm)
    --data-dir PATH     Data directory (default: /var/lib/hal-o-swarm)
    --log-dir PATH      Log directory (default: /var/log/hal-o-swarm)
    --help              Show this help message

Examples:
    # Install supervisor only
    sudo $0 --supervisor

    # Install all components
    sudo $0 --all

    # Dry run to see what would be installed
    sudo $0 --all --dry-run

    # Install with custom paths
    sudo $0 --all --prefix /opt/hal --config-dir /etc/hal

EOF
}

# Main installation flow
main() {
    log_info "HAL-O-SWARM Installation Script v$VERSION"
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
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
            --skip-preflight)
                SKIP_PREFLIGHT=true
                shift
                ;;
            --dry-run)
                DRY_RUN=true
                shift
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
            --help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
    
    # Validate that at least one component is selected
    if [[ "$INSTALL_SUPERVISOR" == "false" ]] && [[ "$INSTALL_AGENT" == "false" ]] && [[ "$INSTALL_HALCTL" == "false" ]]; then
        log_error "No components selected. Use --supervisor, --agent, --halctl, or --all"
        usage
        exit 1
    fi
    
    # Check root
    check_root
    
    # Preflight checks
    if [[ "$SKIP_PREFLIGHT" == "false" ]]; then
        if ! check_system_requirements; then
            log_error "Preflight checks failed"
            exit 1
        fi
    else
        log_warn "Skipping preflight checks"
    fi
    
    # Build binaries
    build_binaries
    
    # Create users
    create_users
    
    # Create directories
    create_directories
    
    # Install binaries
    install_binaries
    
    # Install configs
    install_configs
    
    # Install systemd units
    install_systemd_units
    
    # Set permissions
    set_permissions
    
    # Summary
    echo ""
    log_success "Installation complete!"
    echo ""
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "This was a dry run. No changes were made."
    else
        log_info "Next steps:"
        echo "  1. Edit configuration files:"
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            echo "     - $CONFIG_DIR/supervisor.config.json"
        fi
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            echo "     - $CONFIG_DIR/agent.config.json"
        fi
        echo ""
        echo "  2. Start services:"
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            echo "     sudo systemctl start hal-supervisor"
        fi
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            echo "     sudo systemctl start hal-agent"
        fi
        echo ""
        echo "  3. Enable services to start on boot:"
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            echo "     sudo systemctl enable hal-supervisor"
        fi
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            echo "     sudo systemctl enable hal-agent"
        fi
        echo ""
        echo "  4. Check service status:"
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            echo "     sudo systemctl status hal-supervisor"
        fi
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            echo "     sudo systemctl status hal-agent"
        fi
        echo ""
        echo "  5. View logs:"
        if [[ "$INSTALL_SUPERVISOR" == "true" ]]; then
            echo "     sudo journalctl -u hal-supervisor -f"
        fi
        if [[ "$INSTALL_AGENT" == "true" ]]; then
            echo "     sudo journalctl -u hal-agent -f"
        fi
    fi
}

main "$@"
