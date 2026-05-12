# Milestone 3: Migration Guide

## Overview

This guide covers migrating from Milestone 2 (single-node, disk-backed edges) to Milestone 3 (distributed, Raft-based cluster).

**Migration Types Covered**:
1. **Fresh Deployment** - New distributed cluster from scratch
2. **Standard Migration** - Small downtime (15 minutes)
3. **Zero-Downtime Migration** - Production system (no downtime)

---

## Migration Type 1: Fresh Deployment

**Use Case**: New installation, no existing data

**Downtime**: None (new deployment)

**Steps** (30 minutes):

### 1. Provision Cluster Nodes

**Hardware Requirements** (per node):
- CPU: 8+ cores
- RAM: 32 GB
- Disk: 100 GB SSD
- Network: 1 Gbps to other nodes

**Example (3-node cluster)**:
```
node1.example.com: 10.0.1.10
node2.example.com: 10.0.1.11
node3.example.com: 10.0.1.12
```

### 2. Install GraphDB Milestone 3

**On each node**:
```bash
# Download binary
wget https://github.com/yourusername/graphdb/releases/download/v0.3.0/graphdb-linux-amd64

# Install
sudo mv graphdb-linux-amd64 /usr/local/bin/graphdb
sudo chmod +x /usr/local/bin/graphdb

# Create directories
sudo mkdir -p /var/lib/graphdb/data
sudo mkdir -p /var/lib/graphdb/raft
sudo mkdir -p /etc/graphdb
```

### 3. Configure Each Node

**File**: `/etc/graphdb/config.yaml`

**Node 1**:
```yaml
node_id: node1
bind_addr: 10.0.1.10

raft:
  raft_addr: 10.0.1.10:7000
  data_dir: /var/lib/graphdb/raft
  bootstrap: true  # Only true on first node
  peers:
    - node1: 10.0.1.10:7000
    - node2: 10.0.1.11:7000
    - node3: 10.0.1.12:7000

grpc:
  listen_addr: 0.0.0.0:9000
  tls:
    enabled: true
    cert_file: /etc/graphdb/server.crt
    key_file: /etc/graphdb/server.key

storage:
  data_dir: /var/lib/graphdb/data
  disk_backed_edges: true
  cache_size: 100000

sharding:
  num_shards: 9
  cluster_nodes:
    - 10.0.1.10:9000
    - 10.0.1.11:9000
    - 10.0.1.12:9000
```

**Node 2** (same but `bootstrap: false`)

**Node 3** (same but `bootstrap: false`)

### 4. Start Cluster

**On node1 (leader)**:
```bash
sudo systemctl start graphdb
sudo systemctl enable graphdb

# Check logs
sudo journalctl -u graphdb -f
```

**Wait for node1 to become leader** (~3 seconds)

**On node2 and node3**:
```bash
sudo systemctl start graphdb
sudo systemctl enable graphdb
```

### 5. Verify Cluster Health

```bash
# Check cluster status
graphdb-cli cluster status

# Expected output:
# Node          Role      Status    Shards
# node1         Leader    Healthy   0,3,6
# node2         Follower  Healthy   1,4,7
# node3         Follower  Healthy   2,5,8
```

**Done!** Cluster is ready for use.

---

## Migration Type 2: Standard Migration (Small Downtime)

**Use Case**: Existing Milestone 2 deployment, acceptable downtime

**Downtime**: ~15 minutes

**Data Loss**: None (if migration succeeds)

**Rollback**: Snapshot available

**Steps** (4 hours total, 15 min downtime):

### 1. Preparation (1 hour, system running)

**On existing Milestone 2 node**:

```bash
# 1. Upgrade to latest Milestone 2 patch
graphdb --version  # Should be v0.2.x

# 2. Create full backup
graphdb-cli snapshot create /backup/graphdb-snapshot-$(date +%Y%m%d).json

# 3. Verify backup integrity
graphdb-cli snapshot verify /backup/graphdb-snapshot-*.json

# 4. Document current stats
graphdb-cli stats
# Record: node count, edge count, memory usage, disk usage

# 5. Notify users of upcoming maintenance window
```

### 2. Provision Distributed Cluster (2 hours, system running)

**Set up 3 new nodes** (as in Fresh Deployment above)

**Do NOT start services yet**

### 3. Initial Snapshot Migration (30 min, system running)

**On Milestone 2 node**:
```bash
# Create fresh snapshot
graphdb-cli snapshot create /tmp/migration-snapshot.json

# Copy snapshot to node1 of new cluster
scp /tmp/migration-snapshot.json node1.example.com:/var/lib/graphdb/
```

**On new cluster node1**:
```bash
# Import snapshot (cluster not started yet)
graphdb restore --snapshot /var/lib/graphdb/migration-snapshot.json --data-dir /var/lib/graphdb/data
```

### 4. Downtime Window Begins (15 minutes)

**Mark as beginning of downtime**

**On Milestone 2 node**:
```bash
# 1. Stop accepting writes
graphdb-cli readonly enable

# 2. Wait for in-flight operations to complete
sleep 5

# 3. Create final incremental snapshot (only changes since initial snapshot)
graphdb-cli snapshot create --incremental /tmp/final-delta.json

# 4. Stop Milestone 2 service
sudo systemctl stop graphdb-milestone2
```

**On new cluster node1**:
```bash
# Apply incremental changes
graphdb restore --snapshot /tmp/final-delta.json --data-dir /var/lib/graphdb/data --incremental
```

### 5. Start Distributed Cluster (3 minutes)

```bash
# Start all nodes
ssh node1.example.com "sudo systemctl start graphdb"
ssh node2.example.com "sudo systemctl start graphdb"
ssh node3.example.com "sudo systemctl start graphdb"

# Wait for leader election
sleep 5

# Verify cluster health
graphdb-cli cluster status
```

### 6. Validation (5 minutes)

```bash
# 1. Verify node/edge counts match
graphdb-cli stats
# Compare to pre-migration numbers

# 2. Test sample queries
graphdb-cli query "MATCH (n:Person {name: 'Alice'}) RETURN n"

# 3. Test write operations
graphdb-cli exec "CREATE (n:TestNode {migrated: true})"

# 4. Verify data on all shards
graphdb-cli shard validate
```

### 7. Cutover (2 minutes)

```bash
# 1. Update DNS/load balancer to point to new cluster
# 2. Update application config to use new endpoints
# 3. Enable writes on new cluster (if read-only mode enabled)
```

**Downtime window ends** (~15 minutes total)

### 8. Monitor (24 hours)

```bash
# Watch for errors
graphdb-cli logs --follow --level=error

# Monitor performance
graphdb-cli metrics --watch
```

### 9. Decommission Old Node (after 7 days)

```bash
# Only after confirming new cluster is stable
sudo systemctl stop graphdb-milestone2
sudo systemctl disable graphdb-milestone2

# Keep backup for 30 days before deletion
```

---

## Migration Type 3: Zero-Downtime Migration

**Use Case**: Production system, cannot tolerate downtime

**Downtime**: 0 minutes

**Complexity**: High (dual-write pattern)

**Duration**: 2-3 days (gradual rollout)

**Steps**:

### Phase 1: Setup (Day 1 Morning)

1. **Provision distributed cluster** (as in Migration Type 2)
2. **Import initial snapshot** to new cluster
3. **Start new cluster** (in shadow mode)

### Phase 2: Dual-Write (Day 1 Afternoon - Day 2)

**Architecture**:
```
Application
    ↓
Write Proxy
    ├─→ Milestone 2 (primary)
    └─→ Milestone 3 (shadow, async)
```

**Implementation**:

**File**: `cmd/write-proxy/main.go`

```go
type WriteProxy struct {
    primary   *graphdb.Client  // Milestone 2
    shadow    *graphdb.Client  // Milestone 3
}

func (p *WriteProxy) CreateNode(label string, props map[string]interface{}) (*Node, error) {
    // Write to primary (blocking)
    node, err := p.primary.CreateNode(label, props)
    if err != nil {
        return nil, err
    }

    // Write to shadow (async, non-blocking)
    go func() {
        _, err := p.shadow.CreateNode(label, props)
        if err != nil {
            log.Printf("shadow write failed: %v", err)
            metrics.ShadowWriteErrors.Inc()
        }
    }()

    return node, nil
}
```

**Deploy write proxy**:
```bash
# Deploy proxy in front of Milestone 2
# All application writes go through proxy
# Proxy writes to both old and new cluster
```

**Validation**:
- Monitor shadow write error rate (should be <0.1%)
- Both systems should have same node/edge counts (within small delta)

### Phase 3: Backfill Historical Data (Day 2)

**On Milestone 2 node**:
```bash
# Stream all historical data to new cluster
graphdb-cli export --format=jsonl | \
  ssh node1.example.com "graphdb-cli import --format=jsonl"
```

**Duration**: ~6 hours for 100M nodes

**Validation**:
```bash
# Verify counts match
graphdb-cli stats --cluster=old
graphdb-cli stats --cluster=new

# Spot-check random samples
graphdb-cli diff --old=milestone2 --new=milestone3 --sample=10000
```

### Phase 4: Consistency Check (Day 2 Evening)

**Run consistency validator**:
```bash
graphdb-cli validate-consistency \
  --source=milestone2:9000 \
  --target=milestone3:9000 \
  --sample-rate=0.01  # Check 1% of data
```

**Expected**: >99.99% consistency

**If inconsistencies found**: Investigate and re-sync affected shards

### Phase 5: Read Cutover (Day 3 Morning)

**Gradually shift reads to new cluster**:

```
Hour 0: 10% reads to new cluster
Hour 1: 25% reads to new cluster
Hour 2: 50% reads to new cluster
Hour 3: 75% reads to new cluster
Hour 4: 100% reads to new cluster
```

**Implementation** (load balancer):
```nginx
upstream graphdb_reads {
    server milestone2:9000 weight=0;    # 0% of reads
    server milestone3:9000 weight=100;  # 100% of reads
}
```

**Monitor**:
- Error rates (should stay constant)
- Latency P99 (should improve with distributed)
- Cache hit rates

### Phase 6: Write Cutover (Day 3 Afternoon)

**After reads stable on new cluster**:

1. **Flip write proxy**:
   ```go
   // Now Milestone 3 is primary
   primary   = mileston3Client
   shadow    = milestone2Client  // For rollback safety
   ```

2. **Monitor write performance** (1 hour)

3. **If stable, disable shadow writes**:
   ```go
   // Stop writing to Milestone 2
   shadow = nil
   ```

### Phase 7: Decommission (Day 3+ 7 days)

**After 1 week of stable operation**:
- Stop Milestone 2 service
- Archive final snapshot
- Decommission old hardware

**Total Downtime**: 0 minutes

---

## Rollback Procedures

### Rollback from Migration Type 2 (Standard)

**If migration fails during downtime window**:

1. **Stop new cluster**:
   ```bash
   ssh node1.example.com "sudo systemctl stop graphdb"
   ssh node2.example.com "sudo systemctl stop graphdb"
   ssh node3.example.com "sudo systemctl stop graphdb"
   ```

2. **Restart Milestone 2**:
   ```bash
   sudo systemctl start graphdb-milestone2
   ```

3. **Verify data integrity**:
   ```bash
   graphdb-cli stats
   graphdb-cli query "MATCH (n) RETURN count(n)"
   ```

4. **Re-enable writes**:
   ```bash
   graphdb-cli readonly disable
   ```

**Total rollback time**: 5 minutes

### Rollback from Migration Type 3 (Zero-Downtime)

**If issues detected during dual-write**:

1. **Stop shadow writes** in proxy
2. **Continue with Milestone 2** only
3. **Investigate root cause**
4. **Retry migration later**

**No impact to production**

**If issues after read cutover**:

1. **Shift reads back to Milestone 2** via load balancer
2. **Investigate performance issue**
3. **Fix and retry**

**Rollback time**: <1 minute (load balancer config change)

---

## Validation Checklist

### Pre-Migration Validation

- [ ] Milestone 2 node is healthy (no errors in logs)
- [ ] Full backup created and verified
- [ ] New cluster nodes provisioned and configured
- [ ] Network connectivity between cluster nodes verified
- [ ] Firewall rules allow Raft (7000) and gRPC (9000) ports
- [ ] TLS certificates installed on all nodes
- [ ] Rollback procedure documented and tested

### Post-Migration Validation

- [ ] Cluster status shows all nodes healthy
- [ ] Node count matches pre-migration count
- [ ] Edge count matches pre-migration count
- [ ] Sample queries return correct results
- [ ] Write operations succeed on all shards
- [ ] Read latency meets SLA (P99 < 20ms)
- [ ] Write latency meets SLA (P99 < 50ms)
- [ ] No errors in logs for 1 hour
- [ ] Prometheus metrics look normal
- [ ] Raft leader is stable (no re-elections)

---

## Troubleshooting

### Issue: Leader election fails

**Symptoms**: No leader elected after 10 seconds

**Diagnosis**:
```bash
# Check Raft logs
sudo journalctl -u graphdb | grep -i raft

# Check network connectivity
ping node2.example.com
telnet node2.example.com 7000
```

**Solutions**:
- Verify firewall allows port 7000
- Check Raft peer configuration in `config.yaml`
- Ensure only ONE node has `bootstrap: true`

### Issue: Data inconsistency after migration

**Symptoms**: Different node counts on different shards

**Diagnosis**:
```bash
graphdb-cli shard validate
```

**Solutions**:
- Re-run migration with `--force-consistency` flag
- Manually reconcile affected shards
- Check for dropped writes during migration

### Issue: High write latency (P99 >100ms)

**Symptoms**: Writes slow after migration

**Diagnosis**:
```bash
# Check Raft log size
graphdb-cli raft stats

# Check disk I/O
iostat -x 1
```

**Solutions**:
- Trigger manual snapshot to compact Raft log
- Ensure SSDs are used (not HDDs)
- Tune Raft batch size: `raft.max_append_entries = 100`

### Issue: Cross-shard queries fail

**Symptoms**: Queries that span shards return errors

**Diagnosis**:
```bash
# Check gRPC connectivity between nodes
graphdb-cli cluster connectivity

# Check shard map consistency
graphdb-cli shard map validate
```

**Solutions**:
- Verify all nodes can reach each other on port 9000
- Restart cluster to reload shard map
- Check for network partitions

---

## Best Practices

### Before Migration

1. **Test on staging first** - Never migrate production first
2. **Create multiple backups** - Keep at least 3 backup copies
3. **Schedule during low-traffic period** - Minimize user impact
4. **Communicate with stakeholders** - Set expectations

### During Migration

1. **Monitor constantly** - Watch logs, metrics, alerts
2. **Have rollback plan ready** - Be prepared to abort
3. **Keep stakeholders updated** - Provide status updates every 30 min
4. **Document issues** - Track any anomalies for post-mortem

### After Migration

1. **Monitor for 7 days** - Watch for delayed issues
2. **Keep old system available** - Don't decommission immediately
3. **Run load tests** - Validate performance under peak load
4. **Update documentation** - Reflect new architecture
5. **Conduct post-mortem** - Learn from migration experience

---

## Migration Timeline Summary

| Migration Type | Prep Time | Downtime | Validation | Total |
|----------------|-----------|----------|------------|-------|
| Fresh Deployment | 30 min | 0 | 15 min | 45 min |
| Standard Migration | 3.5 hours | 15 min | 30 min | 4 hours |
| Zero-Downtime | 2 days | 0 | 4 hours | 2-3 days |

---

## Support & Resources

- **Migration Support**: support@example.com
- **Documentation**: https://docs.example.com/migration
- **Community Forum**: https://forum.example.com/migration
- **Emergency Hotline**: +1-555-GRAPHDB (during migration window)

---

**Document Version**: 1.0
**Last Updated**: 2025-11-16
**Status**: Planning Phase
**Tested On**: Staging environment (simulated 50M nodes)
