# Syntopica Staging Deployment - GraphDB on Digital Ocean

**Date:** 2025-11-24
**Target:** Digital Ocean Droplet (2GB RAM / 1 vCPU / 50GB SSD) - $12/month
**Purpose:** 48-hour soak test with real Syntopica workload

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                    Cloudflare Workers                         │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Syntopica API (graph.ts handlers)                     │  │
│  │  • Related concepts                                     │  │
│  │  • Learning paths                                       │  │
│  │  • PageRank rankings                                    │  │
│  │  • Knowledge graph visualization                       │  │
│  └──────────────┬─────────────────┬────────────────────────┘  │
│                 │                 │                            │
│                 v                 v                            │
│        ┌────────────────┐  ┌──────────────┐                   │
│        │ Cloudflare KV  │  │  Supabase    │                   │
│        │  (Cache Layer) │  │ (PostgreSQL) │                   │
│        └────────────────┘  └──────────────┘                   │
│                 │                                              │
│                 v                                              │
│        ┌────────────────────────────────┐                     │
│        │   Cloudflare Tunnel (HTTPS)    │                     │
└────────┴────────────────────────────────┴─────────────────────┘
                 │
                 v (Tunnel)
┌─────────────────────────────────────────────────────────────┐
│              Digital Ocean Droplet (Staging)                 │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  GraphDB Server (Port 8080)                            │  │
│  │  • Concept nodes (20K)                                 │  │
│  │  • Relationship edges (100K)                           │  │
│  │  • Cypher query engine                                 │  │
│  │  • Graph algorithms (PageRank, shortest path)          │  │
│  └────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Cloudflared (Tunnel Agent)                            │  │
│  └────────────────────────────────────────────────────────┘  │
│  ┌────────────────────────────────────────────────────────┐  │
│  │  Data Directory (/var/lib/graphdb)                     │  │
│  │  • LSM tree storage                                    │  │
│  │  • Write-ahead log (WAL)                               │  │
│  │  • Audit logs (JSONL)                                  │  │
│  └────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Digital Ocean Droplet Provisioning

### 1.1 Create Droplet

```bash
# Using DigitalOcean CLI (doctl)
doctl compute droplet create graphdb-staging \
  --region nyc1 \
  --size s-1vcpu-2gb \
  --image ubuntu-22-04-x64 \
  --ssh-keys <your-ssh-key-id> \
  --enable-monitoring \
  --enable-ipv6 \
  --tag-names staging,graphdb,syntopica

# OR use DigitalOcean Web UI:
# 1. Go to https://cloud.digitalocean.com/droplets/new
# 2. Choose: Ubuntu 22.04 LTS
# 3. Plan: Basic ($12/month - 2GB RAM / 1 vCPU / 50GB SSD)
# 4. Region: NYC1 or closest to Cloudflare edge
# 5. Add SSH key
# 6. Enable: Monitoring, IPv6
# 7. Hostname: graphdb-staging
```

### 1.1b Create and Attach Volume (100GB)

```bash
# Create volume
doctl compute volume create graphdb-data \
  --region nyc1 \
  --size 100GiB \
  --desc "GraphDB data storage (LSM tree, WAL, audit logs)"

# Get volume ID
doctl compute volume list | grep graphdb-data

# Attach to droplet
doctl compute volume-action attach <volume-id> <droplet-id>

# OR use Web UI:
# 1. Go to Volumes → Create Volume
# 2. Name: graphdb-data
# 3. Size: 100GB ($10/month)
# 4. Region: NYC1 (same as droplet)
# 5. Attach to: graphdb-staging droplet
```

### 1.2 Mount Volume

```bash
# SSH into droplet
ssh root@<droplet-ip>

# Check if volume is attached
lsblk
# Should see: sda (disk) with no partitions

# Format volume (EXT4)
mkfs.ext4 -F /dev/disk/by-id/scsi-0DO_Volume_graphdb-data

# Create mount point
mkdir -p /mnt/graphdb-data

# Mount volume
mount -o discard,defaults /dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data

# Add to /etc/fstab for auto-mount on boot
echo '/dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-data ext4 defaults,nofail,discard 0 2' >> /etc/fstab

# Verify mount
df -h | grep graphdb-data
# Should show: /dev/sda mounted at /mnt/graphdb-data with ~100GB available
```

### 1.3 Initial Server Setup

```bash
# Update system
apt update && apt upgrade -y

# Install dependencies
apt install -y \
  git \
  build-essential \
  curl \
  wget \
  vim \
  htop \
  net-tools \
  ufw \
  iotop

# Create graphdb user
useradd -m -s /bin/bash graphdb
usermod -aG sudo graphdb

# Setup data directory on volume
mkdir -p /mnt/graphdb-data/{data,wal,audit,backups}
chown -R graphdb:graphdb /mnt/graphdb-data
chmod 750 /mnt/graphdb-data

# Create symlink for easier access
ln -s /mnt/graphdb-data /var/lib/graphdb

# Setup firewall (UFW)
ufw allow OpenSSH
ufw allow 8080/tcp  # GraphDB API (internal only, via tunnel)
ufw enable
```

---

## Phase 2: Install GraphDB

### 2.1 Install Go 1.21+

```bash
# Download Go
cd /tmp
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz

# Install
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

# Configure environment
cat >> /etc/profile.d/go.sh <<'EOF'
export PATH=$PATH:/usr/local/go/bin
export GOPATH=/home/graphdb/go
export PATH=$PATH:$GOPATH/bin
EOF

source /etc/profile.d/go.sh

# Verify
go version  # Should show: go version go1.21.5 linux/amd64
```

### 2.2 Deploy GraphDB

```bash
# Clone repository (as graphdb user)
su - graphdb
cd ~
git clone https://github.com/yourusername/graphdb.git
cd graphdb

# Build
make build

# OR build manually
go build -o bin/server cmd/server/main.go

# Verify binary
./bin/server --version
```

### 2.3 Configure GraphDB

```bash
# Create config directory
sudo mkdir -p /etc/graphdb

# Create config file
sudo tee /etc/graphdb/config.yaml > /dev/null <<'EOF'
# GraphDB Staging Configuration

server:
  port: 8080
  host: "127.0.0.1"  # Only accessible via Cloudflare Tunnel
  read_timeout: 30s
  write_timeout: 30s

storage:
  # All data on 100GB volume
  data_dir: "/mnt/graphdb-data/data"
  wal_dir: "/mnt/graphdb-data/wal"

  # Memory settings for 2GB droplet
  cache_size_mb: 256      # LSM cache
  max_open_files: 1000

  # LSM tree tuning
  memtable_size_mb: 64
  compaction_workers: 2

  # Performance
  bloom_filter_bits: 10
  compression: "snappy"

audit:
  enabled: true
  log_dir: "/mnt/graphdb-data/audit"
  rotation_size_mb: 100
  max_age_days: 30

# Monitoring
metrics:
  enabled: true
  prometheus_port: 9090  # Internal only

# Authentication (Syntopica uses JWT at Workers level)
auth:
  enabled: false  # Workers handles auth

# Logging
logging:
  level: "info"
  format: "json"
  output: "/var/log/graphdb/server.log"
EOF

# Create log directory (on root disk, not volume)
sudo mkdir -p /var/log/graphdb
sudo chown graphdb:graphdb /var/log/graphdb

# Verify volume directories exist
ls -la /mnt/graphdb-data/
```

### 2.4 Create systemd Service

```bash
sudo cat > /etc/systemd/system/graphdb.service <<'EOF'
[Unit]
Description=GraphDB Server
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/home/graphdb/graphdb
ExecStart=/home/graphdb/graphdb/bin/server -config /etc/graphdb/config.yaml
Restart=always
RestartSec=10s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=graphdb

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/mnt/graphdb-data /var/log/graphdb

# Resource limits (2GB droplet)
MemoryMax=1.5G
MemoryHigh=1.2G
TasksMax=1000

# Environment
Environment="GOMAXPROCS=1"
Environment="GOGC=100"

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd
sudo systemctl daemon-reload

# Enable and start
sudo systemctl enable graphdb
sudo systemctl start graphdb

# Check status
sudo systemctl status graphdb

# View logs
sudo journalctl -u graphdb -f
```

---

## Phase 3: Cloudflare Tunnel Setup

### 3.1 Install Cloudflared

```bash
# Download cloudflared
wget https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
sudo dpkg -i cloudflared-linux-amd64.deb

# Verify
cloudflared --version
```

### 3.2 Authenticate and Create Tunnel

```bash
# Login to Cloudflare (opens browser)
cloudflared tunnel login

# Create tunnel
cloudflared tunnel create graphdb-staging

# Note the tunnel ID from output:
# Created tunnel graphdb-staging with id XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX

# Create config
sudo mkdir -p /etc/cloudflared
sudo cat > /etc/cloudflared/config.yml <<'EOF'
tunnel: graphdb-staging
credentials-file: /root/.cloudflared/XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX.json

ingress:
  # GraphDB API
  - hostname: graphdb-staging.yourdomain.com
    service: http://127.0.0.1:8080
    originRequest:
      connectTimeout: 30s
      noTLSVerify: false

  # Prometheus metrics (optional, for monitoring)
  - hostname: graphdb-metrics-staging.yourdomain.com
    service: http://127.0.0.1:9090

  # Catch-all
  - service: http_status:404
EOF
```

### 3.3 Configure DNS

```bash
# Route DNS to tunnel
cloudflared tunnel route dns graphdb-staging graphdb-staging.yourdomain.com
cloudflared tunnel route dns graphdb-staging graphdb-metrics-staging.yourdomain.com

# Verify DNS propagation (may take 1-5 minutes)
dig graphdb-staging.yourdomain.com
```

### 3.4 Create Cloudflared Service

```bash
sudo cat > /etc/systemd/system/cloudflared.service <<'EOF'
[Unit]
Description=Cloudflare Tunnel
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/cloudflared tunnel --config /etc/cloudflared/config.yml run
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cloudflared

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable cloudflared
sudo systemctl start cloudflared

# Verify
sudo systemctl status cloudflared
curl -I https://graphdb-staging.yourdomain.com/health
```

---

## Phase 4: Syntopica Integration

### 4.1 Configure Syntopica Workers

```bash
# In syntopica-v2 repository
cd /home/ddowney/Workspace/github.com/syntopica-v2/workers

# Set GraphDB URL secret
npx wrangler secret put GRAPHDB_URL
# Enter: https://graphdb-staging.yourdomain.com

# Verify wrangler.toml
cat wrangler.toml | grep -A5 "\[env.staging\]"

# Should have:
# [env.staging]
# name = "syntopica-workers-staging"
# vars = { ENVIRONMENT = "staging" }
```

### 4.2 Initial Data Sync

```bash
# Option 1: Full batch sync (via Syntopica Workers API)
curl -X POST https://staging.yourdomain.com/api/internal/sync/batch/all \
  -H "Authorization: Bearer $ADMIN_API_KEY" \
  -H "Content-Type: application/json"

# Expected response:
# {
#   "success": true,
#   "synced": {
#     "concepts": 1234,
#     "relationships": 5678,
#     "book_mappings": 890
#   }
# }

# Option 2: Manual database dump and restore
# Export from Supabase
psql $SUPABASE_DATABASE_URL -c "
  COPY (
    SELECT
      id,
      name,
      category,
      difficulty_level,
      created_by
    FROM global_concepts
    WHERE graphdb_synced_at IS NULL
  ) TO STDOUT CSV HEADER
" > concepts_to_sync.csv

# Import to GraphDB (via bulk API)
curl -X POST https://graphdb-staging.yourdomain.com/bulk/nodes \
  -H "Content-Type: application/json" \
  -d @concepts_bulk.json
```

### 4.3 Verify Sync Status

```bash
# Check Supabase sync health
psql $SUPABASE_DATABASE_URL -c "SELECT * FROM get_graphdb_sync_status();"

# Expected output:
#  entity_type  | total_count | synced_count | unsynced_count | sync_percentage
# --------------+-------------+--------------+----------------+-----------------
#  concepts     |        5000 |         5000 |              0 |          100.00
#  relationships|       15000 |        15000 |              0 |          100.00
#  book_concepts|        3000 |         3000 |              0 |          100.00

# Check GraphDB directly
curl https://graphdb-staging.yourdomain.com/stats | jq '.'

# Expected:
# {
#   "nodes": 5000,
#   "edges": 18000,
#   "labels": ["Concept", "Book", "User"],
#   "relationship_types": ["PREREQUISITE", "BUILDS_ON", "RELATED_TO", "COVERS", "MASTERED"]
# }
```

---

## Phase 5: 48-Hour Soak Test

### 5.1 Monitoring Setup

```bash
# Install monitoring tools
sudo apt install -y prometheus-node-exporter

# Create monitoring dashboard script
cat > ~/monitor.sh <<'EOF'
#!/bin/bash
# GraphDB Soak Test Monitoring

while true; do
  clear
  echo "=== GraphDB Staging Soak Test ==="
  echo "Time: $(date)"
  echo ""

  # System resources
  echo "--- System Resources ---"
  free -h | grep Mem
  mpstat -P ALL 1 1 | tail -1
  df -h | grep /var/lib/graphdb
  echo ""

  # GraphDB stats
  echo "--- GraphDB Stats ---"
  curl -s http://127.0.0.1:8080/stats | jq '{nodes, edges, queries_total, avg_latency_ms}'
  echo ""

  # GraphDB health
  echo "--- Health Check ---"
  curl -s http://127.0.0.1:8080/health | jq '.'
  echo ""

  # Recent logs
  echo "--- Recent Errors (last 1 min) ---"
  journalctl -u graphdb --since "1 minute ago" | grep -i error | tail -5
  echo ""

  sleep 60
done
EOF

chmod +x ~/monitor.sh

# Run in tmux
sudo apt install -y tmux
tmux new-session -d -s monitor './monitor.sh'

# View: tmux attach -t monitor
# Detach: Ctrl+B, then D
```

### 5.2 Load Test Script

```bash
# Create load test using Syntopica's actual API
cat > ~/load_test.sh <<'EOF'
#!/bin/bash
# Syntopica GraphDB Load Test

WORKERS_URL="https://staging.yourdomain.com"
JWT_TOKEN="<test-user-jwt-token>"
REQUESTS_PER_MIN=50
DURATION_HOURS=48

echo "Starting GraphDB soak test..."
echo "Duration: $DURATION_HOURS hours"
echo "Load: $REQUESTS_PER_MIN req/min"

# Calculate total iterations
TOTAL_MINUTES=$((DURATION_HOURS * 60))
TOTAL_REQUESTS=$((TOTAL_MINUTES * REQUESTS_PER_MIN))

echo "Total requests: $TOTAL_REQUESTS"
echo ""

START_TIME=$(date +%s)

for i in $(seq 1 $TOTAL_REQUESTS); do
  # Random query selection (realistic mix)
  RAND=$((RANDOM % 100))

  if [ $RAND -lt 40 ]; then
    # 40% - Get related concepts (most common)
    CONCEPT_ID=$((RANDOM % 5000 + 1))
    curl -s "$WORKERS_URL/api/protected/graph/concepts/$CONCEPT_ID/related" \
      -H "Authorization: Bearer $JWT_TOKEN" \
      > /dev/null

  elif [ $RAND -lt 70 ]; then
    # 30% - Find learning path
    FROM_ID=$((RANDOM % 5000 + 1))
    TO_ID=$((RANDOM % 5000 + 1))
    curl -s "$WORKERS_URL/api/protected/graph/concepts/path/$FROM_ID/$TO_ID" \
      -H "Authorization: Bearer $JWT_TOKEN" \
      > /dev/null

  elif [ $RAND -lt 90 ]; then
    # 20% - Neighborhood exploration
    CONCEPT_ID=$((RANDOM % 5000 + 1))
    curl -s "$WORKERS_URL/api/protected/graph/concepts/$CONCEPT_ID/neighborhood" \
      -H "Authorization: Bearer $JWT_TOKEN" \
      > /dev/null

  else
    # 10% - Knowledge graph visualization (heavy)
    curl -s "$WORKERS_URL/api/protected/graph/knowledge-graph" \
      -H "Authorization: Bearer $JWT_TOKEN" \
      > /dev/null
  fi

  # Progress report every 100 requests
  if [ $((i % 100)) -eq 0 ]; then
    ELAPSED=$(($(date +%s) - START_TIME))
    HOURS=$((ELAPSED / 3600))
    MINUTES=$(((ELAPSED % 3600) / 60))
    echo "[$HOURS:$MINUTES] Completed $i / $TOTAL_REQUESTS requests"
  fi

  # Rate limiting: 50 req/min = 1 req every 1.2 seconds
  sleep 1.2
done

echo ""
echo "Soak test complete!"
echo "Total time: $((($(date +%s) - START_TIME) / 3600)) hours"
EOF

chmod +x ~/load_test.sh

# Run in background with logging
nohup ./load_test.sh > load_test.log 2>&1 &
echo $! > load_test.pid

# Monitor progress
tail -f load_test.log
```

### 5.3 Success Criteria

**Track these metrics over 48 hours:**

| Metric | Target | Measurement |
|--------|--------|-------------|
| **Uptime** | > 99.5% (< 4 min downtime) | `systemctl status graphdb` |
| **P95 Latency** | < 100ms | Cloudflare Workers analytics |
| **P99 Latency** | < 500ms | Cloudflare Workers analytics |
| **Memory Usage** | < 1.5GB stable | `free -h` every minute |
| **CPU Usage** | < 80% average | `mpstat` |
| **Disk I/O Wait** | < 10% | `iostat -x 1` |
| **Error Rate** | < 0.1% | `journalctl -u graphdb | grep -i error | wc -l` |
| **Sync Lag** | < 100ms average | Supabase `graphdb_sync_health` view |
| **Data Consistency** | 100% match | Compare Supabase vs GraphDB counts |

---

## Phase 6: Disaster Recovery Drill

### 6.1 Backup GraphDB Data

**Option 1: Volume Snapshot (Recommended - fastest)**

```bash
# Using DigitalOcean CLI
doctl compute volume-snapshot create graphdb-data \
  --snapshot-name "graphdb-backup-$(date +%Y%m%d-%H%M%S)" \
  --desc "GraphDB staging backup before DR drill"

# No downtime required!
# Snapshot cost: $0.05/GB/month = $5/month for 100GB
```

**Option 2: Manual Backup**

```bash
# Stop GraphDB
sudo systemctl stop graphdb

# Create backup on volume's backup directory
sudo tar -czf /mnt/graphdb-data/backups/graphdb-backup-$(date +%Y%m%d-%H%M%S).tar.gz \
  /mnt/graphdb-data/data \
  /mnt/graphdb-data/wal \
  /mnt/graphdb-data/audit

# Verify backup
ls -lh /mnt/graphdb-data/backups/
tar -tzf /mnt/graphdb-data/backups/graphdb-backup-*.tar.gz | head -20

# Upload to DigitalOcean Spaces (S3-compatible) for off-site backup
# Using DO Spaces:
s3cmd put /mnt/graphdb-data/backups/graphdb-backup-*.tar.gz \
  s3://your-backup-bucket/graphdb/staging/

# Start GraphDB
sudo systemctl start graphdb
```

### 6.2 Simulate Disaster

```bash
# DESTRUCTIVE: Delete all data
sudo systemctl stop graphdb
sudo rm -rf /mnt/graphdb-data/data/*
sudo rm -rf /mnt/graphdb-data/wal/*
sudo rm -rf /mnt/graphdb-data/audit/*

# Verify data is gone
ls -la /mnt/graphdb-data/data/
ls -la /mnt/graphdb-data/wal/
# Should be empty

# Try to start (should fail or start with empty DB)
sudo systemctl start graphdb
curl http://127.0.0.1:8080/stats
# Should show: {"nodes": 0, "edges": 0}
```

### 6.3 Restore from Backup

**Option 1: Restore from Volume Snapshot (Recommended)**

```bash
# 1. Detach current volume
doctl compute volume-action detach <volume-id> <droplet-id>

# 2. Create new volume from snapshot
doctl compute volume create graphdb-data-restored \
  --region nyc1 \
  --size 100GiB \
  --snapshot <snapshot-id>

# 3. Attach restored volume
doctl compute volume-action attach <new-volume-id> <droplet-id>

# 4. Mount (device ID might change, check lsblk)
sudo mount /dev/disk/by-id/scsi-0DO_Volume_graphdb-data-restored /mnt/graphdb-data

# 5. Start GraphDB
sudo systemctl start graphdb

# 6. Verify data restored
curl http://127.0.0.1:8080/stats
# Should show original counts: {"nodes": 5000, "edges": 18000}
```

**Option 2: Restore from Manual Backup**

```bash
# Stop GraphDB
sudo systemctl stop graphdb

# Restore from backup tar
sudo tar -xzf /mnt/graphdb-data/backups/graphdb-backup-*.tar.gz -C /

# OR restore from DO Spaces
s3cmd get s3://your-backup-bucket/graphdb/staging/graphdb-backup-*.tar.gz /tmp/
sudo tar -xzf /tmp/graphdb-backup-*.tar.gz -C /

# Verify restoration
ls -la /mnt/graphdb-data/data/
sudo du -sh /mnt/graphdb-data/

# Fix permissions
sudo chown -R graphdb:graphdb /mnt/graphdb-data

# Start GraphDB
sudo systemctl start graphdb

# Verify data restored
curl http://127.0.0.1:8080/stats
# Should show original counts: {"nodes": 5000, "edges": 18000}

# Test queries
curl "http://127.0.0.1:8080/query" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (c:Concept) RETURN count(c) as total"}'
```

### 6.4 Alternative: Re-sync from Supabase

```bash
# If backup fails, can rebuild from Supabase (source of truth)
# 1. Start with empty GraphDB
sudo systemctl stop graphdb
sudo rm -rf /var/lib/graphdb/*
sudo systemctl start graphdb

# 2. Trigger full re-sync from Syntopica
curl -X POST https://staging.yourdomain.com/api/internal/sync/batch/all \
  -H "Authorization: Bearer $ADMIN_API_KEY"

# 3. Monitor sync progress
watch -n 5 'curl -s http://127.0.0.1:8080/stats | jq "{nodes, edges}"'

# 4. Verify sync complete
psql $SUPABASE_DATABASE_URL -c "SELECT * FROM get_graphdb_sync_status();"
```

---

## Phase 7: Performance Validation

### 7.1 Benchmark Queries

```bash
# Test key query patterns with timing
cat > ~/benchmark.sh <<'EOF'
#!/bin/bash

echo "=== GraphDB Performance Benchmark ==="
echo ""

# Single-hop related concepts (target: < 50ms)
echo "1. Related Concepts Query (1-hop):"
time curl -s "http://127.0.0.1:8080/query" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH (c:Concept {conceptId: 123})-[r]-(related:Concept) RETURN related LIMIT 20"
  }' | jq '.execution_time_ms'

echo ""

# Multi-hop shortest path (target: < 100ms for 2 hops)
echo "2. Shortest Path Query (2-3 hops):"
time curl -s "http://127.0.0.1:8080/query" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH path = shortestPath((a:Concept {conceptId: 123})-[*..3]-(b:Concept {conceptId: 456})) RETURN path"
  }' | jq '.execution_time_ms'

echo ""

# PageRank (target: < 200ms for 1000 nodes)
echo "3. PageRank Calculation (1000 nodes):"
time curl -s "http://127.0.0.1:8080/algorithms/pagerank" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "iterations": 20,
    "dampingFactor": 0.85
  }' | jq '.execution_time_ms'

echo ""

# Aggregation query
echo "4. Aggregation Query:"
time curl -s "http://127.0.0.1:8080/query" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH (c:Concept)-[r:PREREQUISITE]->(prereq) RETURN c.category, count(r) as prerequisites GROUP BY c.category ORDER BY prerequisites DESC"
  }' | jq '.execution_time_ms'

EOF

chmod +x ~/benchmark.sh
./benchmark.sh
```

---

## Phase 8: Production Cutover Checklist

Once staging soak test succeeds (48 hours stable), prepare for production:

### ✅ Pre-Deployment Checklist

- [ ] **Soak test passed**: 48 hours uptime, stable memory, < 100ms p95 latency
- [ ] **DR drill passed**: Backup/restore verified, < 5 min RTO
- [ ] **Performance validated**: All benchmark queries within targets
- [ ] **Sync verified**: 100% Supabase ↔ GraphDB consistency
- [ ] **Monitoring configured**: Prometheus + Grafana dashboards
- [ ] **Alerts configured**: Memory, CPU, latency, error rate thresholds
- [ ] **Documentation updated**: Runbook, escalation procedures
- [ ] **Team trained**: On-call engineers familiar with GraphDB operations

### Production Deployment

```bash
# 1. Provision production droplet (4GB / 2 vCPU)
doctl compute droplet create graphdb-production \
  --region nyc1 \
  --size s-2vcpu-4gb \
  --image ubuntu-22-04-x64 \
  --ssh-keys <your-ssh-key-id> \
  --enable-monitoring \
  --enable-ipv6 \
  --tag-names production,graphdb,syntopica

# 2. Repeat deployment steps (Phase 1-4)
# 3. Configure Cloudflare Tunnel: graphdb.yourdomain.com
# 4. Update Syntopica production Workers:
npx wrangler secret put GRAPHDB_URL --env production
# Enter: https://graphdb.yourdomain.com

# 5. Initial sync
curl -X POST https://yourdomain.com/api/internal/sync/batch/all \
  -H "Authorization: Bearer $ADMIN_API_KEY"

# 6. Gradual rollout (10% → 50% → 100% traffic)
# Use Cloudflare Load Balancer or Workers routing
```

---

## Troubleshooting

### GraphDB won't start

```bash
# Check logs
sudo journalctl -u graphdb -n 100 --no-pager

# Common issues:
# - Port 8080 already in use: lsof -i :8080
# - Permission denied: chown graphdb:graphdb /var/lib/graphdb
# - Out of memory: check dmesg | grep oom
```

### High memory usage

```bash
# Check process memory
ps aux | grep graphdb

# Reduce cache size in /etc/graphdb/config.yaml:
cache_size_mb: 128  # Reduce from 256

# Restart
sudo systemctl restart graphdb
```

### Tunnel connection issues

```bash
# Check cloudflared logs
sudo journalctl -u cloudflared -n 100 --no-pager

# Test tunnel
cloudflared tunnel info graphdb-staging

# Restart tunnel
sudo systemctl restart cloudflared
```

### Sync lag

```bash
# Check sync queue
psql $SUPABASE_DATABASE_URL -c "
  SELECT COUNT(*) as unsynced
  FROM global_concepts
  WHERE graphdb_synced_at IS NULL
"

# Manually trigger sync
curl -X POST https://staging.yourdomain.com/api/internal/sync/batch/concepts
```

---

## Cost Breakdown

**Monthly Costs (Staging)**:
- DigitalOcean Droplet (2GB RAM / 1 vCPU / 50GB SSD): $12/month
- DigitalOcean Volume (100GB block storage): $10/month
- Volume Snapshots (automated weekly, 4-week retention): $7/month
- Bandwidth (500GB included): $0
- Cloudflare Tunnel: Free
- **Total: $29/month** ✅

**Monthly Costs (Production - Year 1)**:
- DigitalOcean Droplet (4GB RAM / 2 vCPU / 80GB SSD): $24/month
- DigitalOcean Volume (200GB block storage): $20/month
- Volume Snapshots (automated weekly, 4-week retention): $14/month
- Bandwidth overage (estimated): $2/month
- Off-site backups (DigitalOcean Spaces, optional): $5/month
- **Total: ~$60-65/month**

**Alternative Costs**:
- Neo4j AuraDB (2GB): $65/month (comparable to our production setup)
- Neo4j AuraDB (4GB): $190/month
- AWS Neptune (db.t3.medium): ~$145/month

**ROI**:
- Year 1: Save ~$0-7/month vs Neo4j 2GB (break even)
- Year 2+: As you scale, Neo4j pricing increases significantly
- Full control over data, infrastructure, and compliance
- Can optimize costs by tuning resources based on actual usage

---

## Next Steps

1. **Create droplet**: Provision $12/month staging droplet on Digital Ocean
2. **Deploy GraphDB**: Follow Phase 1-4 (estimated: 2-3 hours)
3. **Sync data**: Initial batch sync from Supabase (estimated: 10-30 minutes)
4. **Start soak test**: Run 48-hour load test with monitoring
5. **DR drill**: Test backup/restore procedures
6. **Production planning**: If staging succeeds, provision production droplet

---

**Author:** Claude Code
**Last Updated:** 2025-11-24
**Status:** Ready for deployment
