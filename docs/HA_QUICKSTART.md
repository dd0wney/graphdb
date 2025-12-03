# High Availability Quick Start Guide

## Overview

GraphDB now supports automated high availability with leader election, automatic failover, and split-brain prevention. This guide shows how to set up a 3-node HA cluster.

## Features

- **Automatic Failover**: Replicas detect primary failures and elect a new leader automatically
- **Split-Brain Prevention**: Epoch-based fencing ensures only one primary at a time
- **Quorum Consensus**: Elections require N/2+1 votes to prevent split decisions
- **Health Monitoring**: Heartbeat-based failure detection with configurable timeouts
- **Backward Compatible**: Cluster mode is optional and disabled by default

## Architecture

```
┌──────────────┐     Heartbeats      ┌──────────────┐
│   Primary    │◄────────────────────│  Replica 1   │
│  (Leader)    │                     │  (Follower)  │
│              │─────────────────────►│              │
└──────┬───────┘   WAL Replication   └──────────────┘
       │
       │ Heartbeats
       │
       ▼
┌──────────────┐
│  Replica 2   │
│  (Follower)  │
│              │
└──────────────┘

On Primary Failure:
┌──────────────┐     Election        ┌──────────────┐
│  Replica 1   │◄────Request─────────│  Replica 2   │
│ (Candidate)  │                     │  (Follower)  │
│              │─────Vote────────────►│              │
└──────┬───────┘                     └──────────────┘
       │
       │ Wins Election (Quorum: 2/3)
       ▼
┌──────────────┐
│  Replica 1   │
│  (PRIMARY)   │
│              │
└──────────────┘
```

## Quick Setup (3-Node Cluster)

### Node 1 Configuration

```go
package main

import (
    "github.com/dd0wney/cluso-graphdb/pkg/cluster"
    "github.com/dd0wney/cluso-graphdb/pkg/replication"
    "github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
    // Create storage
    storage, _ := storage.NewGraphStorage(storage.Config{
        DataDir: "/data/node1",
    })

    // Configure cluster
    clusterConfig := cluster.ClusterConfig{
        NodeID:             "node-1",
        NodeAddr:           "10.0.0.1:9090",
        SeedNodes:          []string{"10.0.0.1:9090", "10.0.0.2:9090", "10.0.0.3:9090"},
        ElectionTimeout:    5 * time.Second,
        HeartbeatInterval:  1 * time.Second,
        MinQuorumSize:      2,  // 2 out of 3 nodes
        Priority:           1,
        EnableAutoFailover: true,  // Enable HA
        VoteRequestTimeout: 2 * time.Second,
    }

    // Configure replication
    replConfig := replication.ReplicationConfig{
        IsPrimary:         true,  // Start as primary
        ListenAddr:        ":9090",
        HeartbeatInterval: 1 * time.Second,
        MaxReplicas:       10,
    }

    // Create replication manager
    replMgr := replication.NewReplicationManager(replConfig, storage)

    // Enable cluster mode
    if err := replMgr.EnableCluster(clusterConfig); err != nil {
        log.Fatalf("Failed to enable cluster: %v", err)
    }

    // Start replication
    if err := replMgr.Start(); err != nil {
        log.Fatalf("Failed to start replication: %v", err)
    }

    // Keep running
    select {}
}
```

### Node 2 Configuration (Replica)

```go
// Configure as replica
clusterConfig := cluster.ClusterConfig{
    NodeID:             "node-2",
    NodeAddr:           "10.0.0.2:9090",
    SeedNodes:          []string{"10.0.0.1:9090", "10.0.0.2:9090", "10.0.0.3:9090"},
    ElectionTimeout:    5 * time.Second,
    HeartbeatInterval:  1 * time.Second,
    MinQuorumSize:      2,
    Priority:           1,
    EnableAutoFailover: true,
    VoteRequestTimeout: 2 * time.Second,
}

replConfig := replication.ReplicationConfig{
    PrimaryAddr:       "10.0.0.1:9090",  // Connect to node-1
    HeartbeatInterval: 1 * time.Second,
}

replica := replication.NewReplicaNode(replConfig, storage)
replica.EnableCluster(clusterConfig)
replica.Start()
```

### Node 3 Configuration (Replica)

Same as Node 2, but with:
```go
NodeID:   "node-3"
NodeAddr: "10.0.0.3:9090"
```

## Configuration Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `ElectionTimeout` | 5s | Time to wait before starting election |
| `HeartbeatInterval` | 1s | How often to send heartbeats |
| `MinQuorumSize` | 2 | Minimum votes needed to win election |
| `EnableAutoFailover` | false | **Must be true** to enable HA |
| `VoteRequestTimeout` | 2s | Timeout for vote requests |
| `Priority` | 1 | Election priority (higher = preferred) |

## Failure Scenarios

### Primary Failure

1. **Detection**: Replicas detect missing heartbeats after 3x heartbeat interval (3 seconds)
2. **Election**: Replica with highest LSN starts election
3. **Voting**: Nodes vote for candidate with highest LSN and term
4. **Promotion**: Candidate with quorum (2/3) becomes new primary
5. **Fencing**: Old primary (if it comes back) sees higher epoch and steps down

**Timeline:**
```
T=0s:  Primary crashes
T=3s:  Replicas detect timeout
T=3s:  Replica-1 starts election (term=2)
T=4s:  Replica-1 collects votes (2/3 quorum)
T=4s:  Replica-1 becomes new primary
T=5s:  Old primary comes back, sees epoch=2 > epoch=1, steps down
```

### Network Partition

**Scenario**: Network split isolates primary from replicas

```
Before:                    After Partition:
┌─────────┐               ┌─────────┐  (isolated)
│ Primary │               │ Primary │
│ Epoch=1 │               │ Epoch=1 │
└────┬────┘               └─────────┘
     │
     │                    ┌─────────┐
┌────┴────┐              │Replica-1│
│Replica-1│              │ Epoch=2 │ ← New Primary (won election)
└─────────┘              └─────────┘
┌─────────┐              ┌─────────┐
│Replica-2│              │Replica-2│
└─────────┘              └─────────┘
```

**Protection**: When old primary rejoins, it sees Epoch=2 > Epoch=1 and automatically steps down.

### Split Brain Prevention

Epoch fencing ensures no dual-primary:

```go
// In primary.go handleReplicaConnection()
if handshake.Epoch > rm.membership.GetEpoch() {
    log.Printf("⚠️  FENCING: Replica has higher epoch - stepping down")
    // Reject connection and step down
    go rm.onBecomeFollower()
    return
}
```

## Manual Promotion

When cluster mode is enabled, manual promotions trigger elections:

```bash
curl -X POST http://replica-1:8080/admin/upgrade/promote \
  -H "Content-Type: application/json" \
  -d '{"wait_for_sync": true, "timeout": 60}'
```

**Process:**
1. API triggers election
2. Replica votes for itself
3. Waits for quorum (2/3 votes)
4. Becomes primary if election succeeds
5. Returns success/failure

## Monitoring

### Check Cluster Status

```bash
# On any node
curl http://localhost:8080/admin/upgrade/status
```

**Response:**
```json
{
  "phase": "replica_running",
  "ready": true,
  "current_role": "replica",
  "connected_replicas": 2,
  "can_promote": true
}
```

### Election Manager State

```go
// In your application
state := electionMgr.GetState()  // StateFollower, StateCandidate, StateLeader
term := electionMgr.GetCurrentTerm()
isLeader := electionMgr.IsLeader()
```

### Membership Info

```go
allNodes := membership.GetAllNodes()
healthyNodes := membership.GetHealthyNodes(5 * time.Second)
hasQuorum := membership.HasQuorum(2, 5 * time.Second)
```

## Troubleshooting

### Elections Keep Failing

**Symptom**: Replicas keep starting elections but no leader emerges

**Causes:**
- Network issues preventing vote messages
- Quorum size too high for cluster size
- Clock skew causing heartbeat issues

**Fix:**
```go
// Increase timeouts
ElectionTimeout: 10 * time.Second  // Was 5s
VoteRequestTimeout: 5 * time.Second  // Was 2s

// Lower quorum (for testing only)
MinQuorumSize: 2  // For 3-node cluster
```

### Split Brain Detected

**Symptom**: Multiple primaries in logs

**Check:**
```bash
grep "FENCING" /var/log/graphdb.log
```

**Expected**: Old primary sees higher epoch and steps down
```
⚠️  FENCING: Replica has higher epoch 3 > 2 - stepping down
```

### Replica Not Catching Up

**Symptom**: Replica LSN far behind primary

**Check:**
```go
replicaStatus := replica.GetReplicaStatus()
log.Printf("Lag: %d LSN behind",
    primaryLSN - replicaStatus.LastAppliedLSN)
```

**Possible Issues:**
- Network bandwidth
- Disk I/O on replica
- WAL apply bottleneck

## Best Practices

1. **Odd Number of Nodes**: Use 3, 5, or 7 nodes for quorum
2. **Geographic Distribution**: Place nodes in different availability zones
3. **Monitor Health**: Set up alerts for `connected_replicas < MinQuorumSize`
4. **Test Failover**: Regularly test failover by stopping primary
5. **Backup Before Promotion**: Always backup before manual promotion
6. **Gradual Rollout**: Enable `EnableAutoFailover: false` initially, test, then enable

## Production Checklist

- [ ] At least 3 nodes deployed
- [ ] `EnableAutoFailover: true` set
- [ ] Seed nodes configured on all nodes
- [ ] Network connectivity verified between all nodes
- [ ] Firewall allows port 9090 (or your configured port)
- [ ] Monitoring configured for cluster health
- [ ] Tested failover scenario (stop primary, verify election)
- [ ] Tested network partition recovery
- [ ] Backup and restore procedures documented
- [ ] Runbooks created for manual intervention

## Example 3-Node Setup (Docker Compose)

```yaml
version: '3.8'

services:
  node1:
    image: graphdb:latest
    environment:
      - NODE_ID=node-1
      - NODE_ADDR=node1:9090
      - SEED_NODES=node1:9090,node2:9090,node3:9090
      - IS_PRIMARY=true
      - ENABLE_AUTO_FAILOVER=true
    ports:
      - "8081:8080"
      - "9091:9090"

  node2:
    image: graphdb:latest
    environment:
      - NODE_ID=node-2
      - NODE_ADDR=node2:9090
      - SEED_NODES=node1:9090,node2:9090,node3:9090
      - PRIMARY_ADDR=node1:9090
      - ENABLE_AUTO_FAILOVER=true
    ports:
      - "8082:8080"
      - "9092:9090"

  node3:
    image: graphdb:latest
    environment:
      - NODE_ID=node-3
      - NODE_ADDR=node3:9090
      - SEED_NODES=node1:9090,node2:9090,node3:9090
      - PRIMARY_ADDR=node1:9090
      - ENABLE_AUTO_FAILOVER=true
    ports:
      - "8083:8080"
      - "9093:9090"
```

## Next Steps

- Review the [Architecture Design](./HA_ARCHITECTURE.md) for implementation details
- See [Testing Guide](./HA_TESTING.md) for failover testing procedures
- Check [Operations Guide](./HA_OPERATIONS.md) for production operations

## Support

For issues or questions about High Availability:
- Check logs for `Election manager`, `FENCING`, or `cluster` messages
- Review cluster status via admin API
- Ensure all nodes can communicate on configured ports
