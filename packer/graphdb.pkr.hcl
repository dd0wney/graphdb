#######################################################################
# GraphDB DigitalOcean Marketplace Packer Template
#
# This template builds a 1-click deployable DigitalOcean Droplet image
# for the GraphDB graph database.
#
# Features:
# - Pre-installed GraphDB with all dependencies
# - Docker and Docker Compose ready
# - Automatic service start on boot
# - Health monitoring configured
# - Backup scripts pre-installed
# - Marketplace-ready first-boot configuration
#
# Requirements:
# - Packer 1.8+ installed
# - DigitalOcean API token set in environment
# - doctl CLI configured (for snapshot management)
#
# Usage:
#   # Set DO API token
#   export DIGITALOCEAN_API_TOKEN="your-token-here"
#
#   # Build the image
#   packer build graphdb.pkr.hcl
#
#   # Test the snapshot
#   doctl compute droplet create graphdb-test \
#     --image <snapshot-id> \
#     --size s-2vcpu-4gb \
#     --region nyc3
#
# Variables:
#   - do_api_token: DigitalOcean API token
#   - region: Build region (default: nyc3)
#   - size: Temporary build droplet size (default: s-1vcpu-2gb)
#   - graphdb_version: GraphDB version to install (default: latest)
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
  default     = "nyc3"
}

variable "size" {
  type        = string
  description = "Droplet size for build (temporary)"
  default     = "s-1vcpu-2gb"
}

variable "graphdb_version" {
  type        = string
  description = "GraphDB version to install"
  default     = "latest"
}

variable "snapshot_name" {
  type        = string
  description = "Name for the snapshot"
  default     = "graphdb-marketplace-{{timestamp}}"
}

#######################################################################
# DigitalOcean Builder
#######################################################################

source "digitalocean" "graphdb" {
  api_token     = var.do_api_token
  image         = "ubuntu-22-04-x64"
  region        = var.region
  size          = var.size
  ssh_username  = "root"
  snapshot_name = var.snapshot_name

  # Marketplace requirements
  snapshot_regions = [
    "nyc1",
    "nyc3",
    "sfo3",
    "ams3",
    "sgp1",
    "lon1",
    "fra1",
    "tor1",
    "blr1"
  ]

  # Tags for organization
  tags = [
    "graphdb",
    "marketplace",
    "graph-database"
  ]
}

#######################################################################
# Build Configuration
#######################################################################

build {
  name    = "graphdb-marketplace"
  sources = ["source.digitalocean.graphdb"]

  # Update system packages
  provisioner "shell" {
    inline = [
      "export DEBIAN_FRONTEND=noninteractive",
      "apt-get update",
      "apt-get upgrade -y",
      "apt-get clean"
    ]
  }

  # Install GraphDB and dependencies
  provisioner "shell" {
    script = "scripts/install-graphdb.sh"
    environment_vars = [
      "GRAPHDB_VERSION=${var.graphdb_version}"
    ]
  }

  # Copy backup and DR scripts
  provisioner "file" {
    source      = "../scripts/backup-graphdb.sh"
    destination = "/usr/local/bin/backup-graphdb.sh"
  }

  provisioner "file" {
    source      = "../scripts/restore-graphdb.sh"
    destination = "/usr/local/bin/restore-graphdb.sh"
  }

  provisioner "file" {
    source      = "../scripts/test-dr.sh"
    destination = "/usr/local/bin/test-dr.sh"
  }

  # Make scripts executable
  provisioner "shell" {
    inline = [
      "chmod +x /usr/local/bin/backup-graphdb.sh",
      "chmod +x /usr/local/bin/restore-graphdb.sh",
      "chmod +x /usr/local/bin/test-dr.sh"
    ]
  }

  # Copy first-boot configuration script
  provisioner "file" {
    source      = "scripts/first-boot.sh"
    destination = "/usr/local/bin/graphdb-first-boot.sh"
  }

  provisioner "shell" {
    inline = [
      "chmod +x /usr/local/bin/graphdb-first-boot.sh"
    ]
  }

  # Set up first-boot service
  provisioner "file" {
    source      = "scripts/graphdb-first-boot.service"
    destination = "/etc/systemd/system/graphdb-first-boot.service"
  }

  provisioner "shell" {
    inline = [
      "systemctl enable graphdb-first-boot.service"
    ]
  }

  # Copy MOTD (Message of the Day)
  provisioner "file" {
    source      = "scripts/motd.txt"
    destination = "/etc/motd"
  }

  # Create GraphDB directories with proper permissions
  provisioner "shell" {
    inline = [
      "mkdir -p /var/lib/graphdb/{data,backups,logs}",
      "mkdir -p /etc/graphdb",
      "chmod 755 /var/lib/graphdb",
      "chmod 755 /var/lib/graphdb/data",
      "chmod 755 /var/lib/graphdb/backups",
      "chmod 755 /var/lib/graphdb/logs"
    ]
  }

  # Security hardening
  provisioner "shell" {
    script = "scripts/security-hardening.sh"
  }

  # Cleanup for marketplace submission
  provisioner "shell" {
    script = "scripts/cleanup.sh"
  }

  # Verify installation
  provisioner "shell" {
    script = "scripts/verify-installation.sh"
  }

  # Final system cleanup
  provisioner "shell" {
    inline = [
      "apt-get autoremove -y",
      "apt-get autoclean -y",
      "rm -rf /tmp/*",
      "rm -rf /var/tmp/*",
      "history -c"
    ]
  }
}

#######################################################################
# Post-processors (optional)
#######################################################################

# Generate manifest with snapshot details
post-processor "manifest" {
  output     = "manifest.json"
  strip_path = true
}
