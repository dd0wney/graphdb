# GraphDB Production Deployment Guide

Complete guide for deploying GraphDB in production environments.

## Table of Contents

- [Quick Start](#quick-start)
- [Architecture](#architecture)
- [Docker Deployment](#docker-deployment)
- [Cloud Platforms](#cloud-platforms)
- [Configuration](#configuration)
- [Security](#security)
- [Monitoring](#monitoring)
- [Backup & Restore](#backup--restore)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

### Prerequisites

- Docker & Docker Compose installed
- 2GB RAM minimum (4GB+ recommended)
- 10GB disk space minimum
- SSL certificates (for production HTTPS)

### 5-Minute Setup

```bash
# 1. Clone repository
git clone https://github.com/dd0wney/cluso-graphdb.git
cd cluso-graphdb

# 2. Create environment file
cp .env.example .env

# 3. Generate secrets
echo "JWT_SECRET=$(openssl rand -base64 32)" >> .env
echo "ADMIN_API_KEY=$(openssl rand -hex 32)" >> .env

# 4. Edit .env and set your passwords
nano .env

# 5. Start Community edition
docker-compose -f docker-compose.prod.yml --profile community up -d

# 6. Verify deployment
curl http://localhost:8080/health
```

**GraphDB Community is now running on http://localhost:8080**

---

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────┐
│                     Production Stack                         │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   Nginx      │───▶│   GraphDB    │───▶│  PostgreSQL  │  │
│  │  (Reverse    │    │   Community  │    │   (License   │  │
│  │   Proxy)     │    │   OR         │    │    Server)   │  │
│  │              │    │   Enterprise │    │              │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│         │                     │                    │         │
│         │            ┌──────────────┐             │         │
│         └───────────▶│   License    │◀────────────┘         │
│                      │   Server     │                       │
│                      │ (Enterprise) │                       │
│                      └──────────────┘                       │
│                                                               │
│  ┌──────────────┐    ┌──────────────┐                      │
│  │  Prometheus  │───▶│   Grafana    │   (Optional)         │
│  │ (Monitoring) │    │   (Dashboards│                      │
│  └──────────────┘    └──────────────┘                      │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

### Editions

**Community Edition:**
- ✅ Free and open source
- ✅ No license required
- ✅ Full graph database functionality
- ✅ REST + GraphQL APIs
- ✅ Custom HNSW vector search
- ⚠️ No Cloudflare integrations

**Enterprise Edition:**
- ✅ All Community features
- ✅ Cloudflare Vectorize (5M+ dimensions)
- ✅ R2 automated backups (zero egress)
- ✅ Change Data Capture (CDC)
- ✅ Multi-region replication
- ✅ Advanced monitoring & analytics
- ✅ Custom authentication (SAML, OIDC)
- ⚠️ Requires commercial license

---

## Docker Deployment

### Community Edition Only

```bash
# Start Community edition
docker-compose -f docker-compose.prod.yml --profile community up -d

# View logs
docker-compose -f docker-compose.prod.yml logs -f graphdb-community

# Stop
docker-compose -f docker-compose.prod.yml --profile community down
```

### Enterprise Edition with License Server

```bash
# Start Enterprise + License Server + PostgreSQL
docker-compose -f docker-compose.prod.yml \
  --profile enterprise \
  up -d

# View all logs
docker-compose -f docker-compose.prod.yml logs -f

# Stop all services
docker-compose -f docker-compose.prod.yml --profile enterprise down
```

### Full Stack with Monitoring

```bash
# Start everything: Enterprise + Monitoring + Nginx
docker-compose -f docker-compose.prod.yml \
  --profile enterprise \
  --profile monitoring \
  --profile nginx \
  up -d

# Access services:
# - GraphDB:    http://localhost:8081
# - License:    http://localhost:9000
# - Prometheus: http://localhost:9090
# - Grafana:    http://localhost:3000
```

### Building Custom Images

```bash
# Build GraphDB main server
docker build -t dd0wney/graphdb:1.0.0 -f Dockerfile .

# Build License server
docker build -t dd0wney/graphdb-license:1.0.0 -f Dockerfile.license-server .

# Push to Docker Hub
docker push dd0wney/graphdb:1.0.0
docker push dd0wney/graphdb-license:1.0.0
```

---

## Cloud Platforms

### Railway

[Complete Railway Guide →](./deployments/railway/README.md)

**Quick Deploy:**

```bash
# Install Railway CLI
npm install -g @railway/cli

# Login
railway login

# Create project
railway init

# Set environment variables
railway variables set POSTGRES_PASSWORD=your-password
railway variables set JWT_SECRET=$(openssl rand -base64 32)
railway variables set ADMIN_PASSWORD=your-admin-password

# Deploy
railway up
```

**Estimated Cost:** $5-20/month depending on usage

### DigitalOcean

[Complete DigitalOcean Guide →](./deployments/digitalocean/README.md)

**Quick Deploy:**

```bash
# Create droplet (4GB RAM recommended)
doctl compute droplet create graphdb \
  --size s-2vcpu-4gb \
  --image ubuntu-22-04-x64 \
  --region nyc1

# SSH and run setup script
./deployments/digitalocean/setup.sh
```

**Estimated Cost:** $24/month (4GB droplet)

### Cloudflare Workers

[Complete Cloudflare Guide →](./deployments/cloudflare/README.md)

**Best for:**
- GraphQL API edge deployment
- Global distribution
- Zero egress costs with R2

**Estimated Cost:** $5-15/month

---

## Configuration

### Environment Variables Reference

See [.env.example](./.env.example) for complete documentation.

**Critical Variables:**

| Variable | Required | Description |
|----------|----------|-------------|
| `POSTGRES_PASSWORD` | Yes | PostgreSQL password |
| `JWT_SECRET` | Yes | JWT signing secret |
| `ADMIN_PASSWORD` | Yes | Default admin password |
| `ADMIN_API_KEY` | Yes | License server admin key |
| `GRAPHDB_LICENSE_KEY` | Enterprise | Enterprise license key |

**Optional but Recommended:**

| Variable | Description |
|----------|-------------|
| `SMTP_HOST` | Email server for license delivery |
| `SMTP_PASSWORD` | Email authentication |
| `STRIPE_SECRET_KEY` | Stripe payment integration |
| `GRAPHDB_ENABLE_TELEMETRY` | Usage analytics (opt-in) |

### Generating Secrets

```bash
# JWT Secret (32 bytes base64)
openssl rand -base64 32

# Admin API Key (32 bytes hex)
openssl rand -hex 32

# Password (16 bytes base64, user-friendly)
openssl rand -base64 16
```

### Configuration Files

**PostgreSQL:**
- Connection string in `DATABASE_URL`
- Default: `postgres://graphdb:password@postgres:5432/graphdb_licenses`

**License Server:**
- Data directory: `/data/licenses` (JSON fallback)
- PostgreSQL preferred for production

**GraphDB:**
- Data directory: `/data`
- Persistent volumes for data retention

---

## Security

### Pre-Deployment Checklist

- [ ] Change ALL default passwords in `.env`
- [ ] Generate strong JWT_SECRET (32+ chars)
- [ ] Generate strong ADMIN_API_KEY (32+ chars)
- [ ] Set unique POSTGRES_PASSWORD
- [ ] Configure firewall rules
- [ ] Enable HTTPS with SSL certificates
- [ ] Restrict PostgreSQL to internal network only
- [ ] Set up automated backups
- [ ] Review `.gitignore` (`.env` should be excluded)
- [ ] Use environment-specific configs (`.env.production`, `.env.staging`)

### Firewall Configuration

```bash
# Allow HTTP/HTTPS
ufw allow 80/tcp
ufw allow 443/tcp

# Allow GraphDB (adjust as needed)
ufw allow 8080/tcp
ufw allow 8081/tcp

# Allow License Server (internal only)
ufw allow from 172.20.0.0/16 to any port 9000

# Block PostgreSQL from external
ufw deny 5432/tcp

# Enable firewall
ufw enable
```

### SSL/TLS Setup

**Option 1: Let's Encrypt (Free)**

```bash
# Install certbot
apt-get install certbot

# Get certificate
certbot certonly --standalone -d graphdb.example.com

# Certificates will be in:
# /etc/letsencrypt/live/graphdb.example.com/fullchain.pem
# /etc/letsencrypt/live/graphdb.example.com/privkey.pem

# Update nginx config with certificate paths
```

**Option 2: Self-Signed (Development)**

```bash
# Generate self-signed certificate
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout deployments/nginx/ssl/server.key \
  -out deployments/nginx/ssl/server.crt
```

### Secret Rotation

Rotate secrets every 90 days:

```bash
# 1. Generate new secrets
NEW_JWT_SECRET=$(openssl rand -base64 32)
NEW_API_KEY=$(openssl rand -hex 32)

# 2. Update .env
sed -i "s/^JWT_SECRET=.*/JWT_SECRET=$NEW_JWT_SECRET/" .env
sed -i "s/^ADMIN_API_KEY=.*/ADMIN_API_KEY=$NEW_API_KEY/" .env

# 3. Restart services
docker-compose -f docker-compose.prod.yml --profile enterprise restart
```

---

## Monitoring

### Prometheus Metrics

GraphDB exposes Prometheus metrics at `/metrics`:

```
# Node and edge counts
graphdb_nodes_total
graphdb_edges_total

# Query performance
graphdb_query_duration_seconds
graphdb_query_errors_total

# System metrics
graphdb_memory_usage_bytes
graphdb_cpu_usage_percent
```

### Grafana Dashboards

Access Grafana at `http://localhost:3000`:

1. Default credentials: `admin` / `your-password-from-env`
2. Datasource: Prometheus (auto-configured)
3. Import dashboards from `deployments/grafana/dashboards/`

**Available Dashboards:**
- GraphDB Overview
- Query Performance
- License Server Metrics
- System Health

### Health Checks

All services expose `/health` endpoints:

```bash
# GraphDB
curl http://localhost:8080/health

# License Server
curl http://localhost:9000/health

# Expected response:
{"status":"healthy","time":1699564800}
```

### Log Aggregation

```bash
# View all logs
docker-compose -f docker-compose.prod.yml logs -f

# View specific service
docker-compose -f docker-compose.prod.yml logs -f graphdb-enterprise

# Last 100 lines
docker-compose -f docker-compose.prod.yml logs --tail=100 graphdb-enterprise

# Filter by error level
docker-compose -f docker-compose.prod.yml logs | grep ERROR
```

---

## Backup & Restore

### PostgreSQL Backups

**Automated Backup Script:**

```bash
#!/bin/bash
# Save as: backups/backup-postgres.sh

BACKUP_DIR="/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/graphdb_licenses_$TIMESTAMP.sql"

docker-compose -f docker-compose.prod.yml exec -T postgres \
  pg_dump -U graphdb graphdb_licenses > "$BACKUP_FILE"

# Compress
gzip "$BACKUP_FILE"

# Keep last 30 days
find "$BACKUP_DIR" -name "*.sql.gz" -mtime +30 -delete

echo "Backup completed: $BACKUP_FILE.gz"
```

**Restore:**

```bash
# Restore from backup
gunzip < backups/graphdb_licenses_20241119.sql.gz | \
docker-compose -f docker-compose.prod.yml exec -T postgres \
  psql -U graphdb graphdb_licenses
```

**Schedule with Cron:**

```bash
# Add to crontab (daily at 2 AM)
0 2 * * * /path/to/backups/backup-postgres.sh
```

### GraphDB Data Backups

```bash
# Backup GraphDB data volume
docker run --rm \
  -v graphdb_community_data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar czf /backup/graphdb-data-$(date +%Y%m%d).tar.gz /data

# Restore
docker run --rm \
  -v graphdb_community_data:/data \
  -v $(pwd)/backups:/backup \
  alpine tar xzf /backup/graphdb-data-20241119.tar.gz -C /
```

---

## Troubleshooting

### Common Issues

**1. Container won't start**

```bash
# Check logs
docker-compose -f docker-compose.prod.yml logs graphdb-enterprise

# Check environment variables
docker-compose -f docker-compose.prod.yml config

# Verify .env file
cat .env | grep -v '^#' | grep -v '^$'
```

**2. License validation fails**

```bash
# Check license key format
echo $GRAPHDB_LICENSE_KEY  # Should be CGDB-XXXX-XXXX-XXXX-XXXX

# Test license server connectivity
curl http://localhost:9000/health

# Create test license
curl -X POST http://localhost:9000/licenses/create \
  -H "X-API-Key: $ADMIN_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com","type":"enterprise"}'
```

**3. Database connection errors**

```bash
# Check PostgreSQL is running
docker-compose -f docker-compose.prod.yml ps postgres

# Test connection
docker-compose -f docker-compose.prod.yml exec postgres \
  psql -U graphdb -d graphdb_licenses -c "SELECT 1;"

# Check network
docker network inspect graphdb_graphdb-network
```

**4. Out of memory**

```bash
# Check container memory
docker stats

# Increase Docker memory limit
# Docker Desktop: Settings > Resources > Memory > Increase to 4GB+

# Or limit container memory in docker-compose.prod.yml:
#   deploy:
#     resources:
#       limits:
#         memory: 2G
```

**5. Port already in use**

```bash
# Find process using port 8080
lsof -i :8080

# Kill process
kill -9 <PID>

# Or change port in .env
echo "GRAPHDB_COMMUNITY_PORT=8090" >> .env
```

### Performance Tuning

**PostgreSQL:**

```bash
# Increase shared buffers (25% of RAM)
docker-compose -f docker-compose.prod.yml exec postgres \
  psql -U graphdb -c "ALTER SYSTEM SET shared_buffers = '1GB';"

# Restart PostgreSQL
docker-compose -f docker-compose.prod.yml restart postgres
```

**GraphDB:**

```bash
# Monitor query performance
curl http://localhost:8080/metrics | grep graphdb_query_duration

# Check data volume size
docker system df -v | grep graphdb_community_data

# Compact data (if needed)
# Stop server, delete WAL files, restart
```

### Getting Help

- **Documentation:** https://docs.graphdb.dev
- **GitHub Issues:** https://github.com/dd0wney/cluso-graphdb/issues
- **Support Email:** support@graphdb.dev
- **Community:** Discord / Slack (link in README)

---

## Production Deployment Checklist

Before going live:

### Infrastructure
- [ ] Minimum 4GB RAM, 20GB disk
- [ ] SSL certificates configured
- [ ] Firewall rules set up
- [ ] Backup system configured
- [ ] Monitoring enabled

### Security
- [ ] All default passwords changed
- [ ] Strong secrets generated
- [ ] `.env` not in version control
- [ ] HTTPS enforced
- [ ] PostgreSQL access restricted
- [ ] Admin endpoints authenticated

### Configuration
- [ ] `GRAPHDB_EDITION` set correctly
- [ ] License key configured (Enterprise)
- [ ] Email delivery configured
- [ ] Stripe webhook configured (if selling licenses)
- [ ] Domain/DNS configured
- [ ] Reverse proxy configured

### Testing
- [ ] Health checks pass
- [ ] License validation works
- [ ] API authentication works
- [ ] Backup/restore tested
- [ ] Monitoring dashboards accessible
- [ ] Load testing performed

### Documentation
- [ ] Deployment documented
- [ ] Runbooks created
- [ ] On-call procedures defined
- [ ] Incident response plan ready

---

**Ready to deploy? Start with the [Quick Start](#quick-start) guide above!**
