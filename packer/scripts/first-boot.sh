#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB First Boot Configuration
#
# This script runs once on the first boot of a GraphDB droplet created
# from the marketplace image. It handles instance-specific configuration
# that cannot be done at image build time.
#
# Tasks:
# - Generate unique instance ID
# - Configure host-specific settings
# - Start GraphDB service
# - Display welcome message
# - Disable itself after first run
#
#######################################################################

FIRST_BOOT_FLAG="/var/lib/graphdb/.first-boot-complete"

# Skip if already run
if [ -f "$FIRST_BOOT_FLAG" ]; then
    echo "First boot already completed, skipping..."
    exit 0
fi

echo "========================================="
echo "GraphDB First Boot Configuration"
echo "========================================="

#######################################################################
# Generate Instance ID
#######################################################################

echo "[1/5] Generating unique instance ID..."

INSTANCE_ID=$(cat /proc/sys/kernel/random/uuid)
echo "$INSTANCE_ID" > /var/lib/graphdb/instance-id

echo "Instance ID: $INSTANCE_ID"

#######################################################################
# Configure Hostname
#######################################################################

echo "[2/5] Configuring hostname..."

# Get droplet metadata (DigitalOcean specific)
if command -v curl &> /dev/null; then
    DROPLET_ID=$(curl -s http://169.254.169.254/metadata/v1/id 2>/dev/null || echo "unknown")
    DROPLET_IP=$(curl -s http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address 2>/dev/null || echo "unknown")

    if [ "$DROPLET_ID" != "unknown" ]; then
        echo "Droplet ID: $DROPLET_ID"
        echo "Public IP: $DROPLET_IP"

        # Save metadata
        cat > /var/lib/graphdb/droplet-metadata.json <<EOF
{
  "droplet_id": "$DROPLET_ID",
  "public_ip": "$DROPLET_IP",
  "instance_id": "$INSTANCE_ID",
  "first_boot": "$(date -Iseconds)"
}
EOF
    fi
fi

#######################################################################
# Start GraphDB
#######################################################################

echo "[3/5] Starting GraphDB service..."

systemctl start graphdb

# Wait for GraphDB to be healthy
echo "Waiting for GraphDB to be ready..."
max_attempts=30
attempt=0

while [ $attempt -lt $max_attempts ]; do
    if curl -f http://localhost:8080/health &>/dev/null; then
        echo "✓ GraphDB is healthy"
        break
    fi

    attempt=$((attempt + 1))
    if [ $attempt -ge $max_attempts ]; then
        echo "Warning: GraphDB health check timeout (this may be normal on first boot)"
        break
    fi

    sleep 2
done

#######################################################################
# Display Welcome Message
#######################################################################

echo "[4/5] Configuring welcome message..."

PUBLIC_IP=$(curl -s http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address 2>/dev/null || echo "YOUR_DROPLET_IP")

cat > /etc/update-motd.d/99-graphdb <<EOF
#!/bin/bash
cat <<'WELCOME'

  ╔═══════════════════════════════════════════════════════╗
  ║                                                       ║
  ║   ██████╗ ██████╗  █████╗ ██████╗ ██╗  ██╗██████╗   ║
  ║  ██╔════╝ ██╔══██╗██╔══██╗██╔══██╗██║  ██║██╔══██╗  ║
  ║  ██║  ███╗██████╔╝███████║██████╔╝███████║██║  ██║  ║
  ║  ██║   ██║██╔══██╗██╔══██║██╔═══╝ ██╔══██║██║  ██║  ║
  ║  ╚██████╔╝██║  ██║██║  ██║██║     ██║  ██║██████╔╝  ║
  ║   ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝╚═════╝   ║
  ║                                                       ║
  ║   High-Performance Graph Database                    ║
  ║                                                       ║
  ╚═══════════════════════════════════════════════════════╝

GraphDB is running and accessible at:

  API:    http://${PUBLIC_IP}:8080
  Health: http://${PUBLIC_IP}:8080/health
  Docs:   http://${PUBLIC_IP}:8080/docs

Quick Commands:

  Status:  systemctl status graphdb
  Logs:    docker logs -f graphdb
  Backup:  /usr/local/bin/backup-graphdb.sh

Documentation: /root/graphdb-docs/README.txt

Get started: cat /root/graphdb-docs/QUICK-START.md

WELCOME
EOF

chmod +x /etc/update-motd.d/99-graphdb

#######################################################################
# Mark First Boot Complete
#######################################################################

echo "[5/5] Marking first boot complete..."

touch "$FIRST_BOOT_FLAG"

# Disable this service so it doesn't run again
systemctl disable graphdb-first-boot.service

echo ""
echo "========================================="
echo "GraphDB First Boot Complete!"
echo "========================================="
echo ""
echo "GraphDB is now running at:"
echo "  http://${PUBLIC_IP}:8080"
echo ""
echo "View documentation:"
echo "  cat /root/graphdb-docs/README.txt"
echo ""
echo "========================================="
