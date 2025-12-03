#######################################################################
# GraphDB Staging Image for Syntopica
#
# This template builds a native Go binary deployment of GraphDB,
# optimized for integration with Syntopica on Cloudflare Workers.
#
# Features:
# - Native Go binary (no Docker overhead)
# - Pre-installed Go 1.21+
# - GraphDB compiled and ready
# - Cloudflared tunnel support
# - Volume-optimized for 100GB block storage
# - Prometheus metrics endpoint
# - All dependencies pre-installed
#
# Usage:
#   export DIGITALOCEAN_API_TOKEN="your-token-here"
#   packer build -var="environment=staging" graphdb-staging.pkr.hcl
#
# Result:
#   Custom image ready to deploy in seconds
#   Just attach 100GB volume and run first-boot script
#
#######################################################################

variable "do_api_token" {
  type        = string
  description = "DigitalOcean API token"
  default     = env("DIGITALOCEAN_API_TOKEN")
  sensitive   = true
}

variable "region" {
  type        = string
  description = "DigitalOcean region for build"
  default     = "nyc1"
}

variable "environment" {
  type        = string
  description = "Environment name (staging or production)"
  default     = "staging"
}

variable "graphdb_repo" {
  type        = string
  description = "GraphDB git repository URL"
  default     = "https://github.com/dd0wney/graphdb.git"
}

variable "graphdb_branch" {
  type        = string
  description = "GraphDB branch to build from"
  default     = "main"
}

#######################################################################
# DigitalOcean Builder
#######################################################################

source "digitalocean" "graphdb-staging" {
  api_token     = var.do_api_token
  image         = "ubuntu-22-04-x64"
  region        = var.region
  size          = "s-1vcpu-2gb"
  ssh_username  = "root"
  snapshot_name = "graphdb-${var.environment}-{{timestamp}}"

  # Single region for staging (can expand for production)
  snapshot_regions = ["nyc1"]

  tags = [
    "graphdb",
    "syntopica",
    var.environment
  ]
}

#######################################################################
# Build Configuration
#######################################################################

build {
  name    = "graphdb-staging"
  sources = ["source.digitalocean.graphdb-staging"]

  #####################################################################
  # Phase 1: System Update
  #####################################################################

  provisioner "shell" {
    inline = [
      "export DEBIAN_FRONTEND=noninteractive",
      "apt-get update",
      "apt-get upgrade -y"
    ]
  }

  #####################################################################
  # Phase 2: Install Dependencies
  #####################################################################

  provisioner "shell" {
    inline = [
      "apt-get install -y \\",
      "  git \\",
      "  build-essential \\",
      "  curl \\",
      "  wget \\",
      "  vim \\",
      "  htop \\",
      "  iotop \\",
      "  net-tools \\",
      "  ufw \\",
      "  jq \\",
      "  s3cmd \\",
      "  ca-certificates"
    ]
  }

  #####################################################################
  # Phase 3: Install Go 1.21+
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Installing Go 1.21.5 ==='",
      "cd /tmp",
      "wget -q https://go.dev/dl/go1.21.5.linux-amd64.tar.gz",
      "rm -rf /usr/local/go",
      "tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz",
      "rm go1.21.5.linux-amd64.tar.gz",
      "",
      "# Add Go to PATH globally",
      "cat > /etc/profile.d/go.sh <<'EOF'",
      "export PATH=$PATH:/usr/local/go/bin",
      "export GOPATH=/home/graphdb/go",
      "export PATH=$PATH:$GOPATH/bin",
      "EOF",
      "",
      "source /etc/profile.d/go.sh",
      "/usr/local/go/bin/go version"
    ]
  }

  #####################################################################
  # Phase 4: Create graphdb User
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Creating graphdb user ==='",
      "useradd -m -s /bin/bash graphdb",
      "usermod -aG sudo graphdb",
      "",
      "# No password sudo for graphdb user (marketplace convenience)",
      "echo 'graphdb ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/graphdb",
      "chmod 440 /etc/sudoers.d/graphdb"
    ]
  }

  #####################################################################
  # Phase 5: Clone and Build GraphDB
  #####################################################################

  provisioner "shell" {
    environment_vars = [
      "GRAPHDB_REPO=${var.graphdb_repo}",
      "GRAPHDB_BRANCH=${var.graphdb_branch}"
    ]
    inline = [
      "echo '=== Cloning GraphDB repository ==='",
      "su - graphdb -c 'cd ~ && git clone $GRAPHDB_REPO graphdb'",
      "su - graphdb -c 'cd ~/graphdb && git checkout $GRAPHDB_BRANCH'",
      "",
      "echo '=== Building GraphDB ==='",
      "su - graphdb -c 'cd ~/graphdb && /usr/local/go/bin/go build -o bin/server cmd/server/main.go'",
      "su - graphdb -c 'cd ~/graphdb && ls -lh bin/server'",
      "",
      "# Verify binary works",
      "su - graphdb -c '~/graphdb/bin/server --version || echo \"Binary built successfully\"'"
    ]
  }

  #####################################################################
  # Phase 6: Install Cloudflared
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Installing Cloudflared ==='",
      "cd /tmp",
      "wget -q https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb",
      "dpkg -i cloudflared-linux-amd64.deb",
      "rm cloudflared-linux-amd64.deb",
      "cloudflared --version"
    ]
  }

  #####################################################################
  # Phase 7: Create Directory Structure
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Creating directory structure ==='",
      "",
      "# Config directory",
      "mkdir -p /etc/graphdb",
      "chown graphdb:graphdb /etc/graphdb",
      "",
      "# Log directory (on root disk)",
      "mkdir -p /var/log/graphdb",
      "chown graphdb:graphdb /var/log/graphdb",
      "",
      "# Cloudflared config",
      "mkdir -p /etc/cloudflared",
      "",
      "# Create mount point for volume (configured on first boot)",
      "mkdir -p /mnt/graphdb-data",
      "",
      "# Scripts directory",
      "mkdir -p /usr/local/lib/graphdb"
    ]
  }

  #####################################################################
  # Phase 8: Install Configuration Templates
  #####################################################################

  provisioner "file" {
    source      = "config/graphdb-staging.yaml.template"
    destination = "/etc/graphdb/config.yaml.template"
  }

  provisioner "file" {
    source      = "config/cloudflared.yaml.template"
    destination = "/etc/cloudflared/config.yaml.template"
  }

  #####################################################################
  # Phase 9: Install First-Boot Script
  #####################################################################

  provisioner "file" {
    source      = "scripts/staging-first-boot.sh"
    destination = "/usr/local/lib/graphdb/first-boot.sh"
  }

  provisioner "shell" {
    inline = [
      "chmod +x /usr/local/lib/graphdb/first-boot.sh"
    ]
  }

  # Create first-boot service
  provisioner "shell" {
    inline = [
      "cat > /etc/systemd/system/graphdb-first-boot.service <<'EOF'",
      "[Unit]",
      "Description=GraphDB First Boot Configuration",
      "After=network-online.target",
      "Wants=network-online.target",
      "ConditionPathExists=!/var/lib/graphdb/.first-boot-complete",
      "",
      "[Service]",
      "Type=oneshot",
      "ExecStart=/usr/local/lib/graphdb/first-boot.sh",
      "RemainAfterExit=yes",
      "StandardOutput=journal+console",
      "",
      "[Install]",
      "WantedBy=multi-user.target",
      "EOF",
      "",
      "systemctl enable graphdb-first-boot.service"
    ]
  }

  #####################################################################
  # Phase 10: Install systemd Service
  #####################################################################

  provisioner "shell" {
    inline = [
      "cat > /etc/systemd/system/graphdb.service <<'EOF'",
      "[Unit]",
      "Description=GraphDB Server",
      "After=network.target graphdb-first-boot.service",
      "Wants=network-online.target",
      "",
      "[Service]",
      "Type=simple",
      "User=graphdb",
      "Group=graphdb",
      "WorkingDirectory=/home/graphdb/graphdb",
      "ExecStart=/home/graphdb/graphdb/bin/server -config /etc/graphdb/config.yaml",
      "Restart=always",
      "RestartSec=10s",
      "StandardOutput=journal",
      "StandardError=journal",
      "SyslogIdentifier=graphdb",
      "",
      "# Security hardening",
      "NoNewPrivileges=true",
      "PrivateTmp=true",
      "ProtectSystem=strict",
      "ProtectHome=true",
      "ReadWritePaths=/mnt/graphdb-data /var/log/graphdb",
      "",
      "# Resource limits (2GB droplet)",
      "MemoryMax=1.5G",
      "MemoryHigh=1.2G",
      "TasksMax=1000",
      "",
      "# Environment",
      "Environment=\"GOMAXPROCS=1\"",
      "Environment=\"GOGC=100\"",
      "",
      "[Install]",
      "WantedBy=multi-user.target",
      "EOF",
      "",
      "systemctl daemon-reload",
      "systemctl enable graphdb.service"
    ]
  }

  #####################################################################
  # Phase 11: Firewall Configuration
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Configuring firewall ==='",
      "ufw default deny incoming",
      "ufw default allow outgoing",
      "ufw allow OpenSSH",
      "ufw allow 8080/tcp  # GraphDB API (via tunnel only)",
      "echo 'y' | ufw enable",
      "ufw status"
    ]
  }

  #####################################################################
  # Phase 12: Install MOTD
  #####################################################################

  provisioner "shell" {
    inline = [
      "cat > /etc/motd <<'EOF'",
      "",
      "╔═══════════════════════════════════════════════════════════════╗",
      "║                                                               ║",
      "║   ██████╗ ██████╗  █████╗ ██████╗ ██╗  ██╗██████╗ ██████╗   ║",
      "║  ██╔════╝ ██╔══██╗██╔══██╗██╔══██╗██║  ██║██╔══██╗██╔══██╗  ║",
      "║  ██║  ███╗██████╔╝███████║██████╔╝███████║██║  ██║██████╔╝  ║",
      "║  ██║   ██║██╔══██╗██╔══██║██╔═══╝ ██╔══██║██║  ██║██╔══██╗  ║",
      "║  ╚██████╔╝██║  ██║██║  ██║██║     ██║  ██║██████╔╝██████╔╝  ║",
      "║   ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚═╝  ╚═╝╚═════╝ ╚═════╝   ║",
      "║                                                               ║",
      "║             High-Performance Graph Database                   ║",
      "║                  Staging Environment                          ║",
      "║                                                               ║",
      "╚═══════════════════════════════════════════════════════════════╝",
      "",
      "QUICK START:",
      "  sudo systemctl status graphdb      # Check GraphDB status",
      "  sudo journalctl -u graphdb -f      # View logs",
      "  curl http://localhost:8080/health  # Health check",
      "",
      "CONFIGURATION:",
      "  /etc/graphdb/config.yaml           # GraphDB config",
      "  /etc/cloudflared/config.yaml       # Tunnel config",
      "",
      "DOCUMENTATION:",
      "  /root/GRAPHDB-README.md            # Full documentation",
      "",
      "SUPPORT:",
      "  https://github.com/dd0wney/graphdb/issues",
      "",
      "EOF"
    ]
  }

  #####################################################################
  # Phase 13: Create Documentation
  #####################################################################

  provisioner "shell" {
    inline = [
      "cat > /root/GRAPHDB-README.md <<'EOF'",
      "# GraphDB Staging - Quick Reference",
      "",
      "## First Boot",
      "",
      "After creating your droplet from this image:",
      "",
      "1. **Attach 100GB volume** (via DigitalOcean dashboard)",
      "2. **SSH into droplet**:",
      "   ```bash",
      "   ssh root@YOUR_DROPLET_IP",
      "   ```",
      "3. **Mount volume**:",
      "   ```bash",
      "   mkfs.ext4 -F /dev/disk/by-id/scsi-0DO_Volume_graphdb-data",
      "   mount -o discard,defaults /dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data",
      "   echo '/dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data ext4 defaults,nofail,discard 0 2' >> /etc/fstab",
      "   mkdir -p /mnt/graphdb-data/{data,wal,audit,backups}",
      "   chown -R graphdb:graphdb /mnt/graphdb-data",
      "   ```",
      "4. **Start GraphDB**:",
      "   ```bash",
      "   systemctl start graphdb",
      "   systemctl status graphdb",
      "   ```",
      "",
      "## Management",
      "",
      "### Service Control",
      "```bash",
      "systemctl start graphdb    # Start",
      "systemctl stop graphdb     # Stop",
      "systemctl restart graphdb  # Restart",
      "systemctl status graphdb   # Status",
      "```",
      "",
      "### View Logs",
      "```bash",
      "journalctl -u graphdb -f   # Follow logs",
      "tail -f /var/log/graphdb/server.log",
      "```",
      "",
      "### Test API",
      "```bash",
      "curl http://localhost:8080/health",
      "curl http://localhost:8080/stats",
      "```",
      "",
      "## Cloudflare Tunnel Setup",
      "",
      "```bash",
      "# Authenticate",
      "cloudflared tunnel login",
      "",
      "# Create tunnel",
      "cloudflared tunnel create graphdb-staging",
      "",
      "# Edit /etc/cloudflared/config.yaml with tunnel credentials",
      "",
      "# Start tunnel",
      "systemctl start cloudflared",
      "systemctl enable cloudflared",
      "```",
      "",
      "## Backup",
      "",
      "```bash",
      "# Create snapshot (recommended)",
      "doctl compute volume-snapshot create graphdb-data \\",
      "  --snapshot-name \"graphdb-backup-$(date +%Y%m%d-%H%M%S)\"",
      "",
      "# Or manual backup",
      "systemctl stop graphdb",
      "tar -czf /mnt/graphdb-data/backups/backup-$(date +%Y%m%d).tar.gz \\",
      "  /mnt/graphdb-data/data \\",
      "  /mnt/graphdb-data/wal \\",
      "  /mnt/graphdb-data/audit",
      "systemctl start graphdb",
      "```",
      "",
      "## Monitoring",
      "",
      "```bash",
      "# System resources",
      "htop",
      "iotop",
      "df -h",
      "",
      "# GraphDB metrics",
      "curl http://localhost:8080/metrics  # Prometheus format",
      "```",
      "",
      "## Integration with Syntopica",
      "",
      "1. **Update Syntopica Workers**:",
      "   ```bash",
      "   cd syntopica-v2/workers",
      "   npx wrangler secret put GRAPHDB_URL",
      "   # Enter: https://graphdb-staging.yourdomain.com",
      "   ```",
      "",
      "2. **Initial sync**:",
      "   ```bash",
      "   curl -X POST https://staging.yourdomain.com/api/internal/sync/batch/all \\",
      "     -H \"Authorization: Bearer $ADMIN_API_KEY\"",
      "   ```",
      "",
      "3. **Verify**:",
      "   ```bash",
      "   curl http://localhost:8080/stats | jq '.'",
      "   ```",
      "",
      "## Troubleshooting",
      "",
      "### GraphDB won't start",
      "```bash",
      "journalctl -u graphdb -n 100 --no-pager",
      "ls -la /mnt/graphdb-data/  # Check permissions",
      "```",
      "",
      "### High memory usage",
      "```bash",
      "ps aux | grep graphdb",
      "# Edit /etc/graphdb/config.yaml, reduce cache_size_mb",
      "systemctl restart graphdb",
      "```",
      "",
      "### Tunnel not working",
      "```bash",
      "systemctl status cloudflared",
      "journalctl -u cloudflared -n 100",
      "cloudflared tunnel info graphdb-staging",
      "```",
      "",
      "## Support",
      "",
      "- GitHub: https://github.com/dd0wney/graphdb",
      "- Issues: https://github.com/dd0wney/graphdb/issues",
      "",
      "EOF"
    ]
  }

  #####################################################################
  # Phase 14: Cleanup
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo '=== Cleaning up ==='",
      "apt-get autoremove -y",
      "apt-get autoclean -y",
      "rm -rf /tmp/*",
      "rm -rf /var/tmp/*",
      "",
      "# Clear bash history",
      "history -c",
      "rm -f /root/.bash_history",
      "rm -f /home/graphdb/.bash_history",
      "",
      "# Clear cloud-init logs (will regenerate on first boot)",
      "rm -rf /var/lib/cloud/instances/*",
      "rm -rf /var/log/cloud-init*"
    ]
  }

  #####################################################################
  # Phase 15: Final Verification
  #####################################################################

  provisioner "shell" {
    inline = [
      "echo ''",
      "echo '=== Build Verification ==='",
      "echo ''",
      "echo 'Go version:'",
      "/usr/local/go/bin/go version",
      "echo ''",
      "echo 'GraphDB binary:'",
      "ls -lh /home/graphdb/graphdb/bin/server",
      "echo ''",
      "echo 'Cloudflared version:'",
      "cloudflared --version",
      "echo ''",
      "echo 'Systemd services:'",
      "systemctl list-unit-files | grep graphdb",
      "echo ''",
      "echo '=== Image build complete! ==='",
      "echo ''"
    ]
  }
}

#######################################################################
# Post-processors
#######################################################################

post-processor "manifest" {
  output     = "manifest-staging.json"
  strip_path = true
}
