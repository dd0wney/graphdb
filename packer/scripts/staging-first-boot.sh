#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Staging First-Boot Configuration
#
# This script runs once on first boot to configure the instance.
# It sets up:
# - Volume mount detection and validation
# - GraphDB configuration
# - Permissions
# - Health checks
#
# This script is run by systemd: graphdb-first-boot.service
#######################################################################

LOGFILE="/var/log/graphdb-first-boot.log"
COMPLETION_MARKER="/var/lib/graphdb/.first-boot-complete"

exec > >(tee -a "$LOGFILE") 2>&1

echo "======================================================================"
echo "GraphDB First-Boot Configuration"
echo "Started: $(date)"
echo "======================================================================"

#######################################################################
# Function: Check if volume is mounted
#######################################################################

check_volume_mounted() {
    if mountpoint -q /mnt/graphdb-data; then
        echo "✓ Volume is mounted at /mnt/graphdb-data"
        return 0
    else
        echo "⚠ Volume not mounted at /mnt/graphdb-data"
        echo ""
        echo "MANUAL ACTION REQUIRED:"
        echo "1. Attach a 100GB volume via DigitalOcean dashboard"
        echo "2. Run the following commands:"
        echo ""
        echo "   mkfs.ext4 -F /dev/disk/by-id/scsi-0DO_Volume_graphdb-data"
        echo "   mount -o discard,defaults /dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data"
        echo "   echo '/dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data ext4 defaults,nofail,discard 0 2' >> /etc/fstab"
        echo ""
        echo "3. Run: systemctl restart graphdb-first-boot"
        echo ""
        return 1
    fi
}

#######################################################################
# Function: Setup volume directories
#######################################################################

setup_volume_directories() {
    echo "Setting up volume directories..."

    mkdir -p /mnt/graphdb-data/data
    mkdir -p /mnt/graphdb-data/wal
    mkdir -p /mnt/graphdb-data/audit
    mkdir -p /mnt/graphdb-data/backups

    chown -R graphdb:graphdb /mnt/graphdb-data
    chmod 750 /mnt/graphdb-data

    echo "✓ Volume directories created"
}

#######################################################################
# Function: Install GraphDB configuration
#######################################################################

install_config() {
    echo "Installing GraphDB configuration..."

    if [ ! -f /etc/graphdb/config.yaml ]; then
        cp /etc/graphdb/config.yaml.template /etc/graphdb/config.yaml
        chown graphdb:graphdb /etc/graphdb/config.yaml
        chmod 640 /etc/graphdb/config.yaml
        echo "✓ GraphDB configuration installed"
    else
        echo "✓ GraphDB configuration already exists"
    fi
}

#######################################################################
# Function: Get instance metadata
#######################################################################

get_instance_info() {
    echo "Retrieving instance metadata..."

    # DigitalOcean metadata service
    DROPLET_ID=$(curl -s http://169.254.169.254/metadata/v1/id || echo "unknown")
    REGION=$(curl -s http://169.254.169.254/metadata/v1/region || echo "unknown")
    PUBLIC_IPV4=$(curl -s http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address || echo "unknown")

    echo "  Droplet ID: $DROPLET_ID"
    echo "  Region: $REGION"
    echo "  Public IP: $PUBLIC_IPV4"

    # Save for later use
    cat > /var/lib/graphdb/instance-info.txt <<EOF
DROPLET_ID=$DROPLET_ID
REGION=$REGION
PUBLIC_IPV4=$PUBLIC_IPV4
FIRST_BOOT=$(date -Iseconds)
EOF
}

#######################################################################
# Function: Display next steps
#######################################################################

display_next_steps() {
    echo ""
    echo "======================================================================"
    echo "GraphDB First-Boot Configuration Complete!"
    echo "======================================================================"
    echo ""
    echo "NEXT STEPS:"
    echo ""
    echo "1. Configure Cloudflare Tunnel:"
    echo "   cloudflared tunnel login"
    echo "   cloudflared tunnel create graphdb-staging"
    echo "   # Edit /etc/cloudflared/config.yaml with tunnel credentials"
    echo "   systemctl start cloudflared"
    echo "   systemctl enable cloudflared"
    echo ""
    echo "2. Start GraphDB:"
    echo "   systemctl start graphdb"
    echo "   systemctl status graphdb"
    echo ""
    echo "3. Test API:"
    echo "   curl http://localhost:8080/health"
    echo ""
    echo "4. View logs:"
    echo "   journalctl -u graphdb -f"
    echo ""
    echo "5. Configure Syntopica Workers:"
    echo "   cd syntopica-v2/workers"
    echo "   npx wrangler secret put GRAPHDB_URL"
    echo "   # Enter: https://graphdb-staging.yourdomain.com"
    echo ""
    echo "6. Run initial sync:"
    echo "   curl -X POST https://staging.yourdomain.com/api/internal/sync/batch/all \\"
    echo "     -H \"Authorization: Bearer \$ADMIN_API_KEY\""
    echo ""
    echo "======================================================================"
    echo "Full documentation: /root/GRAPHDB-README.md"
    echo "======================================================================"
    echo ""
}

#######################################################################
# Main Execution
#######################################################################

main() {
    # Check if already completed
    if [ -f "$COMPLETION_MARKER" ]; then
        echo "First-boot already completed. Skipping."
        exit 0
    fi

    # Step 1: Check volume
    if ! check_volume_mounted; then
        echo "ERROR: Volume not mounted. Please mount volume and restart this service."
        exit 1
    fi

    # Step 2: Setup directories
    setup_volume_directories

    # Step 3: Install configuration
    install_config

    # Step 4: Get instance info
    get_instance_info

    # Step 5: Mark as complete
    mkdir -p /var/lib/graphdb
    touch "$COMPLETION_MARKER"

    # Step 6: Display next steps
    display_next_steps

    echo "First-boot configuration completed successfully!"
    echo "Completed: $(date)"
}

# Run main function
main

exit 0
