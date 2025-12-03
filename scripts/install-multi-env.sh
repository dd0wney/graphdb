#!/bin/bash
set -euo pipefail

# GraphDB Multi-Environment Installation Script
# Installs both staging and production instances on the same machine
#
# Usage:
#   sudo ./install-multi-env.sh              # Install with defaults
#   sudo ./install-multi-env.sh --uninstall  # Remove installation
#
# Environment Variables (optional):
#   STAGING_PORT           - Staging API port (default: 8081)
#   PRODUCTION_PORT        - Production API port (default: 8080)
#   STAGING_JWT_SECRET     - JWT secret for staging (auto-generated if not set)
#   PRODUCTION_JWT_SECRET  - JWT secret for production (auto-generated if not set)
#   STAGING_ADMIN_PASSWORD - Admin password for staging (auto-generated if not set)
#   PRODUCTION_ADMIN_PASSWORD - Admin password for production (auto-generated if not set)

# Configuration
GRAPHDB_USER="${GRAPHDB_USER:-graphdb}"
GRAPHDB_GROUP="${GRAPHDB_GROUP:-graphdb}"
GRAPHDB_HOME="/var/lib/graphdb"
INSTALL_DIR="/usr/local/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_DIR="/etc/systemd/system"

# Ports
STAGING_PORT="${STAGING_PORT:-8081}"
PRODUCTION_PORT="${PRODUCTION_PORT:-8080}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step()  { echo -e "${BLUE}[STEP]${NC} $1"; }

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

# Generate secure random string
generate_secret() {
    openssl rand -base64 32
}

# Generate password (alphanumeric only for easier handling)
generate_password() {
    openssl rand -base64 16 | tr -d '=+/' | head -c 16
}

# Create graphdb user if it doesn't exist
create_user() {
    if ! id "$GRAPHDB_USER" &>/dev/null; then
        log_step "Creating system user: $GRAPHDB_USER"
        useradd -r -s /sbin/nologin -d "$GRAPHDB_HOME" -m "$GRAPHDB_USER"
    else
        log_info "User $GRAPHDB_USER already exists"
    fi
}

# Create directory structure
create_directories() {
    log_step "Creating directory structure"

    for env in staging production; do
        mkdir -p "$GRAPHDB_HOME/$env/data"
        mkdir -p "$GRAPHDB_HOME/$env/logs"
        mkdir -p "$GRAPHDB_HOME/$env/audit"
    done

    mkdir -p "$GRAPHDB_HOME/backups/staging"
    mkdir -p "$GRAPHDB_HOME/backups/production"

    chown -R "${GRAPHDB_USER}:${GRAPHDB_GROUP}" "$GRAPHDB_HOME"
    chmod 750 "$GRAPHDB_HOME"

    log_info "Directory structure created at $GRAPHDB_HOME"
}

# Create environment configuration file
create_env_file() {
    local env_name="$1"
    local port="$2"
    local graphdb_env="$3"
    local jwt_secret="$4"
    local admin_password="$5"

    local env_file="$GRAPHDB_HOME/$env_name/graphdb.env"

    log_step "Creating environment file: $env_file"

    cat > "$env_file" <<EOF
# GraphDB ${env_name^} Environment Configuration
# Generated on $(date -Iseconds)

# Server Configuration
PORT=${port}
DATA_DIR=${GRAPHDB_HOME}/${env_name}/data

# Environment Mode
# - "production" requires gdb_live_ prefixed API keys
# - "test" allows gdb_test_ prefixed API keys
GRAPHDB_ENV=${graphdb_env}

# Edition (community or enterprise)
GRAPHDB_EDITION=community

# Authentication
JWT_SECRET=${jwt_secret}
ADMIN_PASSWORD=${admin_password}

# Audit Logging (optional)
AUDIT_PERSISTENT=true
AUDIT_DIR=${GRAPHDB_HOME}/${env_name}/audit

# TLS Configuration (optional - uncomment to enable)
# TLS_ENABLED=true
# TLS_CERT_FILE=/path/to/cert.pem
# TLS_KEY_FILE=/path/to/key.pem

# Encryption (optional - uncomment to enable)
# ENCRYPTION_ENABLED=true
# ENCRYPTION_KEY_DIR=${GRAPHDB_HOME}/${env_name}/keys
EOF

    # Secure the environment file (contains secrets)
    chmod 600 "$env_file"
    chown "${GRAPHDB_USER}:${GRAPHDB_GROUP}" "$env_file"

    log_info "Environment file created with secure permissions"
}

# Install systemd service
install_service() {
    local env_name="$1"
    local service_file="graphdb-${env_name}.service"
    local source_file="$SCRIPT_DIR/systemd/$service_file"
    local dest_file="$SYSTEMD_DIR/$service_file"

    log_step "Installing systemd service: $service_file"

    if [[ -f "$source_file" ]]; then
        cp "$source_file" "$dest_file"
    else
        log_warn "Service template not found at $source_file, generating inline"
        generate_service_file "$env_name" > "$dest_file"
    fi

    chmod 644 "$dest_file"
    log_info "Service installed: $dest_file"
}

# Generate service file inline (fallback if template not found)
generate_service_file() {
    local env_name="$1"

    cat <<EOF
[Unit]
Description=GraphDB Graph Database (${env_name^})
Documentation=https://github.com/dd0wney/graphdb
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${GRAPHDB_USER}
Group=${GRAPHDB_GROUP}
WorkingDirectory=${GRAPHDB_HOME}/${env_name}

EnvironmentFile=${GRAPHDB_HOME}/${env_name}/graphdb.env
ExecStart=${INSTALL_DIR}/graphdb-server --port \${PORT} --data \${DATA_DIR}

Restart=always
RestartSec=5
StartLimitInterval=60
StartLimitBurst=3

LimitNOFILE=65535
LimitNPROC=4096

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ReadWritePaths=${GRAPHDB_HOME}/${env_name}

StandardOutput=journal
StandardError=journal
SyslogIdentifier=graphdb-${env_name}

[Install]
WantedBy=multi-user.target
EOF
}

# Check if graphdb-server binary exists
check_binary() {
    if [[ ! -x "$INSTALL_DIR/graphdb-server" ]]; then
        log_warn "graphdb-server binary not found at $INSTALL_DIR/graphdb-server"
        log_info "You need to build and install it first:"
        echo "    go build -o graphdb-server ./cmd/server"
        echo "    sudo cp graphdb-server $INSTALL_DIR/"
        return 1
    fi
    return 0
}

# Enable and start services
start_services() {
    log_step "Enabling and starting services"

    systemctl daemon-reload

    for env in staging production; do
        systemctl enable "graphdb-${env}.service"
        systemctl start "graphdb-${env}.service"

        # Wait a moment and check status
        sleep 2
        if systemctl is-active --quiet "graphdb-${env}.service"; then
            log_info "graphdb-${env} started successfully"
        else
            log_warn "graphdb-${env} may have failed to start, check: journalctl -u graphdb-${env}"
        fi
    done
}

# Print summary
print_summary() {
    local staging_pass="$1"
    local production_pass="$2"

    echo ""
    echo "=============================================="
    echo -e "${GREEN}GraphDB Multi-Environment Setup Complete${NC}"
    echo "=============================================="
    echo ""
    echo "Staging Environment:"
    echo "  URL:            http://localhost:${STAGING_PORT}"
    echo "  API Keys:       Use gdb_test_* prefix"
    echo "  Data Directory: ${GRAPHDB_HOME}/staging/data"
    echo "  Admin Password: ${staging_pass}"
    echo "  Service:        sudo systemctl {start|stop|status} graphdb-staging"
    echo ""
    echo "Production Environment:"
    echo "  URL:            http://localhost:${PRODUCTION_PORT}"
    echo "  API Keys:       Use gdb_live_* prefix"
    echo "  Data Directory: ${GRAPHDB_HOME}/production/data"
    echo "  Admin Password: ${production_pass}"
    echo "  Service:        sudo systemctl {start|stop|status} graphdb-production"
    echo ""
    echo "Useful Commands:"
    echo "  View staging logs:    journalctl -u graphdb-staging -f"
    echo "  View production logs: journalctl -u graphdb-production -f"
    echo "  Edit staging config:  sudo nano ${GRAPHDB_HOME}/staging/graphdb.env"
    echo ""
    echo -e "${YELLOW}IMPORTANT: Save these admin passwords securely!${NC}"
    echo ""
}

# Uninstall function
uninstall() {
    log_step "Uninstalling GraphDB multi-environment setup"

    # Stop and disable services
    for env in staging production; do
        if systemctl is-active --quiet "graphdb-${env}.service" 2>/dev/null; then
            log_info "Stopping graphdb-${env}"
            systemctl stop "graphdb-${env}.service"
        fi
        if systemctl is-enabled --quiet "graphdb-${env}.service" 2>/dev/null; then
            systemctl disable "graphdb-${env}.service"
        fi
        rm -f "$SYSTEMD_DIR/graphdb-${env}.service"
    done

    systemctl daemon-reload

    log_warn "Services removed. Data directory preserved at $GRAPHDB_HOME"
    log_info "To remove data: sudo rm -rf $GRAPHDB_HOME"
    log_info "To remove user: sudo userdel $GRAPHDB_USER"
}

# Main installation function
main() {
    echo ""
    echo "=============================================="
    echo "GraphDB Multi-Environment Installation"
    echo "=============================================="
    echo ""

    check_root

    # Handle uninstall
    if [[ "${1:-}" == "--uninstall" ]]; then
        uninstall
        exit 0
    fi

    # Generate secrets if not provided
    STAGING_JWT_SECRET="${STAGING_JWT_SECRET:-$(generate_secret)}"
    STAGING_ADMIN_PASSWORD="${STAGING_ADMIN_PASSWORD:-$(generate_password)}"
    PRODUCTION_JWT_SECRET="${PRODUCTION_JWT_SECRET:-$(generate_secret)}"
    PRODUCTION_ADMIN_PASSWORD="${PRODUCTION_ADMIN_PASSWORD:-$(generate_password)}"

    create_user
    create_directories

    # Create environment files
    create_env_file "staging" "$STAGING_PORT" "test" "$STAGING_JWT_SECRET" "$STAGING_ADMIN_PASSWORD"
    create_env_file "production" "$PRODUCTION_PORT" "production" "$PRODUCTION_JWT_SECRET" "$PRODUCTION_ADMIN_PASSWORD"

    # Install services
    install_service "staging"
    install_service "production"

    # Check for binary and optionally start services
    if check_binary; then
        start_services
    else
        log_warn "Services installed but not started (binary missing)"
        log_info "After installing the binary, run:"
        echo "    sudo systemctl start graphdb-staging graphdb-production"
    fi

    print_summary "$STAGING_ADMIN_PASSWORD" "$PRODUCTION_ADMIN_PASSWORD"
}

main "$@"
