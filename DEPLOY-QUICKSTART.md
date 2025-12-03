# GraphDB Deployment Quickstart

Deploy GraphDB to Digital Ocean with Cloudflare Tunnel in **one command**.

## What This Does

The `deploy.sh` script automatically:
1. ✅ Creates a Digital Ocean droplet (Ubuntu 22.04)
2. ✅ Installs Docker and dependencies
3. ✅ Creates and configures Cloudflare Tunnel
4. ✅ Sets up automatic SSL/TLS via Cloudflare
5. ✅ Deploys GraphDB with Docker Compose
6. ✅ Configures auto-restart on reboot
7. ✅ Verifies deployment and public access
8. ✅ Provides management commands

**Time to deploy**: ~5-10 minutes

---

## Prerequisites

### 1. Install Required Tools

**Digital Ocean CLI (doctl)**:
```bash
# macOS
brew install doctl

# Linux
cd /tmp
wget https://github.com/digitalocean/doctl/releases/download/v1.104.0/doctl-1.104.0-linux-amd64.tar.gz
tar xf doctl-*.tar.gz
sudo mv doctl /usr/local/bin

# Authenticate
doctl auth init
```

**Cloudflare CLI (cloudflared)**:
```bash
# macOS
brew install cloudflare/cloudflare/cloudflared

# Linux
wget -q https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
sudo dpkg -i cloudflared-linux-amd64.deb

# Authenticate
cloudflared tunnel login
```

### 2. Get Required API Tokens

**Cloudflare API Token**:
1. Go to https://dash.cloudflare.com/profile/api-tokens
2. Click "Create Token"
3. Use template: "Edit Cloudflare Workers"
4. Or create custom token with permissions:
   - Zone: DNS Edit
   - Account: Cloudflare Tunnel Edit
5. Copy the token

**Cloudflare Account ID**:
1. Go to your Cloudflare dashboard
2. Select your domain
3. Scroll down in the right sidebar
4. Copy "Account ID"

**Cloudflare Zone ID**:
1. In Cloudflare dashboard, select your domain
2. Scroll down in the right sidebar
3. Copy "Zone ID"

### 3. Configure DNS (One Time)

1. Point your domain to Cloudflare nameservers (if not already)
2. The deployment script will automatically create DNS records

---

## Deploy in One Command

### Community Edition (Free)

```bash
# Set required environment variables
export CLOUDFLARE_API_TOKEN="your-cloudflare-api-token"
export CLOUDFLARE_ACCOUNT_ID="your-cloudflare-account-id"
export CLOUDFLARE_ZONE_ID="your-cloudflare-zone-id"
export DOMAIN="graphdb.yourdomain.com"

# Deploy!
./deploy.sh
```

### Enterprise Edition

```bash
# Set required environment variables
export CLOUDFLARE_API_TOKEN="your-cloudflare-api-token"
export CLOUDFLARE_ACCOUNT_ID="your-cloudflare-account-id"
export CLOUDFLARE_ZONE_ID="your-cloudflare-zone-id"
export DOMAIN="graphdb.yourdomain.com"
export GRAPHDB_EDITION="enterprise"
export GRAPHDB_LICENSE_KEY="your-license-key"

# Deploy!
./deploy.sh
```

### Optional Configuration

```bash
# Customize droplet size (default: s-2vcpu-4gb)
export DO_SIZE="s-4vcpu-8gb"  # More power

# Customize region (default: nyc3)
export DO_REGION="sfo3"  # San Francisco

# Customize droplet name (default: graphdb-production)
export DROPLET_NAME="graphdb-staging"
```

---

## What Happens During Deployment

```
[1/10] Setting up SSH key...
[2/10] Creating Cloudflare Tunnel...
[3/10] Configuring Cloudflare DNS...
[4/10] Creating Digital Ocean droplet...
[5/10] Waiting for droplet to be ready...
[6/10] Copying deployment files to droplet...
[7/10] Installing dependencies on droplet...
[8/10] Deploying GraphDB and Cloudflare Tunnel...
[9/10] Verifying deployment...
[10/10] Testing public access via Cloudflare Tunnel...

========================================
Deployment Complete! 🚀
========================================

GraphDB is now accessible at:
  https://graphdb.yourdomain.com

Droplet Details:
  IP: 142.93.xxx.xxx
  SSH: ssh -i ~/.ssh/graphdb_deploy root@142.93.xxx.xxx
```

---

## Post-Deployment

### Test Your Deployment

```bash
# Health check
curl https://graphdb.yourdomain.com/health

# Expected output:
# {"status":"ok","version":"1.0.0","uptime":120}
```

### Create Your First Graph

```typescript
import { GraphDBClient } from '@graphdb/client';

const client = new GraphDBClient({
  endpoint: 'https://graphdb.yourdomain.com',
  apiKey: 'your-api-key',  // Set up authentication first
});

// Create nodes
await client.createNode({
  type: 'user',
  properties: { name: 'Alice', email: 'alice@example.com' },
});

await client.createNode({
  type: 'user',
  properties: { name: 'Bob', email: 'bob@example.com' },
});

// Create relationship
await client.createEdge({
  type: 'TRUSTS',
  source: 'user-1',
  target: 'user-2',
  properties: { score: 0.9 },
});
```

---

## Management Commands

### SSH into Droplet

```bash
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP
```

### View Logs

```bash
# All services
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'cd /var/lib/graphdb && docker compose logs -f'

# GraphDB only
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'cd /var/lib/graphdb && docker compose logs -f graphdb'

# Cloudflare Tunnel only
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'cd /var/lib/graphdb && docker compose logs -f cloudflared'
```

### Restart Services

```bash
# Restart all
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'cd /var/lib/graphdb && docker compose restart'

# Restart GraphDB only
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'cd /var/lib/graphdb && docker compose restart graphdb'
```

### Update GraphDB

```bash
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP 'bash -s' <<'EOF'
cd /var/lib/graphdb
docker compose pull
docker compose up -d
EOF
```

### Backup Data

```bash
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'tar -czf /var/lib/graphdb/backups/backup-$(date +%Y%m%d-%H%M%S).tar.gz /var/lib/graphdb/data'
```

---

## Monitoring

### Add Prometheus + Grafana (Optional)

```bash
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP 'bash -s' <<'EOF'
cd /tmp
git clone https://github.com/yourusername/graphdb.git
cd graphdb/deployments
./monitoring-stack.sh
EOF
```

Then access:
- Grafana: `https://grafana.yourdomain.com` (configure in Cloudflare Tunnel)
- Prometheus: `http://YOUR_DROPLET_IP:9090`

---

## Troubleshooting

### Deployment Fails at Step X

```bash
# Check droplet status
doctl compute droplet list

# Check tunnel status
cloudflared tunnel list

# SSH and check Docker
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP 'docker ps'
```

### Cannot Access GraphDB via HTTPS

**Wait 2-3 minutes** - Cloudflare Tunnel propagation can take time.

Then check:
```bash
# 1. Verify tunnel is running
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'docker logs cloudflared'

# 2. Check GraphDB health locally
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'curl http://localhost:8080/health'

# 3. Check Cloudflare DNS
nslookup graphdb.yourdomain.com

# 4. Test tunnel connectivity
cloudflared tunnel info YOUR_TUNNEL_ID
```

### GraphDB Container Keeps Restarting

```bash
# Check logs
ssh -i ~/.ssh/graphdb_deploy root@YOUR_DROPLET_IP \
  'docker logs graphdb'

# Common issues:
# - Insufficient memory (upgrade droplet size)
# - Corrupted data directory (backup and clear /var/lib/graphdb/data)
# - License key issues (enterprise only)
```

---

## Costs

### Digital Ocean

| Droplet Size | vCPUs | RAM | Price/Month | Recommended For |
|--------------|-------|-----|-------------|-----------------|
| s-2vcpu-4gb  | 2     | 4GB | $24         | Development, small datasets |
| s-4vcpu-8gb  | 4     | 8GB | $48         | Production, medium datasets |
| s-8vcpu-16gb | 8     | 16GB| $96         | High traffic, large datasets |

### Cloudflare

- Tunnel: **Free**
- DNS: **Free**
- SSL/TLS: **Free**

**Total minimum cost**: ~$24/month (DO droplet only)

---

## Next Steps

1. ✅ **Set up authentication** - Add API keys for secure access
2. ✅ **Run benchmarks** - Test performance with your data
3. ✅ **Add monitoring** - Deploy Prometheus + Grafana
4. ✅ **Set up backups** - Automate daily backups
5. ✅ **Load test** - Verify it handles your expected traffic

---

## Clean Up (Delete Everything)

```bash
# Delete droplet
doctl compute droplet delete graphdb-production --force

# Delete tunnel
cloudflared tunnel delete graphdb-graphdb-production

# Delete SSH key
doctl compute ssh-key delete graphdb-deploy --force
rm ~/.ssh/graphdb_deploy*

# Delete local tunnel config
rm -rf ~/.cloudflared/
```

---

## Support

- **Issues**: https://github.com/yourusername/graphdb/issues
- **Docs**: https://graphdb.dev/docs
- **Community**: https://discord.gg/graphdb

---

**Ready to deploy? Run `./deploy.sh` and you'll have GraphDB running in 5 minutes!** 🚀
