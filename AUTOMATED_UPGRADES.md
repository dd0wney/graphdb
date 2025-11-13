# Automated Upgrade System

Cluso GraphDB includes a comprehensive automated upgrade system that eliminates manual steps and reduces downtime to near-zero through intelligent orchestration and multiple deployment strategies.

## Table of Contents

1. [Overview](#overview)
2. [Components](#components)
3. [Upgrade Strategies](#upgrade-strategies)
4. [Quick Start](#quick-start)
5. [API Reference](#api-reference)
6. [Advanced Usage](#advanced-usage)

---

## Overview

The automated upgrade system provides:

- **Zero-downtime upgrades** through coordinated failover
- **One-command deployments** with automatic health checks
- **Blue-green deployment support** for instant rollback
- **Graceful shutdown** with connection draining
- **Automated promotion/demotion** of nodes
- **Real-time upgrade status** monitoring

**Traditional vs Automated Upgrade:**

| Aspect | Manual Process | Automated Process |
|--------|---------------|-------------------|
| Commands | 15+ SSH commands | 1 command |
| Downtime | ~30-60 seconds | ~2-5 seconds |
| Human errors | High risk | Automated checks |
| Rollback time | 5-10 minutes | Instant (blue-green) |
| Monitoring | Manual checking | Real-time status |

---

## Components

### 1. Admin API (`pkg/admin/`)

Provides HTTP endpoints for upgrade operations:

- `/admin/upgrade/status` - Get upgrade readiness
- `/admin/upgrade/promote` - Promote replica to primary
- `/admin/upgrade/stepdown` - Demote primary to replica
- `/admin/bluegreen/status` - Blue-green deployment status
- `/admin/bluegreen/switch` - Switch active deployment

### 2. Orchestration CLI (`cmd/graphdb-upgrade/`)

Command-line tool for cluster-wide upgrades:

```bash
graphdb-upgrade --cluster cluster.yaml --version v1.2.0
```

### 3. Graceful Server (`pkg/server/graceful.go`)

Handles signal-based graceful shutdown:

- `SIGTERM`/`SIGINT` - Standard shutdown
- `SIGUSR1` - Rolling restart preparation
- `SIGHUP` - Configuration reload

### 4. Blue-Green Manager (`pkg/admin/bluegreen.go`)

Manages parallel deployments for instant switching:

- Run two versions simultaneously
- Switch traffic atomically
- Instant rollback capability

---

## Upgrade Strategies

### Strategy 1: Rolling Upgrade (Default)

**Best for:** Most production deployments

**Downtime:** ~5 seconds during primary switchover

**Process:**
```bash
# 1. Create cluster configuration
cat > cluster.yaml <<EOF
nodes:
  - name: primary
    host: 10.0.1.10
    http_port: 8080
    role: primary
  - name: replica1
    host: 10.0.1.11
    http_port: 8080
    role: replica
  - name: replica2
    host: 10.0.1.12
    http_port: 8080
    role: replica
EOF

# 2. Preview upgrade plan
graphdb-upgrade --cluster cluster.yaml --version v1.2.0 --dry-run

# 3. Execute upgrade
graphdb-upgrade --cluster cluster.yaml --version v1.2.0
```

**What happens:**
1. Replicas upgraded sequentially while primary serves traffic
2. First replica promoted to new primary (~5s downtime)
3. Old primary demoted and upgraded
4. Cluster running on new version with all replicas

### Strategy 2: Blue-Green Deployment

**Best for:** Zero-downtime requirements, easy rollback

**Downtime:** 0 seconds (instant switch)

**Process:**
```bash
# 1. Deploy new version on "green" environment (port 8081)
# while "blue" (port 8080) continues serving traffic

# 2. Check status
curl http://primary:8080/admin/bluegreen/status

# 3. Verify green is healthy
curl http://primary:8081/health

# 4. Switch traffic to green
curl -X POST http://primary:8080/admin/bluegreen/switch \
  -H "Content-Type: application/json" \
  -d '{"target_color": "green", "drain_time": 5000000000}'

# 5. If issues arise, instant rollback:
curl -X POST http://primary:8080/admin/bluegreen/switch \
  -H "Content-Type: application/json" \
  -d '{"target_color": "blue"}'
```

### Strategy 3: Graceful Rolling Restart

**Best for:** Quick patches, configuration updates

**Downtime:** 0 seconds (no primary change)

**Process:**
```bash
# 1. Signal node for rolling restart
ssh replica1 'kill -USR1 $(cat /var/run/graphdb.pid)'

# 2. Wait for graceful shutdown (drains connections)
# 3. Replace binary
ssh replica1 'cp /tmp/graphdb-v1.2.0 /usr/local/bin/graphdb'

# 4. Restart service
ssh replica1 'systemctl start graphdb'

# 5. Verify health
curl http://replica1:8080/admin/upgrade/status
```

---

## Quick Start

### Basic Rolling Upgrade

```bash
# 1. Install the upgrade CLI
go install github.com/dd0wney/cluso-graphdb/cmd/graphdb-upgrade@latest

# 2. Create cluster.yaml
cat > cluster.yaml <<'EOF'
nodes:
  - name: primary
    host: prod-db-1
    http_port: 8080
    role: primary
  - name: replica1
    host: prod-db-2
    http_port: 8080
    role: replica
  - name: replica2
    host: prod-db-3
    http_port: 8080
    role: replica
EOF

# 3. Run upgrade
graphdb-upgrade --cluster cluster.yaml --version v1.2.0
```

### Manual API-Driven Upgrade

For more control, use the admin API directly:

```bash
# 1. Check replica readiness
curl http://replica1:8080/admin/upgrade/status

# Response:
# {
#   "phase": "replica_running",
#   "ready": true,
#   "replication_lag_ms": 45,
#   "heartbeat_lag": 1,
#   "can_promote": true,
#   "message": "Replica caught up, ready for promotion"
# }

# 2. Promote replica to primary
curl -X POST http://replica1:8080/admin/upgrade/promote \
  -H "Content-Type: application/json" \
  -d '{"wait_for_sync": true, "timeout": 60000000000}'

# Response:
# {
#   "success": true,
#   "new_role": "primary",
#   "previous_role": "replica",
#   "message": "Successfully promoted to primary",
#   "waited_seconds": 2.3
# }

# 3. Demote old primary
curl -X POST http://old-primary:8080/admin/upgrade/stepdown \
  -H "Content-Type: application/json" \
  -d '{"new_primary_id": "replica1:9090", "timeout": 30000000000}'
```

---

## API Reference

### GET /admin/upgrade/status

Get current upgrade readiness status.

**Response:**
```json
{
  "phase": "replica_running",
  "ready": true,
  "replication_lag_ms": 45,
  "heartbeat_lag": 1,
  "message": "Replica caught up, ready for promotion",
  "timestamp": "2025-01-15T10:30:00Z",
  "can_promote": true,
  "current_role": "replica",
  "connected_replicas": 0
}
```

**Fields:**
- `ready`: Can safely proceed with upgrade
- `can_promote`: Can be promoted to primary (replicas) or step down (primary)
- `replication_lag_ms`: Milliseconds behind primary
- `heartbeat_lag`: Number of missed heartbeats

### POST /admin/upgrade/promote

Promote this replica to primary.

**Request:**
```json
{
  "wait_for_sync": true,
  "timeout": 60000000000
}
```

**Response:**
```json
{
  "success": true,
  "new_role": "primary",
  "previous_role": "replica",
  "message": "Successfully promoted to primary",
  "promoted_at": "2025-01-15T10:30:05Z",
  "waited_seconds": 2.3
}
```

### POST /admin/upgrade/stepdown

Demote primary to replica.

**Request:**
```json
{
  "new_primary_id": "10.0.1.11:9090",
  "timeout": 30000000000
}
```

**Response:**
```json
{
  "success": true,
  "new_role": "replica",
  "previous_role": "primary",
  "message": "Successfully demoted to replica, following 10.0.1.11:9090",
  "stepped_down_at": "2025-01-15T10:30:10Z"
}
```

### GET /admin/bluegreen/status

Get blue-green deployment status.

**Response:**
```json
{
  "current_active": "blue",
  "blue": {
    "color": "blue",
    "port": 8080,
    "version": "v1.1.0",
    "active": true,
    "healthy": true,
    "node_count": 1000,
    "edge_count": 5000,
    "last_checked": "2025-01-15T10:30:00Z"
  },
  "green": {
    "color": "green",
    "port": 8081,
    "version": "v1.2.0",
    "active": false,
    "healthy": true,
    "node_count": 1000,
    "edge_count": 5000,
    "last_checked": "2025-01-15T10:30:00Z"
  },
  "can_switch": true
}
```

### POST /admin/bluegreen/switch

Switch active deployment.

**Request:**
```json
{
  "target_color": "green",
  "timeout": 30000000000,
  "drain_time": 5000000000
}
```

**Response:**
```json
{
  "success": true,
  "previous_color": "blue",
  "new_color": "green",
  "message": "Successfully switched from blue to green",
  "switched_at": "2025-01-15T10:30:15Z"
}
```

---

## Advanced Usage

### Custom Upgrade Scripts

Integrate with the orchestration tool:

```bash
#!/bin/bash
# upgrade-with-backup.sh

CLUSTER="cluster.yaml"
VERSION="$1"

# 1. Create backup
echo "Creating pre-upgrade backup..."
curl -X POST http://primary:8080/snapshot

# 2. Run upgrade
echo "Starting automated upgrade to $VERSION..."
graphdb-upgrade --cluster $CLUSTER --version $VERSION

# 3. Verify
echo "Verifying cluster health..."
for node in primary replica1 replica2; do
  STATUS=$(curl -s http://$node:8080/admin/upgrade/status)
  echo "$node: $STATUS"
done

echo "Upgrade complete!"
```

### Integration with CI/CD

**GitHub Actions Example:**

```yaml
name: Deploy to Production

on:
  release:
    types: [published]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build binaries
        run: make build

      - name: Deploy to cluster
        env:
          CLUSTER_CONFIG: ${{ secrets.CLUSTER_CONFIG }}
        run: |
          echo "$CLUSTER_CONFIG" > cluster.yaml
          graphdb-upgrade --cluster cluster.yaml --version ${{ github.ref_name }}
```

### Monitoring During Upgrade

Real-time monitoring script:

```bash
#!/bin/bash
# monitor-upgrade.sh

NODES="primary replica1 replica2"

while true; do
  clear
  echo "=== Cluster Upgrade Status ==="
  date
  echo

  for node in $NODES; do
    echo "[$node]"
    curl -s http://$node:8080/admin/upgrade/status | \
      jq -r '"\(.current_role) | Ready: \(.ready) | Lag: \(.replication_lag_ms)ms | Message: \(.message)"'
    echo
  done

  sleep 2
done
```

### Rollback Procedure

If upgrade fails:

```bash
# 1. Quick rollback with blue-green
curl -X POST http://primary:8080/admin/bluegreen/switch \
  -d '{"target_color": "blue"}'

# 2. Or restore from backup
curl -X POST http://primary:8080/admin/restore \
  -d '{"snapshot_id": "backup-pre-upgrade"}'

# 3. Restart all nodes with old binary
for node in primary replica1 replica2; do
  ssh $node 'cp /usr/local/bin/graphdb.backup /usr/local/bin/graphdb && systemctl restart graphdb'
done
```

---

## Best Practices

### DO

✅ **Test upgrades in staging first**
```bash
graphdb-upgrade --cluster staging-cluster.yaml --version v1.2.0
```

✅ **Use dry-run to preview changes**
```bash
graphdb-upgrade --cluster cluster.yaml --version v1.2.0 --dry-run
```

✅ **Monitor replication lag before promotion**
```bash
while true; do
  curl -s http://replica1:8080/admin/upgrade/status | jq '.replication_lag_ms'
  sleep 1
done
```

✅ **Keep backups before major upgrades**
```bash
curl -X POST http://primary:8080/snapshot
```

✅ **Use blue-green for critical updates**
- Deploy new version alongside old
- Verify thoroughly
- Switch with zero downtime

### DON'T

❌ **Don't upgrade all replicas simultaneously**
- Use the orchestration tool which upgrades sequentially

❌ **Don't promote without checking replication lag**
- Tool automatically waits for sync, but verify manually if needed

❌ **Don't skip health checks**
- Always verify `/health` endpoint returns 200 OK

❌ **Don't forget to update load balancer**
- Orchestration tool handles this, but verify manually

---

## Troubleshooting

### Promotion Fails

**Symptom:** Replica promotion returns error

**Solution:**
```bash
# Check replication status
curl http://replica1:8080/admin/upgrade/status

# Verify primary is reachable
curl http://primary:8080/health

# Check replication lag
curl http://primary:8080/replication/status

# If lag is high, wait longer
curl -X POST http://replica1:8080/admin/upgrade/promote \
  -d '{"wait_for_sync": true, "timeout": 120000000000}'
```

### Blue-Green Switch Fails

**Symptom:** Cannot switch to green deployment

**Solution:**
```bash
# Check both deployments
curl http://localhost:8080/admin/bluegreen/status

# Verify green is healthy
curl http://localhost:8081/health

# Check green logs
journalctl -u graphdb-green -f
```

### Graceful Shutdown Timeout

**Symptom:** Node doesn't shut down within timeout

**Solution:**
```bash
# Check active connections
ss -tn | grep :8080

# Increase drain time
curl -X POST http://node:8080/admin/bluegreen/switch \
  -d '{"target_color": "green", "drain_time": 30000000000}'

# Or force shutdown
systemctl stop graphdb
```

---

## Summary

The automated upgrade system transforms complex manual procedures into simple one-command operations:

**Before:**
```bash
# 15+ manual SSH commands
# 30-60 seconds downtime
# High risk of errors
# 5-10 minutes to rollback
```

**After:**
```bash
graphdb-upgrade --cluster cluster.yaml --version v1.2.0
# 2-5 seconds downtime
# Automated health checks
# Instant rollback (blue-green)
```

For questions or advanced scenarios, refer to the [UPGRADE_GUIDE.md](UPGRADE_GUIDE.md) for detailed manual procedures.
