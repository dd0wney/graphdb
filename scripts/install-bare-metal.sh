#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Bare Metal Installation Script
#
# Supports: Fedora, RHEL, CentOS, Ubuntu, Debian
#
# Usage:
#   # Install with defaults
#   sudo ./install-bare-metal.sh
#
#   # Install with custom settings
#   GRAPHDB_PORT=9090 GRAPHDB_DATA_DIR=/data/graphdb sudo ./install-bare-metal.sh
#
# Environment Variables:
#   GRAPHDB_PORT      - Port to listen on (default: 8080)
#   GRAPHDB_DATA_DIR  - Data directory (default: /var/lib/graphdb/data)
#   GRAPHDB_USER      - System user (default: graphdb)
#   GRAPHDB_BINARY    - Path to binary (default: ./graphdb-server or downloads)
#   GRAPHDB_EDITION   - Edition: enterprise or community (default: enterprise)
#   JWT_SECRET        - JWT secret for auth (default: generated)
#   ADMIN_PASSWORD    - Admin password (default: generated)
#   SKIP_FIREWALL     - Set to "true" to skip firewall config
#
#######################################################################

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${BLUE}[STEP]${NC} $1"; }

# Configuration with defaults
GRAPHDB_PORT="${GRAPHDB_PORT:-8080}"
GRAPHDB_DATA_DIR="${GRAPHDB_DATA_DIR:-/var/lib/graphdb/data}"
GRAPHDB_USER="${GRAPHDB_USER:-graphdb}"
GRAPHDB_GROUP="${GRAPHDB_GROUP:-graphdb}"
GRAPHDB_HOME="/var/lib/graphdb"
GRAPHDB_BINARY="${GRAPHDB_BINARY:-}"
GRAPHDB_EDITION="${GRAPHDB_EDITION:-enterprise}"
INSTALL_DIR="/usr/local/bin"
SERVICE_NAME="graphdb"
SKIP_FIREWALL="${SKIP_FIREWALL:-false}"

# Generate secrets if not provided
JWT_SECRET="${JWT_SECRET:-$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-$(openssl rand -base64 16 2>/dev/null || head -c 16 /dev/urandom | base64 | tr -d '=+/')}"

#######################################################################
# Pre-flight Checks
#######################################################################

preflight_checks() {
    log_step "Running pre-flight checks..."

    # Must be root
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi

    # Detect OS
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS_ID="${ID}"
        OS_VERSION="${VERSION_ID}"
        OS_NAME="${PRETTY_NAME}"
    else
        log_error "Cannot detect OS. /etc/os-release not found."
        exit 1
    fi

    log_info "Detected OS: ${OS_NAME}"

    # Check for systemd
    if ! command -v systemctl &> /dev/null; then
        log_error "systemd not found. This script requires systemd."
        exit 1
    fi

    # Check architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64) GOARCH="amd64" ;;
        aarch64) GOARCH="arm64" ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    log_info "Architecture: ${ARCH} (${GOARCH})"
    log_info "Pre-flight checks passed ✓"
}

#######################################################################
# Find or Download Binary
#######################################################################

setup_binary() {
    log_step "Setting up GraphDB binary..."

    # Check if binary path provided
    if [[ -n "$GRAPHDB_BINARY" ]]; then
        if [[ -f "$GRAPHDB_BINARY" ]]; then
            log_info "Using provided binary: $GRAPHDB_BINARY"
            cp "$GRAPHDB_BINARY" "${INSTALL_DIR}/graphdb-server"
        else
            log_error "Binary not found: $GRAPHDB_BINARY"
            exit 1
        fi
    # Check current directory
    elif [[ -f "./graphdb-server" ]]; then
        log_info "Using binary from current directory"
        cp "./graphdb-server" "${INSTALL_DIR}/graphdb-server"
    # Check if already installed
    elif [[ -f "${INSTALL_DIR}/graphdb-server" ]]; then
        log_info "Using existing binary at ${INSTALL_DIR}/graphdb-server"
    else
        log_error "No GraphDB binary found!"
        echo ""
        echo "Please provide the binary using one of these methods:"
        echo ""
        echo "  1. Place 'graphdb-server' in the current directory"
        echo "  2. Set GRAPHDB_BINARY environment variable:"
        echo "     GRAPHDB_BINARY=/path/to/graphdb-server sudo ./install-bare-metal.sh"
        echo ""
        echo "  Build the binary on your dev machine:"
        echo "     GOOS=linux GOARCH=${GOARCH} go build -ldflags \"-s -w\" -o graphdb-server ./cmd/server"
        echo ""
        exit 1
    fi

    chmod +x "${INSTALL_DIR}/graphdb-server"
    log_info "Binary installed at ${INSTALL_DIR}/graphdb-server ✓"
}

#######################################################################
# Create User and Directories
#######################################################################

setup_user_and_dirs() {
    log_step "Creating user and directories..."

    # Create system user if doesn't exist
    if ! id "$GRAPHDB_USER" &>/dev/null; then
        useradd -r -s /sbin/nologin -d "$GRAPHDB_HOME" -m "$GRAPHDB_USER"
        log_info "Created system user: $GRAPHDB_USER"
    else
        log_info "User $GRAPHDB_USER already exists"
    fi

    # Create directories
    mkdir -p "$GRAPHDB_DATA_DIR"
    mkdir -p "$GRAPHDB_HOME/logs"
    mkdir -p "$GRAPHDB_HOME/backups"

    # Set ownership
    chown -R "${GRAPHDB_USER}:${GRAPHDB_GROUP}" "$GRAPHDB_HOME"

    log_info "Directories created ✓"
}

#######################################################################
# Create Environment File
#######################################################################

setup_environment() {
    log_step "Creating environment configuration..."

    cat > "${GRAPHDB_HOME}/graphdb.env" <<EOF
# GraphDB Configuration
# Generated: $(date -Iseconds)

# Server settings
PORT=${GRAPHDB_PORT}
DATA_DIR=${GRAPHDB_DATA_DIR}

# Edition: enterprise or community
GRAPHDB_EDITION=${GRAPHDB_EDITION}

# Authentication (CHANGE THESE IN PRODUCTION!)
JWT_SECRET=${JWT_SECRET}
ADMIN_PASSWORD=${ADMIN_PASSWORD}

# Optional: CORS (comma-separated origins)
# CORS_ALLOWED_ORIGINS=https://your-app.com

# Optional: TLS
# TLS_ENABLED=true
# TLS_CERT_FILE=/path/to/cert.pem
# TLS_KEY_FILE=/path/to/key.pem

# Optional: Audit logging
# AUDIT_PERSISTENT=true
# AUDIT_DIR=${GRAPHDB_HOME}/audit
EOF

    chmod 600 "${GRAPHDB_HOME}/graphdb.env"
    chown "${GRAPHDB_USER}:${GRAPHDB_GROUP}" "${GRAPHDB_HOME}/graphdb.env"

    log_info "Environment file created at ${GRAPHDB_HOME}/graphdb.env ✓"
}

#######################################################################
# Create Systemd Service
#######################################################################

setup_systemd() {
    log_step "Creating systemd service..."

    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=GraphDB Graph Database
Documentation=https://github.com/dd0wney/graphdb
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${GRAPHDB_USER}
Group=${GRAPHDB_GROUP}
WorkingDirectory=${GRAPHDB_HOME}

# Load environment
EnvironmentFile=${GRAPHDB_HOME}/graphdb.env

# Start command
ExecStart=${INSTALL_DIR}/graphdb-server --port \${PORT} --data \${DATA_DIR}

# Restart policy
Restart=always
RestartSec=5
StartLimitInterval=60
StartLimitBurst=3

# Resource limits
LimitNOFILE=65535
LimitNPROC=4096

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ReadWritePaths=${GRAPHDB_HOME}

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=graphdb

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    log_info "Systemd service created ✓"
}

#######################################################################
# Configure Firewall
#######################################################################

setup_firewall() {
    if [[ "$SKIP_FIREWALL" == "true" ]]; then
        log_info "Skipping firewall configuration (SKIP_FIREWALL=true)"
        return
    fi

    log_step "Configuring firewall..."

    # Fedora/RHEL use firewalld
    if command -v firewall-cmd &> /dev/null; then
        if systemctl is-active --quiet firewalld; then
            firewall-cmd --permanent --add-port="${GRAPHDB_PORT}/tcp" 2>/dev/null || true
            firewall-cmd --reload 2>/dev/null || true
            log_info "Opened port ${GRAPHDB_PORT}/tcp in firewalld ✓"
        else
            log_warn "firewalld is not running, skipping"
        fi
    # Ubuntu/Debian use ufw
    elif command -v ufw &> /dev/null; then
        if ufw status | grep -q "active"; then
            ufw allow "${GRAPHDB_PORT}/tcp" 2>/dev/null || true
            log_info "Opened port ${GRAPHDB_PORT}/tcp in ufw ✓"
        else
            log_warn "ufw is not active, skipping"
        fi
    else
        log_warn "No supported firewall found (firewalld or ufw)"
    fi
}

#######################################################################
# Create Helper Scripts
#######################################################################

setup_helper_scripts() {
    log_step "Creating helper scripts..."

    # Backup script
    cat > "${GRAPHDB_HOME}/backup.sh" <<'EOF'
#!/bin/bash
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/var/lib/graphdb/backups}"
DATA_DIR="${DATA_DIR:-/var/lib/graphdb/data}"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_NAME="graphdb-backup-${TIMESTAMP}.tar.gz"

echo "Creating backup: ${BACKUP_NAME}"

# Stop service briefly for consistent backup
systemctl stop graphdb || true
sleep 2

# Create backup
tar -czf "${BACKUP_DIR}/${BACKUP_NAME}" -C "$(dirname $DATA_DIR)" "$(basename $DATA_DIR)"

# Restart service
systemctl start graphdb

# Keep only last 7 backups
ls -t "${BACKUP_DIR}"/graphdb-backup-*.tar.gz 2>/dev/null | tail -n +8 | xargs -r rm

echo "Backup complete: ${BACKUP_DIR}/${BACKUP_NAME}"
echo "Backups on disk: $(ls -1 ${BACKUP_DIR}/graphdb-backup-*.tar.gz 2>/dev/null | wc -l)"
EOF

    chmod +x "${GRAPHDB_HOME}/backup.sh"

    # Status script
    cat > "${GRAPHDB_HOME}/status.sh" <<EOF
#!/bin/bash
echo "=== GraphDB Status ==="
echo ""
systemctl status ${SERVICE_NAME} --no-pager -l
echo ""
echo "=== Health Check ==="
curl -s http://localhost:${GRAPHDB_PORT}/health | jq . 2>/dev/null || curl -s http://localhost:${GRAPHDB_PORT}/health
echo ""
echo "=== Resource Usage ==="
ps aux | grep graphdb-server | grep -v grep || echo "Process not running"
EOF

    chmod +x "${GRAPHDB_HOME}/status.sh"

    chown -R "${GRAPHDB_USER}:${GRAPHDB_GROUP}" "${GRAPHDB_HOME}"

    log_info "Helper scripts created ✓"
}

#######################################################################
# Enable and Start Service
#######################################################################

start_service() {
    log_step "Enabling and starting GraphDB..."

    systemctl enable "${SERVICE_NAME}"
    systemctl start "${SERVICE_NAME}"

    # Wait for startup
    sleep 3

    if systemctl is-active --quiet "${SERVICE_NAME}"; then
        log_info "GraphDB started successfully ✓"
    else
        log_error "GraphDB failed to start. Check logs with: journalctl -u ${SERVICE_NAME}"
        exit 1
    fi
}

#######################################################################
# Verify Installation
#######################################################################

verify_installation() {
    log_step "Verifying installation..."

    local max_attempts=10
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
        if curl -s "http://localhost:${GRAPHDB_PORT}/health" > /dev/null 2>&1; then
            log_info "Health check passed ✓"
            break
        fi
        log_info "Waiting for GraphDB to be ready... (${attempt}/${max_attempts})"
        sleep 2
        ((attempt++))
    done

    if [[ $attempt -gt $max_attempts ]]; then
        log_warn "Health check timed out, but service may still be starting"
    fi
}

#######################################################################
# Print Summary
#######################################################################

print_summary() {
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}  GraphDB Installation Complete!${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo "  Configuration:"
    echo "    • Binary:      ${INSTALL_DIR}/graphdb-server"
    echo "    • Data:        ${GRAPHDB_DATA_DIR}"
    echo "    • Config:      ${GRAPHDB_HOME}/graphdb.env"
    echo "    • Port:        ${GRAPHDB_PORT}"
    echo "    • Edition:     ${GRAPHDB_EDITION^^}"
    echo "    • User:        ${GRAPHDB_USER}"
    echo ""
    echo "  Credentials (saved in ${GRAPHDB_HOME}/graphdb.env):"
    echo "    • Admin password: ${ADMIN_PASSWORD}"
    echo ""
    echo "  Commands:"
    echo "    • Status:      sudo systemctl status graphdb"
    echo "    • Logs:        sudo journalctl -u graphdb -f"
    echo "    • Restart:     sudo systemctl restart graphdb"
    echo "    • Stop:        sudo systemctl stop graphdb"
    echo "    • Backup:      sudo ${GRAPHDB_HOME}/backup.sh"
    echo ""
    echo "  Quick Test:"
    echo "    curl http://localhost:${GRAPHDB_PORT}/health"
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
}

#######################################################################
# Main
#######################################################################

main() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║           GraphDB Bare Metal Installation Script              ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    preflight_checks
    setup_binary
    setup_user_and_dirs
    setup_environment
    setup_systemd
    setup_firewall
    setup_helper_scripts
    start_service
    verify_installation
    print_summary
}

main "$@"
