#!/bin/bash
set -euo pipefail

#######################################################################
# Cleanup Script for Packer Build
#
# Removes temporary files, logs, and sensitive data before creating
# the marketplace snapshot. Required for DO Marketplace submission.
#
#######################################################################

echo "========================================="
echo "Cleanup for Marketplace Snapshot"
echo "========================================="

#######################################################################
# Remove Temporary Files
#######################################################################

echo "[1/8] Removing temporary files..."

rm -rf /tmp/*
rm -rf /var/tmp/*
rm -rf /root/.cache/*

echo "✓ Temporary files removed"

#######################################################################
# Clear Logs
#######################################################################

echo "[2/8] Clearing logs..."

# Truncate log files but keep them
find /var/log -type f -name "*.log" -exec truncate -s 0 {} \;
find /var/log -type f -name "*.gz" -delete
find /var/log -type f -name "*.1" -delete

# Clear journal
journalctl --vacuum-time=1s

echo "✓ Logs cleared"

#######################################################################
# Remove SSH Host Keys (will be regenerated on first boot)
#######################################################################

echo "[3/8] Removing SSH host keys..."

rm -f /etc/ssh/ssh_host_*

# SSH will regenerate keys on first boot
cat > /etc/systemd/system/regenerate-ssh-keys.service <<'EOF'
[Unit]
Description=Regenerate SSH host keys
Before=ssh.service
ConditionPathExists=|!/etc/ssh/ssh_host_rsa_key
ConditionPathExists=|!/etc/ssh/ssh_host_ecdsa_key
ConditionPathExists=|!/etc/ssh/ssh_host_ed25519_key

[Service]
Type=oneshot
ExecStart=/usr/bin/ssh-keygen -A
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

systemctl enable regenerate-ssh-keys.service

echo "✓ SSH host keys will be regenerated on first boot"

#######################################################################
# Remove Cloud-Init Artifacts
#######################################################################

echo "[4/8] Removing cloud-init artifacts..."

rm -rf /var/lib/cloud/instances/*
rm -rf /var/lib/cloud/instance

echo "✓ Cloud-init artifacts removed"

#######################################################################
# Clear Package Cache
#######################################################################

echo "[5/8] Clearing package cache..."

apt-get clean
apt-get autoclean
rm -rf /var/lib/apt/lists/*

echo "✓ Package cache cleared"

#######################################################################
# Remove Build Artifacts
#######################################################################

echo "[6/8] Removing build artifacts..."

# Remove any leftover files from Packer build
rm -f /root/.wget-hsts
rm -f /root/.bash_history
rm -f /root/.viminfo

echo "✓ Build artifacts removed"

#######################################################################
# Clear Machine ID (will be regenerated)
#######################################################################

echo "[7/8] Clearing machine ID..."

# Truncate machine-id (DO will regenerate)
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id
ln -s /etc/machine-id /var/lib/dbus/machine-id

echo "✓ Machine ID will be regenerated"

#######################################################################
# Zero Out Free Space (Optional - Improves Compression)
#######################################################################

echo "[8/8] Preparing for snapshot..."

# This helps compress the snapshot better
# Commented out by default as it takes time, uncomment if needed
# dd if=/dev/zero of=/EMPTY bs=1M || true
# rm -f /EMPTY

sync

echo "✓ Ready for snapshot"

echo ""
echo "========================================="
echo "Cleanup Complete"
echo "========================================="
echo ""
echo "Image is ready for marketplace snapshot."
echo "All sensitive data and temporary files removed."
echo ""
