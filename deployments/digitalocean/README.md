# GraphDB Digital Ocean Deployment Guide

This guide walks you through deploying GraphDB to Digital Ocean with Cloudflare Tunnel integration for global edge performance.

## Architecture

```
[Users Globally]
       ↓
[Cloudflare Edge] (300+ locations)
       ↓
[Cloudflare Tunnel] (encrypted, no exposed ports)
       ↓
[Digital Ocean Droplet]
  ├── GraphDB (Docker container)
  └── cloudflared (Tunnel daemon)
```

## Prerequisites

1. **Digital Ocean Account**
   - Create a droplet (Ubuntu 22.04 LTS recommended)
   - Minimum: 2GB RAM, 2 vCPUs
   - Recommended: 4GB RAM, 4 vCPUs for production

2. **Cloudflare Account**
   - Domain managed by Cloudflare
   - Cloudflare Tunnel configured
   - Zero Trust dashboard access

3. **GraphDB Edition**
   - Community: Free, open source
   - Enterprise: License key required

## Quick Start

### Step 1: Create Digital Ocean Droplet

```bash
# Via Digital Ocean CLI (doctl)
doctl compute droplet create graphdb \\
  --image ubuntu-22-04-x64 \\
  --size s-2vcpu-4gb \\
  --region nyc1 \\
  --ssh-keys your-ssh-key-id

# Or use the Digital Ocean web console
```

### Step 2: SSH into Droplet

```bash
ssh root@your-droplet-ip
```

### Step 3: Run Deployment Script

```bash
# Download setup script
curl -O https://raw.githubusercontent.com/yourusername/graphdb/main/deployments/digitalocean/setup.sh
chmod +x setup.sh

# Deploy Community Edition
./setup.sh community

# Or Enterprise Edition
./setup.sh enterprise
```

### Step 4: Configure Cloudflare Tunnel

1. Create a tunnel in Cloudflare Zero Trust dashboard
2. Download tunnel credentials
3. Copy configuration files:

```bash
# Copy tunnel credentials
scp your-tunnel-credentials.json root@your-droplet-ip:/etc/cloudflared/credentials.json

# Copy tunnel config
scp deployments/cloudflare/tunnel-config.yml root@your-droplet-ip:/etc/cloudflared/config.yml

# Update hostnames in config
nano /etc/cloudflared/config.yml
```

4. Update `/etc/cloudflared/config.yml` with your tunnel ID and hostnames:

```yaml
tunnel: YOUR_TUNNEL_ID  # Replace with your tunnel ID
credentials-file: /etc/cloudflared/credentials.json

ingress:
  - hostname: api.yourdomain.com  # Replace with your domain
    service: http://localhost:8080
  - service: http_status:404
```

### Step 5: Start GraphDB

```bash
systemctl start graphdb
systemctl status graphdb
```

### Step 6: Verify Deployment

```bash
# Check health endpoint locally
curl http://localhost:8080/health

# Check health endpoint via Cloudflare Tunnel
curl https://api.yourdomain.com/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-01T00:00:00Z",
  "version": "1.0.0",
  "edition": "Community",
  "features": ["vector_search", "graphql"],
  "uptime": "5m30s"
}
```

## Enterprise Edition Setup

For Enterprise features (Cloudflare Vectorize, R2 backups, CDC), additional configuration is required:

### 1. Add License Key

```bash
# Copy license key to droplet
scp license.key root@your-droplet-ip:/etc/graphdb/license.key
```

### 2. Configure Cloudflare Services

Edit `/etc/graphdb/config.yaml`:

```yaml
edition: enterprise

# Cloudflare Vectorize (vector search)
vector:
  implementation: "vectorize"
  vectorize:
    account_id: "YOUR_CLOUDFLARE_ACCOUNT_ID"
    api_token: "YOUR_API_TOKEN"
    index_name: "graphdb-vectors"

# Cloudflare R2 (backups)
backup:
  enabled: true
  implementation: "r2"
  r2:
    account_id: "YOUR_CLOUDFLARE_ACCOUNT_ID"
    access_key_id: "YOUR_R2_ACCESS_KEY"
    secret_access_key: "YOUR_R2_SECRET_KEY"
    bucket: "graphdb-backups"

# Cloudflare Queues (CDC)
cdc:
  enabled: true
  queue:
    account_id: "YOUR_CLOUDFLARE_ACCOUNT_ID"
    api_token: "YOUR_API_TOKEN"
    queue_name: "graphdb-changes"
```

### 3. Restart GraphDB

```bash
systemctl restart graphdb
```

## Maintenance

### View Logs

```bash
# GraphDB logs
docker compose -f /var/lib/graphdb/docker-compose.yml logs -f graphdb

# Cloudflare Tunnel logs
docker compose -f /var/lib/graphdb/docker-compose.yml logs -f cloudflared
```

### Update GraphDB

```bash
cd /var/lib/graphdb
docker compose pull
docker compose up -d
```

### Backup Data

```bash
# Manual backup (Community)
tar -czf /var/lib/graphdb/backups/graphdb-$(date +%Y%m%d).tar.gz /var/lib/graphdb/data

# Automated backups (Enterprise)
# Configured in config.yaml, automatically backs up to R2
```

### Monitor Performance

```bash
# Check metrics
curl http://localhost:8080/metrics

# Via Cloudflare Tunnel
curl https://api.yourdomain.com/metrics
```

## Scaling

### Vertical Scaling (Increase Droplet Size)

```bash
# Via doctl
doctl compute droplet-action resize DROPLET_ID --size s-4vcpu-8gb
```

### Horizontal Scaling (Multi-Region)

For Enterprise multi-region replication:

1. Deploy GraphDB to multiple regions (NYC, SF, LON)
2. Configure Raft consensus in `config.yaml`
3. Set up automatic failover

See [Multi-Region Setup Guide](docs/multi-region.md) for details.

## Troubleshooting

### GraphDB Not Starting

```bash
# Check logs
docker compose -f /var/lib/graphdb/docker-compose.yml logs graphdb

# Common issues:
# - Permission errors: chown -R 1000:1000 /var/lib/graphdb/data
# - Port conflict: netstat -tulpn | grep 8080
```

### Cloudflare Tunnel Not Connecting

```bash
# Check tunnel status
docker compose -f /var/lib/graphdb/docker-compose.yml logs cloudflared

# Verify credentials
cat /etc/cloudflared/credentials.json

# Test tunnel connectivity
cloudflared tunnel info YOUR_TUNNEL_ID
```

### High Memory Usage

```bash
# Check memory
free -h

# Restart GraphDB
systemctl restart graphdb

# Consider upgrading droplet size
```

## Cost Estimate

**Community Edition:**
- Digital Ocean Droplet (4GB RAM): $24/month
- Cloudflare Tunnel: Free
- Total: ~$24/month

**Enterprise Edition:**
- Digital Ocean Droplet (8GB RAM): $48/month
- Cloudflare Tunnel: Free
- Cloudflare Vectorize: ~$5/month
- Cloudflare R2 (100GB): ~$2/month
- Cloudflare Queues: Free tier (up to 1M requests)
- Total: ~$55/month

## Security Best Practices

1. **Firewall**: Only allow SSH (port 22), block all other inbound ports
2. **SSH Key Auth**: Disable password authentication
3. **Automatic Updates**: Enable unattended-upgrades
4. **Cloudflare Access**: Add Zero Trust authentication
5. **Encryption**: All traffic encrypted via Cloudflare Tunnel

## Support

- Community Edition: GitHub Issues
- Enterprise Edition: enterprise@graphdb.com
- Documentation: https://graphdb.com/docs
