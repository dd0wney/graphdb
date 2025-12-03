#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB One-Command Deployment to Digital Ocean + Cloudflare
#
# This script automates the entire deployment process:
# - Creates Digital Ocean droplet
# - Installs Docker and dependencies
# - Sets up Cloudflare Tunnel
# - Deploys GraphDB with monitoring
# - Configures SSL/TLS automatically
#
# Prerequisites:
# - doctl (Digital Ocean CLI) configured
# - cloudflared (Cloudflare CLI) installed locally
# - Environment variables set (see below)
#
# Required Environment Variables:
#   CLOUDFLARE_API_TOKEN - Cloudflare API token
#   CLOUDFLARE_ACCOUNT_ID - Cloudflare account ID
#   CLOUDFLARE_ZONE_ID - Cloudflare zone ID (for your domain)
#   DOMAIN - Your domain name (e.g., graphdb.example.com)
#   DO_SIZE - Droplet size (default: s-2vcpu-4gb)
#   GRAPHDB_EDITION - community or enterprise (default: community)
#
# Optional:
#   GRAPHDB_LICENSE_KEY - Enterprise license key (if using enterprise)
#
# Usage:
#   export CLOUDFLARE_API_TOKEN="your-token"
#   export CLOUDFLARE_ACCOUNT_ID="your-account-id"
#   export CLOUDFLARE_ZONE_ID="your-zone-id"
#   export DOMAIN="graphdb.example.com"
#   bash deploy.sh
#
#######################################################################

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    local missing=0

    if ! command -v doctl &> /dev/null; then
        log_error "doctl not found. Install: https://docs.digitalocean.com/reference/doctl/how-to/install/"
        missing=1
    fi

    if ! command -v cloudflared &> /dev/null; then
        log_error "cloudflared not found. Install: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/"
        missing=1
    fi

    # Check required environment variables
    for var in CLOUDFLARE_API_TOKEN CLOUDFLARE_ACCOUNT_ID CLOUDFLARE_ZONE_ID DOMAIN; do
        if [ -z "${!var:-}" ]; then
            log_error "Missing required environment variable: $var"
            missing=1
        fi
    done

    if [ $missing -eq 1 ]; then
        log_error "Prerequisites check failed. Please install missing tools and set environment variables."
        exit 1
    fi

    log_info "Prerequisites check passed ✓"
}

# Configuration
DROPLET_NAME="${DROPLET_NAME:-graphdb-production}"
DO_REGION="${DO_REGION:-nyc3}"
DO_SIZE="${DO_SIZE:-s-2vcpu-4gb}"
DO_IMAGE="${DO_IMAGE:-ubuntu-22-04-x64}"
GRAPHDB_EDITION="${GRAPHDB_EDITION:-community}"
SSH_KEY_NAME="${SSH_KEY_NAME:-graphdb-deploy}"

echo "========================================="
echo "GraphDB Deployment to Digital Ocean"
echo "========================================="
echo "Droplet: $DROPLET_NAME"
echo "Region: $DO_REGION"
echo "Size: $DO_SIZE"
echo "Edition: $GRAPHDB_EDITION"
echo "Domain: $DOMAIN"
echo "========================================="
echo ""

# Run prerequisite check
check_prerequisites

# Step 1: Create SSH key if it doesn't exist
log_info "[1/10] Setting up SSH key..."
if [ ! -f ~/.ssh/graphdb_deploy ]; then
    ssh-keygen -t ed25519 -f ~/.ssh/graphdb_deploy -N "" -C "graphdb-deployment"
    log_info "Created new SSH key: ~/.ssh/graphdb_deploy"
fi

# Add SSH key to DigitalOcean if not already there
if ! doctl compute ssh-key list --format Name --no-header | grep -q "^${SSH_KEY_NAME}$"; then
    doctl compute ssh-key import $SSH_KEY_NAME --public-key-file ~/.ssh/graphdb_deploy.pub
    log_info "Uploaded SSH key to DigitalOcean"
else
    log_info "SSH key already exists in DigitalOcean"
fi

# Step 2: Create Cloudflare Tunnel
log_info "[2/10] Creating Cloudflare Tunnel..."
TUNNEL_NAME="graphdb-${DROPLET_NAME}"

# Check if tunnel already exists
if cloudflared tunnel list | grep -q "$TUNNEL_NAME"; then
    log_warn "Tunnel '$TUNNEL_NAME' already exists, using existing tunnel"
    TUNNEL_ID=$(cloudflared tunnel list | grep "$TUNNEL_NAME" | awk '{print $1}')
else
    cloudflared tunnel create "$TUNNEL_NAME"
    TUNNEL_ID=$(cloudflared tunnel list | grep "$TUNNEL_NAME" | awk '{print $1}')
    log_info "Created Cloudflare Tunnel: $TUNNEL_ID"
fi

# Get tunnel credentials
TUNNEL_CREDENTIALS_FILE=~/.cloudflared/${TUNNEL_ID}.json

if [ ! -f "$TUNNEL_CREDENTIALS_FILE" ]; then
    log_error "Tunnel credentials file not found: $TUNNEL_CREDENTIALS_FILE"
    exit 1
fi

# Step 3: Configure Cloudflare DNS
log_info "[3/10] Configuring Cloudflare DNS..."
cloudflared tunnel route dns "$TUNNEL_NAME" "$DOMAIN" || log_warn "DNS route may already exist"
log_info "DNS configured: $DOMAIN → Tunnel $TUNNEL_ID"

# Step 4: Create Digital Ocean droplet
log_info "[4/10] Creating Digital Ocean droplet..."
DROPLET_EXISTS=$(doctl compute droplet list --format Name --no-header | grep -c "^${DROPLET_NAME}$" || true)

if [ "$DROPLET_EXISTS" -eq 0 ]; then
    doctl compute droplet create "$DROPLET_NAME" \
        --image "$DO_IMAGE" \
        --size "$DO_SIZE" \
        --region "$DO_REGION" \
        --ssh-keys "$(doctl compute ssh-key list --format ID --no-header | grep -v '^$' | tr '\n' ',')" \
        --wait

    log_info "Droplet created successfully"
else
    log_warn "Droplet '$DROPLET_NAME' already exists"
fi

# Get droplet IP
DROPLET_IP=$(doctl compute droplet list --format Name,PublicIPv4 --no-header | grep "^${DROPLET_NAME}" | awk '{print $2}')

if [ -z "$DROPLET_IP" ]; then
    log_error "Failed to get droplet IP address"
    exit 1
fi

log_info "Droplet IP: $DROPLET_IP"

# Step 5: Wait for droplet to be ready
log_info "[5/10] Waiting for droplet to be ready..."
sleep 30

# Wait for SSH to be available
max_attempts=30
attempt=0
while ! ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 -i ~/.ssh/graphdb_deploy root@$DROPLET_IP "echo 'SSH ready'" &>/dev/null; do
    attempt=$((attempt + 1))
    if [ $attempt -ge $max_attempts ]; then
        log_error "Droplet SSH not accessible after ${max_attempts} attempts"
        exit 1
    fi
    log_info "Waiting for SSH... (attempt $attempt/$max_attempts)"
    sleep 10
done

log_info "Droplet is ready and accessible via SSH"

# Step 6: Copy deployment files to droplet
log_info "[6/10] Copying deployment files to droplet..."

# Create deployment package
DEPLOY_DIR=$(mktemp -d)
cp -r deployments/digitalocean/* "$DEPLOY_DIR/"
cp docker-compose.prod.yml "$DEPLOY_DIR/"

# Copy tunnel credentials
mkdir -p "$DEPLOY_DIR/cloudflared"
cp "$TUNNEL_CREDENTIALS_FILE" "$DEPLOY_DIR/cloudflared/credentials.json"

# Create tunnel config
cat > "$DEPLOY_DIR/cloudflared/config.yml" <<EOF
tunnel: $TUNNEL_ID
credentials-file: /etc/cloudflared/credentials.json

ingress:
  - hostname: $DOMAIN
    service: http://graphdb:8080
  - service: http_status:404
EOF

# Copy to droplet
scp -o StrictHostKeyChecking=no -i ~/.ssh/graphdb_deploy -r "$DEPLOY_DIR"/* root@$DROPLET_IP:/tmp/graphdb-deploy/

# Cleanup
rm -rf "$DEPLOY_DIR"

log_info "Deployment files copied"

# Step 7: Install dependencies on droplet
log_info "[7/10] Installing dependencies on droplet..."
ssh -o StrictHostKeyChecking=no -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'bash -s' <<'ENDSSH'
set -e

# Update system
apt-get update
apt-get upgrade -y

# Install Docker
if ! command -v docker &> /dev/null; then
    curl -fsSL https://get.docker.com | sh
    systemctl enable docker
    systemctl start docker
fi

# Install Docker Compose plugin
if ! docker compose version &> /dev/null; then
    apt-get install -y docker-compose-plugin
fi

# Create directories
mkdir -p /var/lib/graphdb/{data,backups,logs}
mkdir -p /etc/graphdb
mkdir -p /etc/cloudflared

# Move deployment files
mv /tmp/graphdb-deploy/cloudflared/* /etc/cloudflared/
chmod 600 /etc/cloudflared/credentials.json

echo "Dependencies installed successfully"
ENDSSH

log_info "Dependencies installed"

# Step 8: Deploy GraphDB and services
log_info "[8/10] Deploying GraphDB and Cloudflare Tunnel..."

# Create docker-compose configuration
ssh -o StrictHostKeyChecking=no -i ~/.ssh/graphdb_deploy root@$DROPLET_IP "bash -s" <<ENDSSH
set -e

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
      - GRAPHDB_EDITION=${GRAPHDB_EDITION}
      - PORT=8080
      - DATA_DIR=/data
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
    network_mode: host
EOF

# Start services
cd /var/lib/graphdb
GRAPHDB_EDITION=$GRAPHDB_EDITION docker compose up -d

echo "Services started successfully"
ENDSSH

log_info "GraphDB and Cloudflare Tunnel deployed"

# Step 9: Verify deployment
log_info "[9/10] Verifying deployment..."
sleep 15

# Check if services are running
ssh -o StrictHostKeyChecking=no -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'bash -s' <<'ENDSSH'
cd /var/lib/graphdb

if docker compose ps | grep -q "graphdb.*running"; then
    echo "✓ GraphDB is running"
else
    echo "✗ GraphDB is not running"
    docker compose logs graphdb
    exit 1
fi

if docker compose ps | grep -q "cloudflared.*running"; then
    echo "✓ Cloudflared is running"
else
    echo "✗ Cloudflared is not running"
    docker compose logs cloudflared
    exit 1
fi

# Check GraphDB health
if curl -f http://localhost:8080/health &>/dev/null; then
    echo "✓ GraphDB health check passed"
else
    echo "✗ GraphDB health check failed"
    exit 1
fi
ENDSSH

log_info "Local deployment verification passed"

# Step 10: Test public access
log_info "[10/10] Testing public access via Cloudflare Tunnel..."
sleep 10

if curl -f "https://${DOMAIN}/health" &>/dev/null; then
    log_info "✓ Public access test passed"
else
    log_warn "Public access test failed - tunnel may still be propagating (can take 2-3 minutes)"
fi

echo ""
echo "========================================="
echo "Deployment Complete! 🚀"
echo "========================================="
echo ""
echo "GraphDB is now accessible at:"
echo "  https://$DOMAIN"
echo ""
echo "Droplet Details:"
echo "  IP: $DROPLET_IP"
echo "  SSH: ssh -i ~/.ssh/graphdb_deploy root@$DROPLET_IP"
echo ""
echo "Management Commands:"
echo "  View logs:    ssh -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'cd /var/lib/graphdb && docker compose logs -f'"
echo "  Restart:      ssh -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'cd /var/lib/graphdb && docker compose restart'"
echo "  Stop:         ssh -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'cd /var/lib/graphdb && docker compose down'"
echo "  Start:        ssh -i ~/.ssh/graphdb_deploy root@$DROPLET_IP 'cd /var/lib/graphdb && docker compose up -d'"
echo ""
echo "Cloudflare Tunnel:"
echo "  Tunnel ID: $TUNNEL_ID"
echo "  Domain: $DOMAIN"
echo ""
echo "Next Steps:"
echo "  1. Test API: curl https://$DOMAIN/health"
echo "  2. Run benchmarks: ./benchmarks/run-benchmarks.sh"
echo "  3. Set up monitoring: cd deployments && ./monitoring-stack.sh"
echo ""
echo "========================================="
