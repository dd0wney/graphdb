#!/bin/bash
set -euo pipefail

#######################################################################
# Security Hardening Script
#
# Applies security best practices for DigitalOcean Marketplace images.
# Based on DO Marketplace requirements and CIS benchmarks.
#
#######################################################################

echo "========================================="
echo "Security Hardening"
echo "========================================="

#######################################################################
# SSH Hardening
#######################################################################

echo "[1/6] Hardening SSH configuration..."

# Backup original sshd_config
cp /etc/ssh/sshd_config /etc/ssh/sshd_config.backup

# Apply security settings
cat >> /etc/ssh/sshd_config.d/99-graphdb-security.conf <<'EOF'
# GraphDB Security Hardening

# Disable root password login (key-based only)
PermitRootLogin prohibit-password

# Disable password authentication (force key-based)
PasswordAuthentication no
ChallengeResponseAuthentication no

# Disable empty passwords
PermitEmptyPasswords no

# Limit authentication attempts
MaxAuthTries 3

# Disconnect idle sessions
ClientAliveInterval 300
ClientAliveCountMax 2

# Disable X11 forwarding (not needed)
X11Forwarding no

# Use secure ciphers only
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com,aes256-ctr,aes192-ctr,aes128-ctr
MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512,hmac-sha2-256

# Log more details
LogLevel VERBOSE
EOF

echo "✓ SSH hardened"

#######################################################################
# Configure fail2ban
#######################################################################

echo "[2/6] Configuring fail2ban..."

# Enable fail2ban for SSH
cat > /etc/fail2ban/jail.d/sshd.conf <<'EOF'
[sshd]
enabled = true
port = ssh
filter = sshd
logpath = /var/log/auth.log
maxretry = 3
bantime = 3600
findtime = 600
EOF

systemctl enable fail2ban
systemctl restart fail2ban

echo "✓ fail2ban configured"

#######################################################################
# Kernel Security Parameters
#######################################################################

echo "[3/6] Applying kernel security parameters..."

cat > /etc/sysctl.d/99-graphdb-security.conf <<'EOF'
# GraphDB Security Hardening

# Prevent IP spoofing
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1

# Disable IP source routing
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.default.accept_source_route = 0

# Ignore ICMP redirects
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0

# Ignore ICMP ping requests (optional, can disable if needed)
# net.ipv4.icmp_echo_ignore_all = 1

# Enable TCP SYN cookies (DDoS protection)
net.ipv4.tcp_syncookies = 1

# Log suspicious packets
net.ipv4.conf.all.log_martians = 1
net.ipv4.conf.default.log_martians = 1

# Disable IPv6 (if not needed)
# net.ipv6.conf.all.disable_ipv6 = 1
# net.ipv6.conf.default.disable_ipv6 = 1

# Increase security
kernel.dmesg_restrict = 1
kernel.kptr_restrict = 2
EOF

sysctl -p /etc/sysctl.d/99-graphdb-security.conf

echo "✓ Kernel security parameters applied"

#######################################################################
# File Permissions
#######################################################################

echo "[4/6] Setting secure file permissions..."

# Secure sensitive files
chmod 600 /etc/ssh/sshd_config
chmod 600 /root/.ssh/authorized_keys 2>/dev/null || true
chmod 700 /root/.ssh 2>/dev/null || true

# Secure log files
chmod 640 /var/log/auth.log
chmod 640 /var/log/syslog

echo "✓ File permissions secured"

#######################################################################
# Remove Unnecessary Packages
#######################################################################

echo "[5/6] Removing unnecessary packages..."

# Remove packages that aren't needed
apt-get autoremove --purge -y \
  snapd \
  lxd \
  lxd-client \
  2>/dev/null || true

echo "✓ Unnecessary packages removed"

#######################################################################
# Configure Automatic Security Updates
#######################################################################

echo "[6/6] Verifying automatic security updates..."

# Already configured in install-graphdb.sh, just verify
if [ -f /etc/apt/apt.conf.d/50unattended-upgrades ]; then
    echo "✓ Automatic security updates configured"
else
    echo "⚠ Automatic updates not found (should be configured in install script)"
fi

echo ""
echo "========================================="
echo "Security Hardening Complete"
echo "========================================="
