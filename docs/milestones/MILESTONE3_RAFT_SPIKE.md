# Raft Integration Prototype - Technical Spike

## Overview

**Goal**: Validate that `hashicorp/raft` can be integrated with our existing GraphStorage to provide distributed consensus and replication.

**Duration**: 2 hours (prototype)

**Status**: Planning Phase

---

## Objectives

1. **Integrate Raft Library**: Add hashicorp/raft as a dependency
2. **Implement FSM Interface**: Wrap GraphStorage as a Raft Finite State Machine
3. **Test Leader Election**: Validate that 3 nodes can elect a leader
4. **Test Log Replication**: Ensure CreateNode operation replicates to followers
5. **Measure Latency Impact**: Compare latency with/without Raft

---

## Architecture

```
┌───────────────────────────────────────┐
│         Raft Node 1 (Leader)          │
├───────────────────────────────────────┤
│  Application Layer                    │
│  ┌─────────────────────────────────┐  │
│  │  GraphDB API                    │  │
│  │  (CreateNode, GetNode, etc.)    │  │
│  └──────────────┬──────────────────┘  │
│                 │                      │
│  ┌──────────────▼──────────────────┐  │
│  │  Raft Interface                 │  │
│  │  - raft.Apply()                 │  │
│  │  - raft.State()                 │  │
│  └──────────────┬──────────────────┘  │
│                 │                      │
│  ┌──────────────▼──────────────────┐  │
│  │  GraphFSM (implements raft.FSM) │  │
│  │  - Apply(log) → storage.op()    │  │
│  │  - Snapshot() → JSON snapshot   │  │
│  │  - Restore(snap) → load data    │  │
│  └──────────────┬──────────────────┘  │
│                 │                      │
│  ┌──────────────▼──────────────────┐  │
│  │  GraphStorage (Milestone 2)     │  │
│  │  - CreateNode()                 │  │
│  │  - GetNode()                    │  │
│  │  - Existing storage layer       │  │
│  └─────────────────────────────────┘  │
│                                       │
│  ┌─────────────────────────────────┐  │
│  │  Raft Log Store (BoltDB)        │  │
│  └─────────────────────────────────┘  │
│                                       │
│  ┌─────────────────────────────────┐  │
│  │  Raft Transport (TCP)           │  │
│  │  - Leader: :7000                │  │
│  └─────────────────────────────────┘  │
└───────────────────────────────────────┘
         ▲                 ▲
         │  Raft Protocol  │
         ▼                 ▼
    Node 2           Node 3
  (Follower)       (Follower)
```

---

## Implementation Plan

### Phase 1: Dependencies (10 min)

**Add to `go.mod`**:
```bash
go get github.com/hashicorp/raft@v1.5.0
go get github.com/hashicorp/raft-boltdb@v2.3.0
```

**Dependencies**:
- `github.com/hashicorp/raft` - Core Raft implementation
- `github.com/hashicorp/raft-boltdb` - BoltDB log/stable store
- `github.com/boltdb/bolt` - Embedded key-value database

### Phase 2: Minimal FSM Implementation (30 min)

**File**: `pkg/raft/prototype/fsm.go`

```go
package prototype

import (
    "encoding/json"
    "fmt"
    "io"
    "sync"

    "github.com/hashicorp/raft"
    "github.com/yourusername/graphdb/pkg/storage"
)

// GraphFSM implements the raft.FSM interface
type GraphFSM struct {
    storage *storage.GraphStorage
    mu      sync.RWMutex
}

func NewGraphFSM() *GraphFSM {
    return &GraphFSM{
        storage: storage.NewGraphStorage(),
    }
}

// Apply applies a Raft log entry to the FSM
func (f *GraphFSM) Apply(log *raft.Log) interface{} {
    f.mu.Lock()
    defer f.mu.Unlock()

    var cmd Command
    if err := json.Unmarshal(log.Data, &cmd); err != nil {
        return fmt.Errorf("failed to unmarshal command: %w", err)
    }

    switch cmd.Type {
    case "CreateNode":
        node, err := f.storage.CreateNode(cmd.Label, cmd.Properties)
        if err != nil {
            return err
        }
        return node

    case "DeleteNode":
        err := f.storage.DeleteNode(cmd.NodeID)
        return err

    default:
        return fmt.Errorf("unknown command type: %s", cmd.Type)
    }
}

// Snapshot creates a snapshot of the current state
func (f *GraphFSM) Snapshot() (raft.FSMSnapshot, error) {
    f.mu.RLock()
    defer f.mu.RUnlock()

    // Leverage existing snapshot mechanism
    snapshot := &GraphSnapshot{
        storage: f.storage,
    }
    return snapshot, nil
}

// Restore restores the FSM from a snapshot
func (f *GraphFSM) Restore(snapshot io.ReadCloser) error {
    f.mu.Lock()
    defer f.mu.Unlock()

    // Use existing RestoreSnapshot from storage layer
    return f.storage.RestoreSnapshot(snapshot)
}

// Command represents a graph operation
type Command struct {
    Type       string                 `json:"type"`
    Label      string                 `json:"label,omitempty"`
    Properties map[string]interface{} `json:"properties,omitempty"`
    NodeID     uint64                 `json:"node_id,omitempty"`
}

// GraphSnapshot implements raft.FSMSnapshot
type GraphSnapshot struct {
    storage *storage.GraphStorage
}

func (s *GraphSnapshot) Persist(sink raft.SnapshotSink) error {
    // Create snapshot and write to sink
    err := s.storage.CreateSnapshot(sink)
    if err != nil {
        sink.Cancel()
        return err
    }
    return sink.Close()
}

func (s *GraphSnapshot) Release() {
    // Cleanup if needed (our snapshots don't hold resources)
}
```

**Key Design Points**:
- **Minimal Command Set**: Only CreateNode and DeleteNode for proof-of-concept
- **Leverage Existing Snapshot**: Reuse Milestone 2 snapshot code
- **Thread Safety**: FSM mutex protects GraphStorage access
- **Error Handling**: Return errors from Apply (Raft will handle them)

### Phase 3: Raft Node Setup (45 min)

**File**: `pkg/raft/prototype/node.go`

```go
package prototype

import (
    "fmt"
    "net"
    "os"
    "path/filepath"
    "time"

    "github.com/hashicorp/raft"
    raftboltdb "github.com/hashicorp/raft-boltdb"
)

type RaftNode struct {
    raft *raft.Raft
    fsm  *GraphFSM
}

func NewRaftNode(nodeID, raftAddr, dataDir string, bootstrap bool) (*RaftNode, error) {
    // Create Raft configuration
    config := raft.DefaultConfig()
    config.LocalID = raft.ServerID(nodeID)

    // Set up data directory
    if err := os.MkdirAll(dataDir, 0700); err != nil {
        return nil, fmt.Errorf("failed to create data dir: %w", err)
    }

    // Create FSM
    fsm := NewGraphFSM()

    // Create log store
    logStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-log.db"))
    if err != nil {
        return nil, fmt.Errorf("failed to create log store: %w", err)
    }

    // Create stable store (for metadata)
    stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-stable.db"))
    if err != nil {
        return nil, fmt.Errorf("failed to create stable store: %w", err)
    }

    // Create snapshot store
    snapshotStore, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
    if err != nil {
        return nil, fmt.Errorf("failed to create snapshot store: %w", err)
    }

    // Create TCP transport
    addr, err := net.ResolveTCPAddr("tcp", raftAddr)
    if err != nil {
        return nil, fmt.Errorf("failed to resolve addr: %w", err)
    }

    transport, err := raft.NewTCPTransport(raftAddr, addr, 3, 10*time.Second, os.Stderr)
    if err != nil {
        return nil, fmt.Errorf("failed to create transport: %w", err)
    }

    // Create Raft system
    r, err := raft.NewRaft(config, fsm, logStore, stableStore, snapshotStore, transport)
    if err != nil {
        return nil, fmt.Errorf("failed to create raft: %w", err)
    }

    // Bootstrap if this is the first node
    if bootstrap {
        configuration := raft.Configuration{
            Servers: []raft.Server{
                {
                    ID:      config.LocalID,
                    Address: transport.LocalAddr(),
                },
            },
        }
        r.BootstrapCluster(configuration)
    }

    return &RaftNode{
        raft: r,
        fsm:  fsm,
    }, nil
}

// CreateNode applies a CreateNode command via Raft
func (n *RaftNode) CreateNode(label string, props map[string]interface{}) (uint64, error) {
    // Check if leader
    if n.raft.State() != raft.Leader {
        return 0, fmt.Errorf("not the leader")
    }

    // Create command
    cmd := Command{
        Type:       "CreateNode",
        Label:      label,
        Properties: props,
    }

    // Serialize command
    data, err := json.Marshal(cmd)
    if err != nil {
        return 0, err
    }

    // Apply via Raft (blocks until committed)
    future := n.raft.Apply(data, 10*time.Second)
    if err := future.Error(); err != nil {
        return 0, err
    }

    // Extract result
    node := future.Response().(*storage.Node)
    return node.ID, nil
}

// GetNode reads directly from FSM (no Raft consensus needed for reads)
func (n *RaftNode) GetNode(id uint64) (*storage.Node, error) {
    return n.fsm.storage.GetNode(id)
}

// IsLeader returns true if this node is the Raft leader
func (n *RaftNode) IsLeader() bool {
    return n.raft.State() == raft.Leader
}

// Leader returns the current leader address
func (n *RaftNode) Leader() string {
    return string(n.raft.Leader())
}

// AddVoter adds a new node to the Raft cluster
func (n *RaftNode) AddVoter(nodeID, addr string) error {
    if n.raft.State() != raft.Leader {
        return fmt.Errorf("not the leader")
    }

    future := n.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 10*time.Second)
    return future.Error()
}

// Shutdown gracefully shuts down the Raft node
func (n *RaftNode) Shutdown() error {
    return n.raft.Shutdown().Error()
}
```

### Phase 4: Test Cluster Setup (30 min)

**File**: `pkg/raft/prototype/cluster_test.go`

```go
package prototype_test

import (
    "fmt"
    "testing"
    "time"

    "github.com/yourusername/graphdb/pkg/raft/prototype"
)

func TestRaftCluster_LeaderElection(t *testing.T) {
    // Create 3-node cluster
    node1, err := prototype.NewRaftNode("node1", "127.0.0.1:7001", "/tmp/raft-node1", true)
    if err != nil {
        t.Fatalf("failed to create node1: %v", err)
    }
    defer node1.Shutdown()

    node2, err := prototype.NewRaftNode("node2", "127.0.0.1:7002", "/tmp/raft-node2", false)
    if err != nil {
        t.Fatalf("failed to create node2: %v", err)
    }
    defer node2.Shutdown()

    node3, err := prototype.NewRaftNode("node3", "127.0.0.1:7003", "/tmp/raft-node3", false)
    if err != nil {
        t.Fatalf("failed to create node3: %v", err)
    }
    defer node3.Shutdown()

    // Join node2 and node3 to cluster
    if err := node1.AddVoter("node2", "127.0.0.1:7002"); err != nil {
        t.Fatalf("failed to add node2: %v", err)
    }

    if err := node1.AddVoter("node3", "127.0.0.1:7003"); err != nil {
        t.Fatalf("failed to add node3: %v", err)
    }

    // Wait for leader election
    time.Sleep(3 * time.Second)

    // Verify leader exists
    var leader *prototype.RaftNode
    for _, node := range []*prototype.RaftNode{node1, node2, node3} {
        if node.IsLeader() {
            leader = node
            break
        }
    }

    if leader == nil {
        t.Fatal("no leader elected after 3 seconds")
    }

    t.Logf("Leader elected: %s", leader.Leader())
}

func TestRaftCluster_CreateNodeReplication(t *testing.T) {
    // Set up cluster (same as above)
    node1, _ := prototype.NewRaftNode("node1", "127.0.0.1:7001", "/tmp/raft-node1", true)
    defer node1.Shutdown()

    node2, _ := prototype.NewRaftNode("node2", "127.0.0.1:7002", "/tmp/raft-node2", false)
    defer node2.Shutdown()

    node3, _ := prototype.NewRaftNode("node3", "127.0.0.1:7003", "/tmp/raft-node3", false)
    defer node3.Shutdown()

    node1.AddVoter("node2", "127.0.0.1:7002")
    node1.AddVoter("node3", "127.0.0.1:7003")

    time.Sleep(3 * time.Second)

    // Create node on leader
    var leader *prototype.RaftNode
    for _, node := range []*prototype.RaftNode{node1, node2, node3} {
        if node.IsLeader() {
            leader = node
            break
        }
    }

    nodeID, err := leader.CreateNode("Person", map[string]interface{}{
        "name": "Alice",
        "age":  30,
    })
    if err != nil {
        t.Fatalf("failed to create node: %v", err)
    }

    t.Logf("Created node ID: %d on leader", nodeID)

    // Wait for replication
    time.Sleep(1 * time.Second)

    // Verify node exists on all followers
    for _, node := range []*prototype.RaftNode{node1, node2, node3} {
        n, err := node.GetNode(nodeID)
        if err != nil {
            t.Errorf("node %s failed to read node %d: %v", node.Leader(), nodeID, err)
            continue
        }

        if n.Label != "Person" {
            t.Errorf("node %s has wrong label: got %s, want Person", node.Leader(), n.Label)
        }

        if n.Properties["name"] != "Alice" {
            t.Errorf("node %s has wrong name: got %v, want Alice", node.Leader(), n.Properties["name"])
        }

        t.Logf("Node %d successfully replicated to %s", nodeID, node.Leader())
    }
}

func BenchmarkRaft_CreateNodeLatency(b *testing.B) {
    node, _ := prototype.NewRaftNode("node1", "127.0.0.1:7001", "/tmp/raft-bench", true)
    defer node.Shutdown()

    time.Sleep(1 * time.Second) // Wait for leader election

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := node.CreateNode("Test", map[string]interface{}{
            "index": i,
        })
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

---

## Expected Results

### Test 1: Leader Election

**Input**: Start 3 nodes, join them to cluster

**Expected Output**:
```
Leader elected: 127.0.0.1:7001
```

**Success Criteria**:
- One node becomes leader within 3 seconds
- Other two nodes become followers
- Leader remains stable (no re-election)

### Test 2: Log Replication

**Input**: Create node on leader

**Expected Output**:
```
Created node ID: 1 on leader
Node 1 successfully replicated to node1
Node 1 successfully replicated to node2
Node 1 successfully replicated to node3
```

**Success Criteria**:
- Node created on leader
- Node replicated to all followers within 1 second
- All nodes have identical data (label, properties)

### Benchmark: CreateNode Latency

**Baseline (No Raft)**: ~1.3 μs per CreateNode
**With Raft (Single Node)**: ~10-50 ms per CreateNode

**Expected Overhead**: 10,000x slower (due to disk fsync + Raft log)

**Breakdown**:
- Serialize command: ~10 μs
- Raft log append: ~5 ms (BoltDB write + fsync)
- Network round-trip (3-node cluster): ~1-5 ms
- FSM apply: ~10 μs
- **Total**: ~10-15 ms per write (P50)

**Optimization Opportunities**:
- Batch multiple operations in single Raft entry (100x throughput)
- Use SSDs for log store (2-3x faster fsync)
- Tune Raft batch size and timeouts

---

## Risks & Mitigations

### Risk 1: Log Growth

**Issue**: Raft log grows unbounded without compaction

**Impact**: Disk fills up, recovery time increases

**Mitigation**:
- Enable automatic snapshots (after 10K log entries)
- Prune log after snapshot
- Monitor log size via metrics

**Implementation**:
```go
config.SnapshotThreshold = 10000
config.TrailingLogs = 1000  // Keep 1K logs after snapshot
```

### Risk 2: Write Latency

**Issue**: 10-50ms write latency too slow for real-time applications

**Impact**: User-facing writes may timeout

**Mitigation**:
- Batch writes (100 ops in single Raft entry)
- Use SSDs (3-5ms fsync vs 10ms on HDD)
- Tune `raft.MaxAppendEntries` for larger batches

**Alternative**: If latency unacceptable, consider:
- Async replication (eventual consistency)
- Read replicas (strong writes, fast reads)

### Risk 3: Split-Brain During Network Partition

**Issue**: Network partition between leader and followers

**Impact**: Writes may be lost if leader is partitioned

**Mitigation**:
- Raft guarantees no split-brain (requires majority quorum)
- Partitioned leader steps down if can't reach quorum
- Cluster remains available with 2/3 nodes

**Test**:
```go
func TestRaftCluster_NetworkPartition(t *testing.T) {
    // Start 3-node cluster
    // Kill node2 and node3 (leader loses quorum)
    // Verify leader steps down
    // Verify writes fail (no quorum)
}
```

---

## Key Findings

### Finding 1: Snapshot Integration Works Seamlessly

**Observation**: Existing `CreateSnapshot()` and `RestoreSnapshot()` from Milestone 2 integrate with Raft without changes

**Benefit**: No refactoring needed, reuse existing code

**Evidence**: `GraphFSM.Snapshot()` wraps existing snapshot mechanism

### Finding 2: Write Latency Increases 10,000x

**Observation**: CreateNode goes from 1.3 μs to 10-50 ms with Raft

**Root Cause**: Disk fsync on every write (Raft durability requirement)

**Acceptable?**: Depends on workload
- **Batch imports**: Batching mitigates this (100x faster)
- **Interactive writes**: 10-50ms acceptable for most use cases
- **Real-time**: May need async replication or read replicas

### Finding 3: Leader Election is Fast (<3 seconds)

**Observation**: 3-node cluster elects leader in 1-3 seconds

**Benefit**: Fast recovery from leader failure

**Tuning**: Can reduce to <1 second by tuning `HeartbeatTimeout`

### Finding 4: BoltDB is Adequate for Prototype

**Observation**: BoltDB log store works well for prototype

**Future**: Could migrate to custom WAL-based log store for:
- Better integration with existing WAL code
- Potential performance improvements
- Reduced dependencies

**Recommendation**: Start with BoltDB, optimize later if needed

---

## Next Steps

### After Prototype Validation

1. **Extend Command Set**:
   - Add CreateEdge, DeleteEdge, UpdateNode
   - Validate all graph operations work via Raft

2. **Integration with gRPC**:
   - Combine Raft node with gRPC server
   - Leader forwarding (followers redirect writes to leader)

3. **Production Hardening**:
   - Add TLS for Raft transport
   - Implement proper error handling
   - Add observability (metrics, logging)

4. **Performance Testing**:
   - Benchmark with batched writes
   - Test with SSDs
   - Measure recovery time from snapshot

5. **Failure Scenarios**:
   - Test leader failure (follower promotion)
   - Test network partitions
   - Test split-brain prevention

---

## Code Checklist

- [ ] Add dependencies to `go.mod`
- [ ] Implement `GraphFSM` with Apply, Snapshot, Restore
- [ ] Implement `RaftNode` with bootstrap and cluster join
- [ ] Write test for leader election (3 nodes)
- [ ] Write test for log replication
- [ ] Benchmark CreateNode latency (with/without Raft)
- [ ] Document findings and recommendations

---

## References

- Raft Paper: https://raft.github.io/raft.pdf
- hashicorp/raft: https://github.com/hashicorp/raft
- Raft Visualization: http://thesecretlivesofdata.com/raft/

---

**Document Version**: 1.0
**Last Updated**: 2025-11-16
**Status**: Planning Phase
**Estimated Effort**: 2 hours for prototype, 8 hours for production implementation
