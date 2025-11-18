#!/bin/bash
set -e

#######################################################################
# GraphDB Digital Ocean Deployment Script
#
# This script automates the deployment of GraphDB to a Digital Ocean
# droplet with Cloudflare Tunnel integration.
#
# Prerequisites:
# - Digital Ocean droplet (Ubuntu 22.04 LTS recommended)
# - Cloudflare account with Tunnel configured
# - Domain name pointed to Cloudflare
#
# Usage:
#   bash setup.sh [community|enterprise]
#######################################################################

EDITION="${1:-community}"
GRAPHDB_VERSION="latest"

echo "========================================="
echo "GraphDB Digital Ocean Deployment"
echo "Edition: $EDITION"
echo "========================================="

# Update system packages
echo "[1/8] Updating system packages..."
apt-get update
apt-get upgrade -y

# Install Docker and Docker Compose
echo "[2/8] Installing Docker..."
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    systemctl enable docker
    systemctl start docker
    rm get-docker.sh
else
    echo "Docker already installed"
fi

# Install cloudflared (Cloudflare Tunnel daemon)
echo "[3/8] Installing cloudflared..."
if ! command -v cloudflared &> /dev/null; then
    curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb -o cloudflared.deb
    dpkg -i cloudflared.deb
    rm cloudflared.deb
else
    echo "cloudflared already installed"
fi

# Create GraphDB data directory
echo "[4/8] Creating data directories..."
mkdir -p /var/lib/graphdb/data
mkdir -p /var/lib/graphdb/backups
mkdir -p /etc/graphdb
mkdir -p /etc/cloudflared

# Set up configuration based on edition
echo "[5/8] Setting up $EDITION edition configuration..."
if [ "$EDITION" = "enterprise" ]; then
    cat > /etc/graphdb/config.yaml <<EOF
# GraphDB Enterprise Edition Configuration
edition: enterprise
server:
  port: 8080
storage:
  data_dir: /var/lib/graphdb/data
  enable_wal: true
  enable_batching: true
  enable_compression: true
EOF
else
    cat > /etc/graphdb/config.yaml <<EOF
# GraphDB Community Edition Configuration
edition: community
server:
  port: 8080
storage:
  data_dir: /var/lib/graphdb/data
  enable_wal: true
  enable_batching: true
  enable_compression: true
EOF
fi

# Create Docker Compose file
echo "[6/8] Creating Docker Compose configuration..."
cat > /var/lib/graphdb/docker-compose.yml <<EOF
version: '3.8'

services:
  graphdb:
    image: graphdb/graphdb:${GRAPHDB_VERSION}
    container_name: graphdb
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /var/lib/graphdb/data:/data
      - /etc/graphdb/config.yaml:/etc/graphdb/config.yaml:ro
    environment:
      - GRAPHDB_EDITION=${EDITION}
      - PORT=8080
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s

  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: cloudflared
    restart: unless-stopped
    command: tunnel --config /etc/cloudflared/config.yml run
    volumes:
      - /etc/cloudflared:/etc/cloudflared:ro
    depends_on:
      - graphdb
EOF

# Set up systemd service for auto-start
echo "[7/8] Creating systemd service..."
cat > /etc/systemd/system/graphdb.service <<EOF
[Unit]
Description=GraphDB with Cloudflare Tunnel
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/var/lib/graphdb
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down

[Install]
WantedBy=multi-user.target
EOF

# Enable service
systemctl daemon-reload
systemctl enable graphdb

echo "[8/8] Installation complete!"
echo ""
echo "========================================="
echo "Next Steps:"
echo "========================================="
echo "1. Configure Cloudflare Tunnel:"
echo "   - Copy tunnel-config.yml to /etc/cloudflared/config.yml"
echo "   - Copy tunnel credentials to /etc/cloudflared/credentials.json"
echo "   - Update hostnames in config.yml"
echo ""
echo "2. Start GraphDB:"
echo "   systemctl start graphdb"
echo ""
echo "3. Check status:"
echo "   systemctl status graphdb"
echo "   docker compose -f /var/lib/graphdb/docker-compose.yml logs -f"
echo ""
echo "4. Verify deployment:"
echo "   curl http://localhost:8080/health"
echo ""
if [ "$EDITION" = "enterprise" ]; then
    echo "5. Add license key (Enterprise):"
    echo "   Copy license.key to /etc/graphdb/license.key"
    echo ""
fi
echo "========================================="
