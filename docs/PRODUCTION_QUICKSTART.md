# Cluso GraphDB - Production Quickstart

## Deploy Today, Update Tomorrow

This guide shows you how to deploy Cluso GraphDB to production TODAY and safely update it TOMORROW.

---

## Day 1: Initial Production Deployment

### Architecture: Minimal High-Availability Setup

```
                    ┌──────────────┐
                    │ Load Balancer│
                    │  (nginx)     │
                    └───────┬──────┘
                            │
        ┌───────────────────┼───────────────────┐
        │                   │                   │
   ┌────▼─────┐       ┌────▼─────┐       ┌────▼─────┐
   │ Primary  │       │ Replica1 │       │ Replica2 │
   │ :8080    │──────▶│ :8081    │       │ :8082    │
   │ (Write)  │       │ (Read)   │       │ (Read)   │
   └──────────┘       └──────────┘       └──────────┘
   /data/primary      /data/replica1     /data/replica2
```

### Prerequisites

**3 servers (or Docker containers):**
- CPU: 4 cores minimum
- RAM: 8GB minimum
- Disk: 100GB SSD (data can grow, plan accordingly)
- Network: Low latency between nodes (<10ms)

**Software:**
- Go 1.21+ (to build binaries)
- nginx or HAProxy (load balancer)
- systemd (for process management)

---

### Step 1: Build Binaries

```bash
# On your build machine
cd /path/to/cluso-graphdb

# Build primary server
go build -o graphdb-primary ./cmd/graphdb-primary

# Build replica server
go build -o graphdb-replica ./cmd/graphdb-replica

# Verify builds
./graphdb-primary --version
./graphdb-replica --version
```

---

### Step 2: Deploy Primary Node

**On primary server (e.g., 10.0.1.10):**

```bash
# Create data directory
sudo mkdir -p /var/lib/graphdb/primary
sudo chown $USER:$USER /var/lib/graphdb/primary

# Copy binary
sudo cp graphdb-primary /usr/local/bin/
sudo chmod +x /usr/local/bin/graphdb-primary

# Create systemd service
sudo tee /etc/systemd/system/graphdb-primary.service > /dev/null <<EOF
[Unit]
Description=Cluso GraphDB Primary Node
After=network.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/var/lib/graphdb/primary
ExecStart=/usr/local/bin/graphdb-primary \\
  --data=/var/lib/graphdb/primary \\
  --http=8080 \\
  --replication=:9090
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Resource limits
LimitNOFILE=65536
LimitNPROC=32768

[Install]
WantedBy=multi-user.target
EOF

# Create user
sudo useradd -r -s /bin/false graphdb
sudo chown -R graphdb:graphdb /var/lib/graphdb

# Start primary
sudo systemctl daemon-reload
sudo systemctl enable graphdb-primary
sudo systemctl start graphdb-primary

# Verify it's running
sudo systemctl status graphdb-primary
curl http://localhost:8080/health
```

**Expected output:**
```json
{
  "status": "healthy",
  "role": "primary",
  "version": "1.0.0",
  "uptime_seconds": 5
}
```

---

### Step 3: Deploy Replica Nodes

**On replica1 server (e.g., 10.0.1.11):**

```bash
# Create data directory
sudo mkdir -p /var/lib/graphdb/replica1
sudo useradd -r -s /bin/false graphdb
sudo chown -R graphdb:graphdb /var/lib/graphdb

# Copy binary
sudo cp graphdb-replica /usr/local/bin/
sudo chmod +x /usr/local/bin/graphdb-replica

# Create systemd service
sudo tee /etc/systemd/system/graphdb-replica.service > /dev/null <<EOF
[Unit]
Description=Cluso GraphDB Replica Node
After=network.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/var/lib/graphdb/replica1
ExecStart=/usr/local/bin/graphdb-replica \\
  --data=/var/lib/graphdb/replica1 \\
  --http=8081 \\
  --primary=10.0.1.10:9090 \\
  --id=replica-01
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

LimitNOFILE=65536
LimitNPROC=32768

[Install]
WantedBy=multi-user.target
EOF

# Start replica
sudo systemctl daemon-reload
sudo systemctl enable graphdb-replica
sudo systemctl start graphdb-replica

# Verify it's running
sudo systemctl status graphdb-replica
curl http://localhost:8081/health
```

**Expected output:**
```json
{
  "status": "healthy",
  "role": "replica",
  "connected": "connected",
  "primary": "10.0.1.10:9090"
}
```

**Repeat on replica2 server (10.0.1.12):**
- Change port to `8082`
- Change data dir to `/var/lib/graphdb/replica2`
- Change ID to `replica-02`

---

### Step 4: Configure Load Balancer

**On nginx server (or same as primary):**

```bash
# Install nginx
sudo apt-get install nginx  # Debian/Ubuntu
# or
sudo yum install nginx      # RHEL/CentOS

# Configure
sudo tee /etc/nginx/conf.d/graphdb.conf > /dev/null <<'EOF'
upstream graphdb_primary {
    server 10.0.1.10:8080 max_fails=3 fail_timeout=30s;
}

upstream graphdb_replicas {
    server 10.0.1.11:8081 max_fails=3 fail_timeout=30s;
    server 10.0.1.12:8082 max_fails=3 fail_timeout=30s;
}

server {
    listen 80;
    server_name graphdb.example.com;

    # Health check endpoint
    location /health {
        proxy_pass http://graphdb_primary;
    }

    # Write operations -> Primary only
    location ~ ^/api/(nodes|edges) {
        if ($request_method ~ ^(POST|PUT|DELETE)$) {
            proxy_pass http://graphdb_primary;
        }
        # GET requests -> Replicas
        proxy_pass http://graphdb_replicas;
    }

    # Replication status -> Primary
    location /replication {
        proxy_pass http://graphdb_primary;
    }

    # Queries -> Replicas (read-only)
    location /query {
        proxy_pass http://graphdb_replicas;
    }

    # Stats -> Any node
    location /stats {
        proxy_pass http://graphdb_replicas;
    }

    # Default -> Primary
    location / {
        proxy_pass http://graphdb_primary;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
EOF

# Test configuration
sudo nginx -t

# Reload nginx
sudo systemctl reload nginx
```

---

### Step 5: Verify Production Setup

```bash
# Check all nodes healthy
curl http://graphdb.example.com/health

# Check replication status
curl http://10.0.1.10:8080/replication/status | jq .

# Expected: All replicas connected with low lag
{
  "is_primary": true,
  "replica_count": 2,
  "replicas": [
    {
      "replica_id": "replica-01",
      "connected": true,
      "heartbeat_lag": 0,
      "lag_ms": 12
    },
    {
      "replica_id": "replica-02",
      "connected": true,
      "heartbeat_lag": 0,
      "lag_ms": 15
    }
  ]
}
```

**Test write operation:**
```bash
# Write to primary
curl -X POST http://graphdb.example.com/api/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["Customer"],
    "properties": {
      "name": "Test Customer",
      "email": "test@example.com"
    }
  }'

# Wait 1 second for replication
sleep 1

# Read from replica
curl http://10.0.1.11:8081/stats

# Verify node count increased
```

---

### Step 6: Setup Monitoring (Optional but Recommended)

**Create monitoring script:**

```bash
sudo tee /usr/local/bin/monitor-graphdb.sh > /dev/null <<'EOF'
#!/bin/bash

PRIMARY="http://10.0.1.10:8080"
ALERT_EMAIL="ops@example.com"

# Check primary health
if ! curl -sf $PRIMARY/health > /dev/null; then
    echo "PRIMARY DOWN" | mail -s "GraphDB Alert" $ALERT_EMAIL
    exit 1
fi

# Check replication lag
MAX_LAG=$(curl -s $PRIMARY/replication/status | jq '[.replicas[].heartbeat_lag] | max')

if [ "$MAX_LAG" -gt 10 ]; then
    echo "HIGH REPLICATION LAG: $MAX_LAG" | mail -s "GraphDB Alert" $ALERT_EMAIL
fi

# Check for disconnected replicas
DISCONNECTED=$(curl -s $PRIMARY/replication/status | jq '[.replicas[] | select(.connected == false)] | length')

if [ "$DISCONNECTED" -gt 0 ]; then
    echo "REPLICAS DISCONNECTED: $DISCONNECTED" | mail -s "GraphDB Alert" $ALERT_EMAIL
fi
EOF

sudo chmod +x /usr/local/bin/monitor-graphdb.sh

# Add to cron (run every minute)
echo "* * * * * /usr/local/bin/monitor-graphdb.sh" | sudo crontab -
```

---

### Step 7: Setup Backups

**Daily backup script:**

```bash
sudo tee /usr/local/bin/backup-graphdb.sh > /dev/null <<'EOF'
#!/bin/bash

BACKUP_DIR="/var/backups/graphdb"
DATE=$(date +%Y%m%d-%H%M%S)
PRIMARY_DATA="/var/lib/graphdb/primary"

# Create backup directory
mkdir -p $BACKUP_DIR

# Trigger snapshot on primary
curl -X POST http://localhost:8080/snapshot

# Wait for snapshot to complete
sleep 5

# Backup data directory
tar -czf $BACKUP_DIR/graphdb-$DATE.tar.gz $PRIMARY_DATA

# Keep only last 7 days
find $BACKUP_DIR -name "graphdb-*.tar.gz" -mtime +7 -delete

echo "Backup completed: graphdb-$DATE.tar.gz"
EOF

sudo chmod +x /usr/local/bin/backup-graphdb.sh

# Add to cron (run daily at 2am)
echo "0 2 * * * /usr/local/bin/backup-graphdb.sh" | sudo crontab -
```

---

## Day 2: Update Production Database

You've discovered a bug or want to deploy a new feature. Here's how to update safely.

### Update Scenario: Deploy v1.1 with Zero Downtime

**New binaries available:**
- `graphdb-primary-v1.1`
- `graphdb-replica-v1.1`

---

### Step 1: Pre-Update Checks

```bash
# 1. Verify current version
curl http://10.0.1.10:8080/health | jq '.version'
# Output: "1.0.0"

# 2. Check replication healthy
curl http://10.0.1.10:8080/replication/status | jq '.replicas[] | {id, connected, lag: .heartbeat_lag}'

# 3. Create backup (run backup script manually)
sudo /usr/local/bin/backup-graphdb.sh

# 4. Test new version in staging (if available)
# ... staging tests pass ...
```

---

### Step 2: Update Replica1 (Zero Downtime)

**On replica1 server (10.0.1.11):**

```bash
# Stop replica
sudo systemctl stop graphdb-replica

# Backup current binary
sudo cp /usr/local/bin/graphdb-replica /usr/local/bin/graphdb-replica.v1.0.backup

# Backup data (optional but safe)
sudo tar -czf /tmp/replica1-backup-$(date +%Y%m%d).tar.gz /var/lib/graphdb/replica1

# Install new binary
sudo cp graphdb-replica-v1.1 /usr/local/bin/graphdb-replica

# Start upgraded replica
sudo systemctl start graphdb-replica

# Monitor logs for errors
sudo journalctl -u graphdb-replica -f
# Watch for: "Connected to primary", no error messages

# Check health
curl http://localhost:8081/health | jq .
# Expected: version "1.1.0", connected "connected"

# Check lag on primary
curl http://10.0.1.10:8080/replication/status | jq '.replicas[] | select(.replica_id=="replica-01")'
# Expected: connected: true, heartbeat_lag < 5
```

**Wait 2-3 minutes and verify replica1 is stable.**

---

### Step 3: Update Replica2 (Zero Downtime)

**On replica2 server (10.0.1.12):**

```bash
# Same process as replica1
sudo systemctl stop graphdb-replica
sudo cp /usr/local/bin/graphdb-replica /usr/local/bin/graphdb-replica.v1.0.backup
sudo cp graphdb-replica-v1.1 /usr/local/bin/graphdb-replica
sudo systemctl start graphdb-replica

# Verify
curl http://localhost:8082/health | jq .
curl http://10.0.1.10:8080/replication/status | jq '.replicas[] | select(.replica_id=="replica-02")'
```

**Current state:**
```
Primary: v1.0 (still serving writes)
Replica1: v1.1 ✓
Replica2: v1.1 ✓
```

---

### Step 4: Promote Replica1 to Primary (5-Second Switchover)

**This is the only brief downtime window**

**On monitoring terminal, watch replication:**
```bash
# Terminal 1: Monitor replication lag
watch 'curl -s http://10.0.1.10:8080/replication/status | jq ".replicas[] | {id, lag: .heartbeat_lag}"'
```

**Execute promotion:**

```bash
# Terminal 2: Execute these commands

# 1. Stop writes to old primary (optional - enables graceful drain)
# For now we'll do a quick stop since replication is realtime

# 2. Wait for lag = 0 (should already be 0)
curl -s http://10.0.1.10:8080/replication/status | jq '.replicas[0].heartbeat_lag'
# Verify: 0

# 3. Stop old primary
ssh 10.0.1.10 'sudo systemctl stop graphdb-primary'

# 4. Promote replica1 to primary
ssh 10.0.1.11 'sudo systemctl stop graphdb-replica'

# Update replica1 config to primary mode
ssh 10.0.1.11 'sudo tee /etc/systemd/system/graphdb-primary.service > /dev/null <<EOF
[Unit]
Description=Cluso GraphDB Primary Node
After=network.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/var/lib/graphdb/replica1
ExecStart=/usr/local/bin/graphdb-primary \\
  --data=/var/lib/graphdb/replica1 \\
  --http=8081 \\
  --replication=:9090
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF'

# Start new primary
ssh 10.0.1.11 'sudo systemctl daemon-reload && sudo systemctl start graphdb-primary'

# Verify new primary
curl http://10.0.1.11:8081/health
# Expected: role: "primary"
```

**Downtime: ~5 seconds** (time between stopping old primary and starting new)

---

### Step 5: Update Load Balancer

**On nginx server:**

```bash
# Update upstream primary address
sudo sed -i 's/10.0.1.10:8080/10.0.1.11:8081/' /etc/nginx/conf.d/graphdb.conf

# Test configuration
sudo nginx -t

# Reload (no downtime)
sudo systemctl reload nginx

# Verify traffic flowing to new primary
curl http://graphdb.example.com/health
# Expected: role: "primary", version: "1.1.0"
```

---

### Step 6: Upgrade Old Primary as New Replica

**On old primary server (10.0.1.10):**

```bash
# Install new binary
sudo cp graphdb-replica-v1.1 /usr/local/bin/graphdb-replica

# Create replica service
sudo tee /etc/systemd/system/graphdb-replica.service > /dev/null <<EOF
[Unit]
Description=Cluso GraphDB Replica Node
After=network.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/var/lib/graphdb/primary
ExecStart=/usr/local/bin/graphdb-replica \\
  --data=/var/lib/graphdb/primary \\
  --http=8080 \\
  --primary=10.0.1.11:9090 \\
  --id=replica-03
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Start as replica
sudo systemctl daemon-reload
sudo systemctl start graphdb-replica

# Verify connection
curl http://localhost:8080/health
# Expected: role: "replica", connected: "connected"

# Check on new primary
curl http://10.0.1.11:8081/replication/status | jq .
# Expected: 3 replicas connected
```

---

### Step 7: Final Verification

```bash
# All nodes upgraded
curl http://10.0.1.11:8081/health | jq .version  # "1.1.0" (primary)
curl http://10.0.1.10:8080/health | jq .version  # "1.1.0" (replica)
curl http://10.0.1.12:8082/health | jq .version  # "1.1.0" (replica)

# All replicas healthy
curl http://10.0.1.11:8081/replication/status | jq '.replicas[] | {id, connected, lag: .heartbeat_lag}'

# Test write operation
curl -X POST http://graphdb.example.com/api/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["TestV1.1"],
    "properties": {"updated": true}
  }'

# Test read from replica
curl http://10.0.1.10:8080/stats | jq .
```

**Upgrade complete!** ✓

---

## Summary: What Just Happened

**Day 1:** Deployed 3-node cluster with primary + 2 replicas

**Day 2 Update Timeline:**
```
00:00 - Started update process
00:02 - Replica1 upgraded (no customer impact)
00:05 - Replica2 upgraded (no customer impact)
00:08 - Promoted replica1 to primary (~5 sec downtime)
00:09 - Updated load balancer (no downtime)
00:11 - Upgraded old primary as replica (no customer impact)
00:13 - Update complete, all nodes v1.1

Total customer downtime: ~5 seconds
```

---

## Rollback Procedure (If Something Goes Wrong)

**If replica upgrade fails:**
```bash
# Stop failed replica
sudo systemctl stop graphdb-replica

# Restore old binary
sudo cp /usr/local/bin/graphdb-replica.v1.0.backup /usr/local/bin/graphdb-replica

# Restart
sudo systemctl start graphdb-replica
```

**If promotion fails:**
```bash
# Start old primary again
ssh 10.0.1.10 'sudo systemctl start graphdb-primary'

# Demote new "primary" back to replica
ssh 10.0.1.11 'sudo systemctl stop graphdb-primary && sudo systemctl start graphdb-replica'

# Restore nginx config
sudo sed -i 's/10.0.1.11:8081/10.0.1.10:8080/' /etc/nginx/conf.d/graphdb.conf
sudo systemctl reload nginx
```

**If complete disaster:**
```bash
# Restore from backup
sudo systemctl stop graphdb-primary
sudo rm -rf /var/lib/graphdb/primary
sudo tar -xzf /var/backups/graphdb/graphdb-YYYYMMDD-HHMMSS.tar.gz -C /
sudo systemctl start graphdb-primary
```

---

## Production Checklist

### Day 1 (Deployment)
- [ ] 3 servers provisioned
- [ ] Binaries built and tested
- [ ] Primary node running
- [ ] 2 replica nodes running and connected
- [ ] Load balancer configured
- [ ] Monitoring script deployed
- [ ] Backup script deployed
- [ ] Health checks passing
- [ ] Write/read tests successful

### Day 2 (Update)
- [ ] Backup created
- [ ] Replication lag = 0
- [ ] Replica1 upgraded and healthy
- [ ] Replica2 upgraded and healthy
- [ ] Promotion successful (check logs)
- [ ] Load balancer updated
- [ ] Old primary rejoined as replica
- [ ] All 3 nodes running v1.1
- [ ] Write/read tests successful
- [ ] Monitoring shows healthy state

---

## Troubleshooting

**Replica won't connect after upgrade:**
```bash
# Check network connectivity
telnet 10.0.1.10 9090

# Check logs for error details
sudo journalctl -u graphdb-replica -n 100

# Common issues:
# - Firewall blocking port 9090
# - Wrong primary address in config
# - Version incompatibility (check release notes)
```

**High replication lag:**
```bash
# Check primary write rate
curl http://primary:8080/stats | jq .

# Check replica CPU/disk
top
iostat -x 1

# Common causes:
# - High write volume (normal, wait for catch-up)
# - Slow disk on replica
# - Network congestion
```

**Need to restore from backup:**
```bash
# Stop node
sudo systemctl stop graphdb-primary

# Restore data
sudo rm -rf /var/lib/graphdb/primary
sudo tar -xzf /var/backups/graphdb/backup.tar.gz -C /

# Restart
sudo systemctl start graphdb-primary
```

---

## Next Steps

**After successful deployment:**

1. **Monitor for 24 hours** - Watch metrics, replication lag, error rates
2. **Test failover** - Intentionally stop primary, verify replica promotion works
3. **Load test** - Verify performance under production traffic
4. **Document your setup** - Server IPs, ports, credentials
5. **Train team** - Make sure ops team knows how to check status, deploy updates

**Future improvements:**

- Add more replicas for higher read capacity
- Setup multi-region replication for disaster recovery
- Implement automated failover with consensus
- Add metrics dashboard (Prometheus + Grafana)
- Setup alerting (PagerDuty, Slack)

---

## Questions?

Common questions addressed in other docs:

- Data format compatibility → See `UPGRADE_GUIDE.md`
- Replication protocol details → See `pkg/replication/protocol.go`
- Backup/restore internals → See data persistence analysis
- Performance tuning → See `PERFORMANCE.md` (if exists)

**You're production ready!** 🚀
