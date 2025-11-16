# Cluso GraphDB Upgrade Guide

## Zero-Downtime Upgrade Strategy

This guide describes how to upgrade Cluso GraphDB in production without losing customer data or causing extended downtime.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Compatibility Considerations](#compatibility-considerations)
3. [Rolling Upgrade Procedure](#rolling-upgrade-procedure)
4. [Rollback Strategy](#rollback-strategy)
5. [Version-Specific Notes](#version-specific-notes)

---

## Prerequisites

### Required Setup
- **Minimum 2 replicas** running for zero-downtime upgrades
- **Monitoring** of replication lag (via `/replication/status` endpoint)
- **Backup** of data directory before upgrade
- **Load balancer** that can redirect traffic between primary nodes

### Pre-Upgrade Checklist

```bash
# 1. Verify current version
curl http://primary:8080/health

# 2. Check replication status (all replicas healthy)
curl http://primary:8080/replication/status | jq .

# 3. Create snapshot backup
curl -X POST http://primary:8080/snapshot

# 4. Backup data directory
tar -czf backup-$(date +%Y%m%d-%H%M%S).tar.gz /path/to/data

# 5. Verify disk space (upgrade may need temp space for compaction)
df -h /path/to/data
```

---

## Compatibility Considerations

### Data Format Changes

Cluso GraphDB currently has **limited version compatibility**. Before upgrading:

1. **Review release notes** for breaking changes
2. **Check these components** for format changes:
   - WAL entry format (`pkg/wal/`)
   - SSTable structure (`pkg/lsm/sstable.go`)
   - Snapshot JSON schema (`pkg/storage/`)
   - Node/Edge structure fields

### Compatibility Matrix

| Upgrade Path | Data Compatibility | Replication Compatibility | Risk |
|--------------|-------------------|---------------------------|------|
| v1.0 → v1.1 (patch) | ✅ Full | ✅ Full | Low |
| v1.0 → v1.x (minor) | ⚠️  Check notes | ⚠️  Test required | Medium |
| v1.0 → v2.0 (major) | ❌ Migration needed | ❌ Incompatible | High |

### Breaking Change Indicators

**HIGH RISK** - Requires export/import:
- Changes to `Node` or `Edge` struct fields
- WAL entry format modifications
- SSTable header changes
- Snapshot JSON schema changes

**MEDIUM RISK** - Requires testing:
- Replication protocol changes
- Query execution plan changes
- Index structure changes

**LOW RISK** - Safe for rolling upgrade:
- Bug fixes
- Performance improvements
- Configuration additions
- New optional features

---

## Rolling Upgrade Procedure

### Architecture Overview

```
                                    ┌─────────────┐
                                    │ Load        │
    Client Traffic ────────────────▶│ Balancer    │
                                    └──────┬──────┘
                                           │
                    ┌──────────────────────┼──────────────────────┐
                    │                      │                      │
              ┌─────▼─────┐          ┌────▼──────┐        ┌──────▼─────┐
              │ Primary   │          │ Replica1  │        │ Replica2   │
              │ (Write)   │─────────▶│ (Read)    │        │ (Read)     │
              │           │          │           │        │            │
              └───────────┘          └───────────┘        └────────────┘
                  v1.0                   v1.0                  v1.0

                           UPGRADE REPLICAS FIRST
                                      ↓
              ┌───────────┐          ┌───────────┐        ┌────────────┐
              │ Primary   │          │ Replica1  │        │ Replica2   │
              │           │─────────▶│           │        │            │
              │           │          │           │        │            │
              └───────────┘          └───────────┘        └────────────┘
                  v1.0                   v1.1 ✓                v1.1 ✓

                        PROMOTE REPLICA → PRIMARY
                                      ↓
              ┌───────────┐          ┌───────────┐        ┌────────────┐
              │ Replica   │◀─────────│ Primary   │        │ Replica2   │
              │ (v1.1)    │          │ (v1.1)    │───────▶│ (v1.1)     │
              │           │          │ PROMOTED  │        │            │
              └───────────┘          └───────────┘        └────────────┘
```

### Step-by-Step Procedure

#### Phase 1: Upgrade Replicas (No Downtime)

**For each replica:**

```bash
# 1. Stop replica process
systemctl stop graphdb-replica@1

# 2. Backup data directory
cp -r /var/lib/graphdb/replica1 /var/lib/graphdb/replica1.backup

# 3. Install new binary
cp /tmp/graphdb-replica-v1.1 /usr/local/bin/graphdb-replica

# 4. Start upgraded replica
systemctl start graphdb-replica@1

# 5. Monitor replication status
watch 'curl -s http://primary:8080/replication/status | jq ".replicas[] | select(.replica_id==\"replica1\")"'

# 6. Verify metrics:
#    - connected: true
#    - heartbeat_lag: <5
#    - lag_ms: <1000
```

**Wait for replica to catch up** before upgrading next replica:
```bash
# Check lag is minimal
curl http://primary:8080/replication/status | jq '.replicas[] | {id: .replica_id, lag: .heartbeat_lag, connected: .connected}'
```

#### Phase 2: Promote Replica to Primary (Brief Switchover)

**Downtime window: ~5 seconds**

```bash
# 1. Enable maintenance mode (optional - stop new writes)
curl -X POST http://primary:8080/maintenance/enable

# 2. Wait for replication lag = 0
while true; do
  LAG=$(curl -s http://primary:8080/replication/status | jq '.replicas[0].heartbeat_lag')
  if [ "$LAG" -eq 0 ]; then
    echo "Replicas caught up!"
    break
  fi
  echo "Waiting for replication to catch up (lag=$LAG)..."
  sleep 1
done

# 3. Stop old primary (graceful shutdown writes snapshot)
systemctl stop graphdb-primary

# 4. Promote replica to primary
#    - Update config: IsPrimary=true
#    - Restart as primary
sed -i 's/"is_primary": false/"is_primary": true/' /etc/graphdb/replica1.json
systemctl stop graphdb-replica@1
systemctl start graphdb-primary@1

# 5. Update load balancer to point to new primary
#    (Implementation depends on your LB - HAProxy, nginx, etc.)
lb-update-primary replica1-host:8080

# 6. Verify new primary is serving traffic
curl http://new-primary:8080/health
curl -X POST http://new-primary:8080/api/nodes -d '{"labels":["Test"]}'

# 7. Upgrade old primary to v1.1 and reconnect as replica
cp /tmp/graphdb-replica-v1.1 /usr/local/bin/graphdb-replica
# Update config: IsPrimary=false, PrimaryAddr=new-primary:9090
systemctl start graphdb-replica@old-primary
```

#### Phase 3: Verification

```bash
# 1. Check all replicas connected
curl http://new-primary:8080/replication/status | jq .

# 2. Verify data integrity
curl http://new-primary:8080/stats

# 3. Test write + read cycle
curl -X POST http://new-primary:8080/api/nodes -d '{"labels":["UpgradeTest"]}'

# 4. Disable maintenance mode
curl -X POST http://new-primary:8080/maintenance/disable
```

---

## Rollback Strategy

### If Upgrade Fails on Replica

```bash
# 1. Stop failed replica
systemctl stop graphdb-replica@1

# 2. Restore from backup
rm -rf /var/lib/graphdb/replica1
cp -r /var/lib/graphdb/replica1.backup /var/lib/graphdb/replica1

# 3. Restore old binary
cp /usr/local/bin/graphdb-replica.backup /usr/local/bin/graphdb-replica

# 4. Restart
systemctl start graphdb-replica@1
```

### If Promotion Fails

```bash
# 1. Demote new primary back to replica
systemctl stop graphdb-primary@1
sed -i 's/"is_primary": true/"is_primary": false/' /etc/graphdb/replica1.json
systemctl start graphdb-replica@1

# 2. Restart old primary
systemctl start graphdb-primary

# 3. Update load balancer back to old primary
lb-update-primary old-primary-host:8080

# 4. Verify old primary serving traffic
curl http://old-primary:8080/health
```

### Data Recovery from Backup

**If catastrophic failure:**

```bash
# 1. Stop all nodes
systemctl stop graphdb-primary
systemctl stop graphdb-replica@*

# 2. Restore primary from backup
tar -xzf backup-20251113-094530.tar.gz -C /var/lib/graphdb/primary

# 3. Start primary
systemctl start graphdb-primary

# 4. Let replicas reconnect and sync
systemctl start graphdb-replica@1
systemctl start graphdb-replica@2

# 5. Monitor replication catch-up
watch 'curl -s http://primary:8080/replication/status | jq .'
```

---

## Version-Specific Notes

### v1.0 → v1.1

**Data Compatibility:** ✅ Full
**Replication Compatibility:** ✅ Full (v1.1 replicas can sync from v1.0 primary)
**Changes:**
- Added UUID-based node IDs (backward compatible)
- Improved heartbeat tracking (backward compatible)
- Enhanced error handling (no format changes)

**Special Instructions:** None - standard rolling upgrade safe

### Future Versions

**When format changes occur**, release notes will indicate:
- `BREAKING: WAL format` - Requires snapshot + WAL truncation before upgrade
- `BREAKING: SSTable format` - Requires full compaction before upgrade
- `BREAKING: Snapshot schema` - Requires export/import via API

---

## Monitoring During Upgrade

### Key Metrics to Watch

```bash
# Replication lag (should be <5 heartbeats, <1000ms)
curl http://primary:8080/replication/status | jq '.replicas[] | {id, heartbeat_lag, lag_ms}'

# Error rate (should be 0)
curl http://primary:8080/stats | jq '.query_errors'

# Throughput (should remain stable)
curl http://primary:8080/stats | jq '{nodes, edges, queries}'

# Memory/CPU (check system metrics)
top -b -n 1 | grep graphdb
```

### Alert Thresholds

- ⚠️  **Warning**: Heartbeat lag > 10, Lag ms > 2000
- 🚨 **Critical**: Replica disconnected, Heartbeat lag > 50, Errors > 0

---

## Best Practices

### DO

✅ Test upgrade on staging environment first
✅ Upgrade during low-traffic period
✅ Have backup ready before starting
✅ Monitor replication lag continuously
✅ Keep load balancer ready to switch back
✅ Document your specific configuration
✅ Verify data integrity after each phase

### DON'T

❌ Upgrade all nodes simultaneously
❌ Skip backup step
❌ Ignore replication lag warnings
❌ Upgrade without testing
❌ Proceed if lag doesn't converge to 0
❌ Forget to verify data after promotion

---

## Troubleshooting

### Replica Won't Reconnect After Upgrade

**Symptoms:** Replica shows `connected: false`, logs show handshake errors

**Solution:**
```bash
# Check version compatibility
curl http://replica:8081/health | jq '.version'
curl http://primary:8080/health | jq '.version'

# Check network connectivity
telnet primary-host 9090

# Check logs for detailed error
journalctl -u graphdb-replica@1 -f
```

### Replication Lag Not Converging

**Symptoms:** Heartbeat lag stays high or increases

**Solution:**
```bash
# Check primary write rate
curl http://primary:8080/stats | jq '.write_rate'

# Check replica processing rate
curl http://replica:8081/stats | jq '.replay_rate'

# If lag increasing: wait longer or investigate performance issue
# If lag constant: check for replication protocol incompatibility
```

### Data Corruption Detected

**Symptoms:** CRC errors, WAL replay failures

**Solution:**
```bash
# Stop affected node
systemctl stop graphdb-replica@1

# Restore from backup
rm -rf /var/lib/graphdb/replica1
tar -xzf replica1-backup.tar.gz -C /var/lib/graphdb/

# Restart
systemctl start graphdb-replica@1
```

---

## Emergency Contacts

Keep these handy during upgrades:

- Database administrator on-call
- DevOps team lead
- Backup system access credentials
- Load balancer admin interface
- Rollback procedure quick reference

---

## Appendix: Automation Scripts

### Health Check Script

```bash
#!/bin/bash
# check-replication-health.sh

PRIMARY="http://localhost:8080"

STATUS=$(curl -s $PRIMARY/replication/status)

echo "$STATUS" | jq -r '.replicas[] |
  "Replica: \(.replica_id) | Connected: \(.connected) | HeartbeatLag: \(.heartbeat_lag) | LagMs: \(.lag_ms)"'

# Exit 0 if all healthy, 1 if any issues
UNHEALTHY=$(echo "$STATUS" | jq '[.replicas[] | select(.connected == false or .heartbeat_lag > 10)] | length')

if [ "$UNHEALTHY" -gt 0 ]; then
  echo "WARNING: $UNHEALTHY unhealthy replicas detected"
  exit 1
fi

echo "All replicas healthy"
exit 0
```

### Wait for Replication Script

```bash
#!/bin/bash
# wait-for-replication.sh

PRIMARY="http://localhost:8080"
TIMEOUT=300  # 5 minutes
INTERVAL=1   # Check every second

start_time=$(date +%s)

while true; do
  # Get max heartbeat lag across all replicas
  MAX_LAG=$(curl -s $PRIMARY/replication/status | jq '[.replicas[].heartbeat_lag] | max')

  if [ "$MAX_LAG" -eq 0 ]; then
    echo "✅ All replicas caught up (lag=0)"
    exit 0
  fi

  elapsed=$(($(date +%s) - start_time))
  if [ $elapsed -gt $TIMEOUT ]; then
    echo "❌ Timeout waiting for replication (lag=$MAX_LAG)"
    exit 1
  fi

  echo "⏳ Waiting for replication (lag=$MAX_LAG, elapsed=${elapsed}s)..."
  sleep $INTERVAL
done
```

---

## Summary

Zero-downtime upgrades are achievable with Cluso GraphDB's primary-replica architecture by:

1. **Upgrading replicas first** while primary serves traffic
2. **Promoting a replica** to primary with brief (<5s) switchover
3. **Upgrading old primary** and reconnecting as replica

**Key success factors:**
- Always backup before upgrade
- Monitor replication lag continuously
- Test in staging first
- Have rollback plan ready
- Follow compatibility guidelines

For questions or issues, consult the troubleshooting section or restore from backup.
