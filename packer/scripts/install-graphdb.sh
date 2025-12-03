#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Installation Script for Packer
#
# This script is run by Packer to install GraphDB and all dependencies
# on a fresh Ubuntu 22.04 droplet.
#
# Installs:
# - Docker and Docker Compose
# - GraphDB container image
# - System dependencies
# - Monitoring tools
# - Security configurations
#
#######################################################################

export DEBIAN_FRONTEND=noninteractive
GRAPHDB_VERSION="${GRAPHDB_VERSION:-latest}"

echo "========================================="
echo "GraphDB Installation Starting"
echo "Version: $GRAPHDB_VERSION"
echo "========================================="

#######################################################################
# Install Docker
#######################################################################

echo "[1/8] Installing Docker..."

# Add Docker's official GPG key
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | \
  gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

# Add Docker repository
echo \
  "deb [arch="$(dpkg --print-architecture)" signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  "$(. /etc/os-release && echo "$VERSION_CODENAME")" stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

# Install Docker
apt-get update
apt-get install -y \
  docker-ce \
  docker-ce-cli \
  containerd.io \
  docker-buildx-plugin \
  docker-compose-plugin

# Enable Docker service
systemctl enable docker
systemctl start docker

echo "✓ Docker installed successfully"

#######################################################################
# Install System Dependencies
#######################################################################

echo "[2/8] Installing system dependencies..."

apt-get install -y \
  curl \
  wget \
  git \
  jq \
  htop \
  vim \
  nano \
  ufw \
  fail2ban \
  unattended-upgrades \
  ca-certificates \
  gnupg \
  lsb-release \
  software-properties-common \
  apt-transport-https \
  rsync \
  s3cmd

echo "✓ System dependencies installed"

#######################################################################
# Install Monitoring Tools
#######################################################################

echo "[3/8] Installing monitoring tools..."

# Install node_exporter for Prometheus (optional)
ARCH="amd64"
NODE_EXPORTER_VERSION="1.7.0"

wget -q https://github.com/prometheus/node_exporter/releases/download/v${NODE_EXPORTER_VERSION}/node_exporter-${NODE_EXPORTER_VERSION}.linux-${ARCH}.tar.gz
tar xzf node_exporter-${NODE_EXPORTER_VERSION}.linux-${ARCH}.tar.gz
cp node_exporter-${NODE_EXPORTER_VERSION}.linux-${ARCH}/node_exporter /usr/local/bin/
rm -rf node_exporter-${NODE_EXPORTER_VERSION}.linux-${ARCH}*

# Create node_exporter service
cat > /etc/systemd/system/node_exporter.service <<'EOF'
[Unit]
Description=Node Exporter
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/node_exporter

[Install]
WantedBy=multi-user.target
EOF

systemctl enable node_exporter

echo "✓ Monitoring tools installed"

#######################################################################
# Pull GraphDB Container Image
#######################################################################

echo "[4/8] Pulling GraphDB container image..."

# Pull the GraphDB image (update this to your actual image)
if [ "$GRAPHDB_VERSION" = "latest" ]; then
  docker pull ghcr.io/dd0wney/graphdb:latest || \
    echo "Warning: GraphDB image not yet available, will be pulled on first boot"
else
  docker pull ghcr.io/dd0wney/graphdb:$GRAPHDB_VERSION || \
    echo "Warning: GraphDB image not yet available, will be pulled on first boot"
fi

echo "✓ GraphDB image pulled"

#######################################################################
# Create Docker Compose Configuration
#######################################################################

echo "[5/8] Creating Docker Compose configuration..."

mkdir -p /var/lib/graphdb

cat > /var/lib/graphdb/docker-compose.yml <<'EOF'
version: '3.8'

services:
  graphdb:
    image: ghcr.io/dd0wney/graphdb:latest
    container_name: graphdb
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /var/lib/graphdb/data:/data
      - /var/lib/graphdb/logs:/logs
    environment:
      - GRAPHDB_EDITION=community
      - PORT=8080
      - DATA_DIR=/data
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
EOF

echo "✓ Docker Compose configuration created"

#######################################################################
# Create Systemd Service
#######################################################################

echo "[6/8] Creating systemd service..."

cat > /etc/systemd/system/graphdb.service <<'EOF'
[Unit]
Description=GraphDB Graph Database
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/var/lib/graphdb
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable graphdb.service

echo "✓ Systemd service created and enabled"

#######################################################################
# Configure Firewall
#######################################################################

echo "[7/8] Configuring firewall..."

# Default firewall rules
ufw default deny incoming
ufw default allow outgoing

# Allow SSH
ufw allow 22/tcp

# Allow GraphDB API
ufw allow 8080/tcp

# Allow node_exporter (optional, for monitoring)
ufw allow 9100/tcp

# Enable UFW (non-interactive)
echo "y" | ufw enable

echo "✓ Firewall configured"

#######################################################################
# Configure Automatic Updates
#######################################################################

echo "[8/8] Configuring automatic security updates..."

cat > /etc/apt/apt.conf.d/50unattended-upgrades <<'EOF'
Unattended-Upgrade::Allowed-Origins {
    "${distro_id}:${distro_codename}-security";
    "${distro_id}ESMApps:${distro_codename}-apps-security";
    "${distro_id}ESM:${distro_codename}-infra-security";
};

Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::MinimalSteps "true";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "false";
EOF

# Enable automatic updates
cat > /etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF

echo "✓ Automatic updates configured"

#######################################################################
# Create Welcome Documentation
#######################################################################

mkdir -p /root/graphdb-docs

cat > /root/graphdb-docs/README.txt <<'EOF'
========================================
Welcome to GraphDB on DigitalOcean!
========================================

GraphDB is now installed and will start automatically on boot.

GETTING STARTED
---------------

1. Check GraphDB status:
   systemctl status graphdb

2. View logs:
   docker logs -f graphdb

3. Test the API:
   curl http://localhost:8080/health

4. Access the API:
   GraphDB API: http://YOUR_DROPLET_IP:8080

MANAGEMENT COMMANDS
-------------------

Start GraphDB:
  systemctl start graphdb

Stop GraphDB:
  systemctl stop graphdb

Restart GraphDB:
  systemctl restart graphdb

View logs:
  docker logs -f graphdb

BACKUP & RESTORE
----------------

Create backup:
  /usr/local/bin/backup-graphdb.sh

Restore from backup:
  BACKUP_FILE=/path/to/backup.tar.gz /usr/local/bin/restore-graphdb.sh

Test disaster recovery:
  DROPLET_IP=localhost /usr/local/bin/test-dr.sh

DOCUMENTATION
-------------

Full documentation: https://github.com/dd0wney/graphdb
API documentation: http://YOUR_DROPLET_IP:8080/docs

SUPPORT
-------

GitHub Issues: https://github.com/dd0wney/graphdb/issues
Community: https://discord.gg/graphdb (coming soon)

========================================
EOF

cat > /root/graphdb-docs/QUICK-START.md <<'EOF'
# GraphDB Quick Start

## Your First Query

```bash
# Create a node
curl -X POST http://localhost:8080/api/v1/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user",
    "properties": {
      "name": "Alice",
      "email": "alice@example.com"
    }
  }'

# Create another node
curl -X POST http://localhost:8080/api/v1/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "type": "user",
    "properties": {
      "name": "Bob",
      "email": "bob@example.com"
    }
  }'

# Create a relationship
curl -X POST http://localhost:8080/api/v1/edges \
  -H "Content-Type: application/json" \
  -d '{
    "type": "TRUSTS",
    "source": "node-id-1",
    "target": "node-id-2",
    "properties": {
      "score": 0.95
    }
  }'

# Query nodes
curl http://localhost:8080/api/v1/nodes?type=user&limit=10
```

## Using the Client SDK

```bash
npm install @graphdb/client
```

```typescript
import { GraphDBClient } from '@graphdb/client';

const client = new GraphDBClient({
  endpoint: 'http://YOUR_DROPLET_IP:8080'
});

const nodes = await client.getNodes({ type: 'user', limit: 10 });
```

## Next Steps

1. Set up authentication (API keys)
2. Configure backups to DigitalOcean Spaces
3. Set up monitoring with Prometheus + Grafana
4. Review the DR runbook: /root/graphdb-docs/DISASTER-RECOVERY.md

Happy graphing!
EOF

echo "✓ Documentation created at /root/graphdb-docs/"

#######################################################################
# Installation Complete
#######################################################################

echo ""
echo "========================================="
echo "GraphDB Installation Complete!"
echo "========================================="
echo ""
echo "GraphDB will start automatically on first boot."
echo ""
echo "Documentation:"
echo "  /root/graphdb-docs/README.txt"
echo "  /root/graphdb-docs/QUICK-START.md"
echo ""
echo "Management commands:"
echo "  systemctl status graphdb"
echo "  docker logs -f graphdb"
echo "  /usr/local/bin/backup-graphdb.sh"
echo ""
echo "========================================="
