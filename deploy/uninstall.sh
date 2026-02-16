#!/bin/bash
set -euo pipefail

# HAL-O-SWARM Uninstallation Script
# Safely removes supervisor and/or agent with data preservation options

VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
REMOVE_SUPERVISOR=${REMOVE_SUPERVISOR:-false}
REMOVE_AGENT=${REMOVE_AGENT:-false}
REMOVE_HALCTL=${REMOVE_HALCTL:-false}
PRESERVE_DATA=${PRESERVE_DATA:-true}
PRESERVE_CONFIG=${PRESERVE_CONFIG:-true}
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

# Stop services
stop_services() {
    log_info "Stopping services..."
    
    if [[ "$REMOVE_SUPERVISOR" == "true" ]]; then
        if systemctl is-active --quiet hal-supervisor; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would stop hal-supervisor"
            else
                log_info "Stopping hal-supervisor..."
                systemctl stop hal-supervisor
                log_success "Stopped hal-supervisor"
            fi
        else
            log_info "hal-supervisor is not running"
        fi
    fi
    
    if [[ "$REMOVE_AGENT" == "true" ]]; then
        if systemctl is-active --quiet hal-agent; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would stop hal-agent"
            else
                log_info "Stopping hal-agent..."
                systemctl stop hal-agent
                log_success "Stopped hal-agent"
            fi
        else
            log_info "hal-agent is not running"
        fi
    fi
}

# Disable services
disable_services() {
    log_info "Disabling services..."
    
    if [[ "$REMOVE_SUPERVISOR" == "true" ]]; then
        if systemctl is-enabled --quiet hal-supervisor 2>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would disable hal-supervisor"
            else
                systemctl disable hal-supervisor
                log_success "Disabled hal-supervisor"
            fi
        fi
    fi
    
    if [[ "$REMOVE_AGENT" == "true" ]]; then
        if systemctl is-enabled --quiet hal-agent 2>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would disable hal-agent"
            else
                systemctl disable hal-agent
                log_success "Disabled hal-agent"
            fi
        fi
    fi
}

# Remove systemd units
remove_systemd_units() {
    log_info "Removing systemd units..."
    
    if [[ "$REMOVE_SUPERVISOR" == "true" ]]; then
        if [[ -f /etc/systemd/system/hal-supervisor.service ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove hal-supervisor.service"
            else
                rm -f /etc/systemd/system/hal-supervisor.service
                systemctl daemon-reload
                log_success "Removed hal-supervisor.service"
            fi
        fi
    fi
    
    if [[ "$REMOVE_AGENT" == "true" ]]; then
        if [[ -f /etc/systemd/system/hal-agent.service ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove hal-agent.service"
            else
                rm -f /etc/systemd/system/hal-agent.service
                systemctl daemon-reload
                log_success "Removed hal-agent.service"
            fi
        fi
    fi
}

# Remove binaries
remove_binaries() {
    log_info "Removing binaries..."
    
    if [[ "$REMOVE_SUPERVISOR" == "true" ]]; then
        if [[ -f "$INSTALL_PREFIX/bin/hal-supervisor" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove $INSTALL_PREFIX/bin/hal-supervisor"
            else
                rm -f "$INSTALL_PREFIX/bin/hal-supervisor"
                log_success "Removed hal-supervisor"
            fi
        fi
    fi
    
    if [[ "$REMOVE_AGENT" == "true" ]]; then
        if [[ -f "$INSTALL_PREFIX/bin/hal-agent" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove $INSTALL_PREFIX/bin/hal-agent"
            else
                rm -f "$INSTALL_PREFIX/bin/hal-agent"
                log_success "Removed hal-agent"
            fi
        fi
    fi
    
    if [[ "$REMOVE_HALCTL" == "true" ]]; then
        if [[ -f "$INSTALL_PREFIX/bin/halctl" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove $INSTALL_PREFIX/bin/halctl"
            else
                rm -f "$INSTALL_PREFIX/bin/halctl"
                log_success "Removed halctl"
            fi
        fi
    fi
}

# Remove configuration files
remove_configs() {
    log_info "Handling configuration files..."
    
    if [[ "$PRESERVE_CONFIG" == "true" ]]; then
        log_info "Preserving configuration files (--preserve-config enabled)"
        if [[ -d "$CONFIG_DIR" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would archive config to $CONFIG_DIR.backup.$(date +%s)"
            else
                local backup_dir="$CONFIG_DIR.backup.$(date +%s)"
                cp -r "$CONFIG_DIR" "$backup_dir"
                log_success "Configuration backed up to: $backup_dir"
            fi
        fi
    else
        log_warn "Removing configuration files (--preserve-config disabled)"
        if [[ -d "$CONFIG_DIR" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove $CONFIG_DIR"
            else
                rm -rf "$CONFIG_DIR"
                log_success "Removed configuration directory"
            fi
        fi
    fi
}

# Remove data files
remove_data() {
    log_info "Handling data files..."
    
    if [[ "$PRESERVE_DATA" == "true" ]]; then
        log_info "Preserving data files (--preserve-data enabled)"
        if [[ -d "$DATA_DIR" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would archive data to $DATA_DIR.backup.$(date +%s)"
            else
                local backup_dir="$DATA_DIR.backup.$(date +%s)"
                cp -r "$DATA_DIR" "$backup_dir"
                log_success "Data backed up to: $backup_dir"
            fi
        fi
    else
        log_warn "Removing data files (--preserve-data disabled)"
        if [[ -d "$DATA_DIR" ]]; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove $DATA_DIR"
            else
                rm -rf "$DATA_DIR"
                log_success "Removed data directory"
            fi
        fi
    fi
}

# Remove log files
remove_logs() {
    log_info "Handling log files..."
    
    if [[ -d "$LOG_DIR" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log_info "[DRY RUN] Would remove $LOG_DIR"
        else
            rm -rf "$LOG_DIR"
            log_success "Removed log directory"
        fi
    fi
}

# Remove users
remove_users() {
    log_info "Removing system users..."
    
    if [[ "$REMOVE_SUPERVISOR" == "true" ]]; then
        if id "hal-supervisor" &>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove user: hal-supervisor"
            else
                userdel hal-supervisor
                log_success "Removed user: hal-supervisor"
            fi
        fi
    fi
    
    if [[ "$REMOVE_AGENT" == "true" ]]; then
        if id "hal-agent" &>/dev/null; then
            if [[ "$DRY_RUN" == "true" ]]; then
                log_info "[DRY RUN] Would remove user: hal-agent"
            else
                userdel hal-agent
                log_success "Removed user: hal-agent"
            fi
        fi
    fi
}

# Print usage
usage() {
    cat << EOF
HAL-O-SWARM Uninstallation Script v$VERSION

Usage: $0 [OPTIONS]

Options:
    --supervisor        Remove supervisor component
    --agent             Remove agent component
    --halctl            Remove halctl CLI tool
    --all               Remove all components
    --preserve-data     Keep data files (default: true)
    --remove-data       Delete data files
    --preserve-config   Keep configuration files (default: true)
    --remove-config     Delete configuration files
    --dry-run           Show what would be removed without making changes
    --prefix PATH       Installation prefix (default: /usr/local)
    --config-dir PATH   Configuration directory (default: /etc/hal-o-swarm)
    --data-dir PATH     Data directory (default: /var/lib/hal-o-swarm)
    --log-dir PATH      Log directory (default: /var/log/hal-o-swarm)
    --help              Show this help message

Examples:
    # Remove supervisor only (preserve data and config)
    sudo $0 --supervisor

    # Remove all components and delete all data
    sudo $0 --all --remove-data --remove-config

    # Dry run to see what would be removed
    sudo $0 --all --dry-run

    # Remove with custom paths
    sudo $0 --all --prefix /opt/hal --config-dir /etc/hal

EOF
}

# Main uninstallation flow
main() {
    log_info "HAL-O-SWARM Uninstallation Script v$VERSION"
    
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --supervisor)
                REMOVE_SUPERVISOR=true
                shift
                ;;
            --agent)
                REMOVE_AGENT=true
                shift
                ;;
            --halctl)
                REMOVE_HALCTL=true
                shift
                ;;
            --all)
                REMOVE_SUPERVISOR=true
                REMOVE_AGENT=true
                REMOVE_HALCTL=true
                shift
                ;;
            --preserve-data)
                PRESERVE_DATA=true
                shift
                ;;
            --remove-data)
                PRESERVE_DATA=false
                shift
                ;;
            --preserve-config)
                PRESERVE_CONFIG=true
                shift
                ;;
            --remove-config)
                PRESERVE_CONFIG=false
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
    if [[ "$REMOVE_SUPERVISOR" == "false" ]] && [[ "$REMOVE_AGENT" == "false" ]] && [[ "$REMOVE_HALCTL" == "false" ]]; then
        log_error "No components selected. Use --supervisor, --agent, --halctl, or --all"
        usage
        exit 1
    fi
    
    # Check root
    check_root
    
    # Confirmation
    echo ""
    log_warn "This will remove HAL-O-SWARM components"
    echo "  Preserve data: $PRESERVE_DATA"
    echo "  Preserve config: $PRESERVE_CONFIG"
    echo ""
    
    if [[ "$DRY_RUN" != "true" ]]; then
        read -p "Are you sure? (yes/no): " -r
        if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
            log_info "Uninstallation cancelled"
            exit 0
        fi
    fi
    
    # Uninstallation steps
    stop_services
    disable_services
    remove_systemd_units
    remove_binaries
    remove_configs
    remove_data
    remove_logs
    remove_users
    
    # Summary
    echo ""
    log_success "Uninstallation complete!"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "This was a dry run. No changes were made."
    else
        if [[ "$PRESERVE_DATA" == "true" ]] || [[ "$PRESERVE_CONFIG" == "true" ]]; then
            echo ""
            log_info "Preserved files:"
            if [[ "$PRESERVE_DATA" == "true" ]] && [[ -d "$DATA_DIR.backup."* ]]; then
                echo "  - Data backups in: $DATA_DIR.backup.*"
            fi
            if [[ "$PRESERVE_CONFIG" == "true" ]] && [[ -d "$CONFIG_DIR.backup."* ]]; then
                echo "  - Config backups in: $CONFIG_DIR.backup.*"
            fi
        fi
    fi
}

main "$@"
