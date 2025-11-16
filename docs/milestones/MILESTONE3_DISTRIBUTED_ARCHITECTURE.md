# Milestone 3: Distributed Architecture Design

## Executive Summary

**Goal**: Scale from 5M nodes (Milestone 2) to **100M+ nodes** across a distributed cluster

**Approach**: Implement Raft consensus for replication, gRPC for communication, and consistent hashing for data sharding

**Target Deployment**: 3-5 node cluster with strong consistency and fault tolerance

**Key Capabilities**:
- **Capacity**: 100M nodes across cluster (20M-33M per node)
- **Consistency**: Strong consistency via Raft consensus
- **Availability**: Survives 1-2 node failures (3-5 node cluster)
- **Latency**: P99 < 10ms for local queries, P99 < 20ms for cross-shard queries
- **Throughput**: 100K+ writes/sec, 1M+ reads/sec cluster-wide

---

## 1. Goals & Constraints

### 1.1 Functional Requirements

**FR1: Distributed Storage**
- Store 100M nodes across 3-5 machines
- Each node stores 20M-33M graph nodes locally
- Total cluster memory: 96-160 GB (5x32 GB nodes)

**FR2: Strong Consistency**
- All writes go through Raft consensus
- Reads see the latest committed state
- No split-brain scenarios

**FR3: Fault Tolerance**
- Cluster survives single node failure (3-node minimum)
- Cluster survives two node failures (5-node recommended)
- Automatic leader election on failure
- Configurable replication factor (default: 3x)

**FR4: Distributed Queries**
- Local queries execute on single node (fast path)
- Cross-shard queries use scatter-gather pattern
- Graph traversals work across node boundaries
- Shortest path, BFS, DFS across distributed graph

### 1.2 Non-Functional Requirements

**NFR1: Performance**
- Write latency: P99 < 50ms (including Raft replication)
- Read latency (local): P99 < 5ms
- Read latency (cross-shard): P99 < 20ms
- Throughput: 100K+ writes/sec, 1M+ reads/sec (cluster-wide)

**NFR2: Availability**
- Failover time: < 5 seconds
- Data availability: 99.9%
- Zero data loss on single node failure

**NFR3: Scalability**
- Horizontal scaling: Add nodes to increase capacity
- Linear scaling up to 10 nodes
- Rebalancing support (future: automatic shard migration)

**NFR4: Operational**
- Zero-downtime rolling upgrades
- Incremental migration from Milestone 2
- Monitoring and observability (Prometheus metrics)

### 1.3 Constraints

**C1: Backward Compatibility**
- Must support migration from Milestone 2 single-node deployment
- Existing WAL and snapshot formats remain valid
- GraphStorage API remains unchanged

**C2: Technology Stack**
- Language: Go (existing codebase)
- Consensus: Raft (hashicorp/raft library)
- RPC: gRPC with Protocol Buffers
- No external dependencies beyond Go modules

**C3: Resource Limits**
- Target deployment: Cloud VMs or bare metal servers
- Per-node RAM: 32 GB minimum
- Per-node disk: 100 GB SSD minimum
- Network: 1 Gbps minimum between nodes

---

## 2. Architecture Overview

### 2.1 Component Diagram

```
┌────────────────────────────────────────────────────────────┐
│                        Client Layer                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │  gRPC Client │  │  HTTP Client │  │  CLI Client  │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
└─────────┼──────────────────┼──────────────────┼────────────┘
          │                  │                  │
          └──────────────────┼──────────────────┘
                             │
          ┌──────────────────▼──────────────────┐
          │         Load Balancer               │
          │    (Round-robin to any node)        │
          └──────────────────┬──────────────────┘
                             │
          ┌──────────────────┼──────────────────┐
          │                  │                  │
┌─────────▼─────────┐  ┌─────▼──────────┐  ┌──▼─────────────┐
│     Node 1        │  │    Node 2      │  │    Node 3      │
│  (Raft Leader)    │  │  (Follower)    │  │  (Follower)    │
├───────────────────┤  ├────────────────┤  ├────────────────┤
│  Query Router     │  │  Query Router  │  │  Query Router  │
│  ┌─────────────┐  │  │  ┌──────────┐  │  │  ┌──────────┐  │
│  │ gRPC Server │  │  │  │ gRPC Srv │  │  │  │ gRPC Srv │  │
│  └─────────────┘  │  │  └──────────┘  │  │  └──────────┘  │
├───────────────────┤  ├────────────────┤  ├────────────────┤
│  Raft Consensus   │◄─┼─►Raft Consensus│◄─┼─►Raft Consensus│
│  ┌─────────────┐  │  │  ┌──────────┐  │  │  ┌──────────┐  │
│  │ Log Store   │  │  │  │Log Store │  │  │  │Log Store │  │
│  │ FSM (Graph) │  │  │  │FSM(Graph)│  │  │  │FSM(Graph)│  │
│  └─────────────┘  │  │  └──────────┘  │  │  └──────────┘  │
├───────────────────┤  ├────────────────┤  ├────────────────┤
│  Sharding Layer   │  │  Sharding Lyr  │  │  Sharding Lyr  │
│  ┌─────────────┐  │  │  ┌──────────┐  │  │  ┌──────────┐  │
│  │  Shard Map  │  │  │  │Shard Map │  │  │  │Shard Map │  │
│  │  Router     │  │  │  │Router    │  │  │  │Router    │  │
│  └─────────────┘  │  │  └──────────┘  │  │  └──────────┘  │
├───────────────────┤  ├────────────────┤  ├────────────────┤
│  Storage Layer    │  │  Storage Layer │  │  Storage Layer │
│  ┌─────────────┐  │  │  ┌──────────┐  │  │  ┌──────────┐  │
│  │GraphStorage │  │  │  │ Graph    │  │  │  │ Graph    │  │
│  │(Milestone 2)│  │  │  │ Storage  │  │  │  │ Storage  │  │
│  ├─────────────┤  │  │  ├──────────┤  │  │  ├──────────┤  │
│  │  EdgeStore  │  │  │  │EdgeStore │  │  │  │EdgeStore │  │
│  │  (LSM+LRU)  │  │  │  │(LSM+LRU) │  │  │  │(LSM+LRU) │  │
│  └─────────────┘  │  │  └──────────┘  │  │  └──────────┘  │
└───────────────────┘  └────────────────┘  └────────────────┘
  Shard 0 (0-33M)      Shard 1 (33-66M)    Shard 2 (66-100M)
```

### 2.2 Data Flow

**Write Path (Strong Consistency)**:
1. Client sends `CreateNode(label, props)` to any cluster node
2. Node forwards request to Raft leader (if not leader)
3. Leader appends to Raft log and replicates to followers
4. Once majority acknowledges, leader commits to FSM (GraphStorage)
5. FSM applies write to local storage (same Milestone 2 code)
6. Leader responds to client with success
7. Followers asynchronously apply committed log entries

**Read Path (Local - Fast)**:
1. Client sends `GetNode(id)` to any cluster node
2. Node calculates shard: `hash(id) % num_nodes`
3. If local shard: Read from local GraphStorage, return result
4. Query completes in P99 < 5ms

**Read Path (Cross-shard - Scatter-Gather)**:
1. Client sends `FindNodesByLabel(label)` to any cluster node
2. Coordinator node broadcasts query to all shards
3. Each shard executes local query on GraphStorage
4. Results stream back to coordinator
5. Coordinator merges and returns to client
6. Query completes in P99 < 20ms

**Traversal Path (Multi-hop)**:
1. Client sends `BFS(startNode, maxDepth)` to any cluster node
2. Coordinator determines shard for `startNode`
3. Coordinator performs BFS, fetching remote nodes via gRPC
4. Each hop may require cross-shard RPC
5. Results stream back as nodes are discovered

---

## 3. Detailed Component Design

### 3.1 Raft Consensus Layer

**Purpose**: Ensure all writes are replicated and committed before acknowledgment

**Implementation**: `pkg/raft/`

#### 3.1.1 Raft FSM (Finite State Machine)

File: `pkg/raft/fsm.go`

```go
// GraphFSM implements raft.FSM interface
type GraphFSM struct {
    storage *storage.GraphStorage  // Existing Milestone 2 storage
    mu      sync.RWMutex
}

// Apply applies a Raft log entry to the state machine
func (f *GraphFSM) Apply(log *raft.Log) interface{} {
    var cmd Command
    if err := json.Unmarshal(log.Data, &cmd); err != nil {
        return err
    }

    switch cmd.Type {
    case "CreateNode":
        return f.storage.CreateNode(cmd.Label, cmd.Properties)
    case "CreateEdge":
        return f.storage.CreateEdge(cmd.FromID, cmd.ToID, cmd.EdgeType, cmd.Properties)
    case "DeleteNode":
        return f.storage.DeleteNode(cmd.NodeID)
    case "DeleteEdge":
        return f.storage.DeleteEdge(cmd.EdgeID)
    case "UpdateNode":
        return f.storage.UpdateNodeProperties(cmd.NodeID, cmd.Properties)
    default:
        return fmt.Errorf("unknown command: %s", cmd.Type)
    }
}

// Snapshot creates a snapshot of the current state
func (f *GraphFSM) Snapshot() (raft.FSMSnapshot, error) {
    // Leverage existing snapshot.go from Milestone 2
    return f.storage.CreateSnapshot()
}

// Restore restores from a snapshot
func (f *GraphFSM) Restore(snapshot io.ReadCloser) error {
    return f.storage.RestoreSnapshot(snapshot)
}
```

**Key Design Decisions**:
- **Reuse GraphStorage**: FSM wraps existing Milestone 2 storage (no changes needed)
- **Command Log**: Serialize graph operations as JSON commands
- **Snapshot Integration**: Leverage existing snapshot.go (already implemented)
- **Idempotency**: Commands are idempotent (safe to replay)

#### 3.1.2 Raft Configuration

File: `pkg/raft/config.go`

```go
type RaftConfig struct {
    // Cluster membership
    NodeID      string            // Unique node identifier
    BindAddr    string            // "10.0.0.1:7000"
    Peers       []string          // ["node1:7000", "node2:7000"]

    // Raft tuning
    HeartbeatTimeout time.Duration // Default: 1s
    ElectionTimeout  time.Duration // Default: 1s
    LeaderLeaseTimeout time.Duration // Default: 500ms

    // Storage
    LogDir      string            // "/var/lib/graphdb/raft"
    SnapshotDir string            // "/var/lib/graphdb/snapshots"

    // Performance
    MaxLogEntries  uint64         // Trigger snapshot after N entries
    SnapshotInterval time.Duration // Default: 2 hours
}
```

**Tuning for Graph Workloads**:
- Short heartbeat (1s) for fast failure detection
- MaxLogEntries triggers compaction (avoid unbounded log growth)
- Snapshot interval balances recovery time vs overhead

#### 3.1.3 Log Store Integration

File: `pkg/raft/log_store.go`

**Option 1**: Use `raft-boltdb` (hashicorp's BoltDB store)
- Pros: Battle-tested, simple integration
- Cons: Extra dependency

**Option 2**: Adapt existing WAL (`pkg/wal/wal.go`)
- Pros: Reuse existing code, fewer dependencies
- Cons: Need to implement `raft.LogStore` interface

**Recommendation**: Start with `raft-boltdb`, migrate to custom WAL later if needed

---

### 3.2 gRPC Communication Layer

**Purpose**: Enable inter-node communication for queries, replication, and coordination

**Implementation**: `pkg/api/`

#### 3.2.1 Protocol Buffer Definitions

File: `pkg/api/graphdb.proto`

```protobuf
syntax = "proto3";
package graphdb;
option go_package = "github.com/yourusername/graphdb/pkg/api";

// GraphDB service for distributed operations
service GraphDB {
    // Node operations
    rpc CreateNode(CreateNodeRequest) returns (Node);
    rpc GetNode(GetNodeRequest) returns (Node);
    rpc DeleteNode(DeleteNodeRequest) returns (Empty);
    rpc UpdateNode(UpdateNodeRequest) returns (Node);

    // Edge operations
    rpc CreateEdge(CreateEdgeRequest) returns (Edge);
    rpc GetEdge(GetEdgeRequest) returns (Edge);
    rpc DeleteEdge(DeleteEdgeRequest) returns (Empty);

    // Query operations
    rpc FindNodesByLabel(FindNodesByLabelRequest) returns (stream Node);
    rpc FindNodesByProperty(FindNodesByPropertyRequest) returns (stream Node);
    rpc FindEdgesByType(FindEdgesByTypeRequest) returns (stream Edge);

    // Traversal operations
    rpc BFS(TraversalRequest) returns (stream Node);
    rpc DFS(TraversalRequest) returns (stream Node);
    rpc ShortestPath(ShortestPathRequest) returns (PathResponse);

    // Cluster operations
    rpc GetShardMap(Empty) returns (ShardMapResponse);
    rpc GetNodeShard(GetNodeShardRequest) returns (ShardResponse);
}

// Messages
message Node {
    uint64 id = 1;
    string label = 2;
    map<string, Property> properties = 3;
}

message Edge {
    uint64 id = 1;
    uint64 from_id = 2;
    uint64 to_id = 3;
    string edge_type = 4;
    map<string, Property> properties = 5;
}

message Property {
    oneof value {
        string string_value = 1;
        int64 int_value = 2;
        double float_value = 3;
        bool bool_value = 4;
        bytes bytes_value = 5;
    }
}

message CreateNodeRequest {
    string label = 1;
    map<string, Property> properties = 2;
}

message GetNodeRequest {
    uint64 id = 1;
}

message FindNodesByLabelRequest {
    string label = 1;
}

message TraversalRequest {
    uint64 start_node_id = 1;
    int32 max_depth = 2;
    string direction = 3;  // "outgoing", "incoming", "both"
}

message ShortestPathRequest {
    uint64 from_id = 1;
    uint64 to_id = 2;
}

message PathResponse {
    repeated uint64 node_ids = 1;
    int32 length = 2;
}

message ShardMapResponse {
    map<uint32, string> shard_to_node = 1;  // shard_id -> node_address
    uint32 num_shards = 2;
}

message GetNodeShardRequest {
    uint64 node_id = 1;
}

message ShardResponse {
    uint32 shard_id = 1;
    string node_address = 2;
}

message Empty {}
```

#### 3.2.2 gRPC Server Implementation

File: `pkg/api/server.go`

```go
type Server struct {
    api.UnimplementedGraphDBServer

    storage    *storage.GraphStorage
    raft       *raft.Raft
    shardMap   *sharding.ShardMap
    peerConns  map[string]api.GraphDBClient  // Connections to other nodes
}

func (s *Server) CreateNode(ctx context.Context, req *api.CreateNodeRequest) (*api.Node, error) {
    // Check if we're the leader
    if s.raft.State() != raft.Leader {
        // Forward to leader
        leaderAddr := s.raft.Leader()
        return s.peerConns[leaderAddr].CreateNode(ctx, req)
    }

    // Serialize command
    cmd := Command{
        Type:       "CreateNode",
        Label:      req.Label,
        Properties: protoToProperties(req.Properties),
    }
    cmdBytes, _ := json.Marshal(cmd)

    // Apply via Raft (blocks until committed)
    future := s.raft.Apply(cmdBytes, 10*time.Second)
    if err := future.Error(); err != nil {
        return nil, err
    }

    // Extract result
    node := future.Response().(*storage.Node)
    return nodeToProto(node), nil
}

func (s *Server) GetNode(ctx context.Context, req *api.GetNodeRequest) (*api.Node, error) {
    // Determine shard
    shardID := s.shardMap.GetShard(req.Id)
    nodeAddr := s.shardMap.GetNodeAddress(shardID)

    // Local shard - fast path
    if s.shardMap.IsLocal(shardID) {
        node, err := s.storage.GetNode(req.Id)
        if err != nil {
            return nil, err
        }
        return nodeToProto(node), nil
    }

    // Remote shard - forward request
    return s.peerConns[nodeAddr].GetNode(ctx, req)
}

func (s *Server) FindNodesByLabel(req *api.FindNodesByLabelRequest, stream api.GraphDB_FindNodesByLabelServer) error {
    // Scatter-gather across all shards
    var wg sync.WaitGroup
    resultCh := make(chan *api.Node, 100)

    for shardID := uint32(0); shardID < s.shardMap.NumShards(); shardID++ {
        wg.Add(1)
        go func(sid uint32) {
            defer wg.Done()

            nodeAddr := s.shardMap.GetNodeAddress(sid)
            client := s.peerConns[nodeAddr]

            // Execute query on remote shard
            remoteStream, err := client.FindNodesByLabel(stream.Context(), req)
            if err != nil {
                return
            }

            // Stream results back
            for {
                node, err := remoteStream.Recv()
                if err == io.EOF {
                    break
                }
                if err != nil {
                    return
                }
                resultCh <- node
            }
        }(shardID)
    }

    // Close channel when all shards complete
    go func() {
        wg.Wait()
        close(resultCh)
    }()

    // Stream merged results to client
    for node := range resultCh {
        if err := stream.Send(node); err != nil {
            return err
        }
    }

    return nil
}
```

**Key Features**:
- **Leader Forwarding**: Non-leaders forward writes to leader
- **Local Fast Path**: Reads from local shard skip network
- **Scatter-Gather**: Parallel query execution across shards
- **Streaming**: Results stream to client (avoids buffering large results)

#### 3.2.3 Connection Pooling

File: `pkg/api/conn_pool.go`

```go
type ConnPool struct {
    conns map[string]*grpc.ClientConn
    mu    sync.RWMutex
}

func (p *ConnPool) GetConnection(addr string) (api.GraphDBClient, error) {
    p.mu.RLock()
    conn, exists := p.conns[addr]
    p.mu.RUnlock()

    if exists {
        return api.NewGraphDBClient(conn), nil
    }

    // Create new connection
    p.mu.Lock()
    defer p.mu.Unlock()

    // Double-check after acquiring write lock
    if conn, exists := p.conns[addr]; exists {
        return api.NewGraphDBClient(conn), nil
    }

    // Dial with keepalive
    conn, err := grpc.Dial(addr,
        grpc.WithInsecure(),  // TODO: Add TLS in production
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                10 * time.Second,
            Timeout:             3 * time.Second,
            PermitWithoutStream: true,
        }),
    )
    if err != nil {
        return nil, err
    }

    p.conns[addr] = conn
    return api.NewGraphDBClient(conn), nil
}
```

**Performance Optimizations**:
- Persistent connections with keepalive
- Connection pooling (avoid dial overhead)
- TODO: Add TLS for production security

---

### 3.3 Data Sharding Layer

**Purpose**: Distribute graph nodes across cluster for horizontal scalability

**Implementation**: `pkg/sharding/`

#### 3.3.1 Sharding Strategy

**Approach**: Hash-based sharding by node ID

```go
shard_id = hash(node_id) % num_shards
```

**Rationale**:
- **Simple**: Easy to implement and reason about
- **Balanced**: Uniform distribution (assuming good hash function)
- **Deterministic**: Same node ID always maps to same shard
- **Stateless**: No coordination needed to determine shard

**Alternative Considered** (Range-based sharding):
- Shard 0: node IDs 0-33M
- Shard 1: node IDs 33M-66M
- Shard 2: node IDs 66M-100M

**Pros**: Easier to add new shards incrementally
**Cons**: Risk of hotspots if IDs not uniformly distributed
**Decision**: Start with hash-based, add range-based in Milestone 4

#### 3.3.2 Shard Map

File: `pkg/sharding/shard_map.go`

```go
type ShardMap struct {
    numShards   uint32
    shardToNode map[uint32]string  // shard_id -> node_address
    nodeToShard map[string][]uint32  // node_address -> shard_ids
    localNode   string              // This node's address
    mu          sync.RWMutex
}

func NewShardMap(numShards uint32, localNode string, clusterNodes []string) *ShardMap {
    sm := &ShardMap{
        numShards:   numShards,
        shardToNode: make(map[uint32]string),
        nodeToShard: make(map[string][]uint32),
        localNode:   localNode,
    }

    // Distribute shards evenly across nodes
    for shardID := uint32(0); shardID < numShards; shardID++ {
        nodeIdx := shardID % uint32(len(clusterNodes))
        nodeAddr := clusterNodes[nodeIdx]
        sm.shardToNode[shardID] = nodeAddr
        sm.nodeToShard[nodeAddr] = append(sm.nodeToShard[nodeAddr], shardID)
    }

    return sm
}

func (sm *ShardMap) GetShard(nodeID uint64) uint32 {
    h := fnv.New32a()
    binary.Write(h, binary.LittleEndian, nodeID)
    return h.Sum32() % sm.numShards
}

func (sm *ShardMap) GetNodeAddress(shardID uint32) string {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    return sm.shardToNode[shardID]
}

func (sm *ShardMap) IsLocal(shardID uint32) bool {
    return sm.GetNodeAddress(shardID) == sm.localNode
}

func (sm *ShardMap) GetLocalShards() []uint32 {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    return sm.nodeToShard[sm.localNode]
}
```

**Features**:
- **Even Distribution**: Round-robin shard assignment
- **Fast Lookups**: O(1) shard → node mapping
- **Thread-Safe**: RWMutex for concurrent access

**Future Enhancement**: Rebalancing support for dynamic shard migration

#### 3.3.3 Edge Handling in Sharded Environment

**Challenge**: Edges span two nodes, which may be on different shards

**Solution**: Store edge on the source node's shard

```
Edge (node_42 -> node_89)
Stored on: shard(hash(42))
```

**Implications**:
- **GetOutgoingEdges(node_42)**: Local query (fast)
- **GetIncomingEdges(node_89)**: Requires global scan (slow)

**Optimization** (Phase 2):
- Maintain reverse index on destination shard
- Trade-off: 2x storage for edges vs faster incoming queries

**Traversal Impact**:
- BFS outgoing: Efficient (follow edge pointers)
- BFS incoming: Slower (may need cross-shard lookups)

---

### 3.4 Distributed Query Execution

**Purpose**: Execute graph queries that span multiple shards

**Implementation**: `pkg/distributed/`

#### 3.4.1 Query Router

File: `pkg/distributed/router.go`

```go
type QueryRouter struct {
    shardMap *sharding.ShardMap
    clients  map[string]api.GraphDBClient
}

func (qr *QueryRouter) FindNodesByLabel(ctx context.Context, label string) ([]*Node, error) {
    // Determine which shards may contain nodes with this label
    // Optimization: If label index is sharded, query only relevant shards
    // For now: Query all shards (conservative approach)

    shards := qr.shardMap.GetAllShards()

    var mu sync.Mutex
    var results []*Node
    var wg sync.WaitGroup

    for _, shardID := range shards {
        wg.Add(1)
        go func(sid uint32) {
            defer wg.Done()

            nodeAddr := qr.shardMap.GetNodeAddress(sid)
            client := qr.clients[nodeAddr]

            stream, err := client.FindNodesByLabel(ctx, &api.FindNodesByLabelRequest{Label: label})
            if err != nil {
                return
            }

            for {
                node, err := stream.Recv()
                if err == io.EOF {
                    break
                }
                if err != nil {
                    return
                }

                mu.Lock()
                results = append(results, protoToNode(node))
                mu.Unlock()
            }
        }(shardID)
    }

    wg.Wait()
    return results, nil
}
```

#### 3.4.2 Distributed Traversal

File: `pkg/distributed/traversal.go`

```go
type DistributedTraverser struct {
    router   *QueryRouter
    shardMap *sharding.ShardMap
}

func (dt *DistributedTraverser) BFS(startNodeID uint64, maxDepth int) ([]*Node, error) {
    visited := make(map[uint64]bool)
    queue := []uint64{startNodeID}
    result := []*Node{}
    depth := 0

    for len(queue) > 0 && depth < maxDepth {
        levelSize := len(queue)

        for i := 0; i < levelSize; i++ {
            nodeID := queue[0]
            queue = queue[1:]

            if visited[nodeID] {
                continue
            }
            visited[nodeID] = true

            // Fetch node (may be remote)
            node, err := dt.router.GetNode(nodeID)
            if err != nil {
                continue
            }
            result = append(result, node)

            // Fetch outgoing edges (may be remote)
            edges, err := dt.router.GetOutgoingEdges(nodeID)
            if err != nil {
                continue
            }

            // Add neighbors to queue
            for _, edge := range edges {
                if !visited[edge.ToID] {
                    queue = append(queue, edge.ToID)
                }
            }
        }

        depth++
    }

    return result, nil
}
```

**Performance Characteristics**:
- **Latency**: Each BFS level requires cross-shard RPC (adds 1-5ms per level)
- **Optimization**: Batch node fetches (fetch 100 nodes in single RPC)
- **Future**: Compute shipping (send traversal code to shards, reduce network)

---

## 4. Deployment Architecture

### 4.1 Three-Node Cluster (Minimum)

```
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│   Node 1        │  │   Node 2        │  │   Node 3        │
│   10.0.0.1      │  │   10.0.0.2      │  │   10.0.0.3      │
├─────────────────┤  ├─────────────────┤  ├─────────────────┤
│ Raft: Leader    │  │ Raft: Follower  │  │ Raft: Follower  │
│ Shards: 0,3,6   │  │ Shards: 1,4,7   │  │ Shards: 2,5,8   │
│ gRPC: :9000     │  │ gRPC: :9000     │  │ gRPC: :9000     │
│ Raft: :7000     │  │ Raft: :7000     │  │ Raft: :7000     │
├─────────────────┤  ├─────────────────┤  ├─────────────────┤
│ RAM: 32 GB      │  │ RAM: 32 GB      │  │ RAM: 32 GB      │
│ Disk: 100 GB    │  │ Disk: 100 GB    │  │ Disk: 100 GB    │
│ Capacity: 33M   │  │ Capacity: 33M   │  │ Capacity: 33M   │
└─────────────────┘  └─────────────────┘  └─────────────────┘
   Total Cluster Capacity: ~100M nodes
```

**Replication**: 3x (each write replicated to 3 nodes via Raft)
**Fault Tolerance**: Survives 1 node failure (needs 2/3 quorum)
**Network**: 1 Gbps between nodes (cloud or datacenter LAN)

### 4.2 Five-Node Cluster (Recommended)

```
Node 1-5: Same as above, but with 5 nodes
Shards: 15 total (0-14), 3 shards per node
Replication: 3x (configurable)
Fault Tolerance: Survives 2 node failures (needs 3/5 quorum)
Capacity: ~165M nodes (33M per node)
```

### 4.3 Network Ports

| Port | Protocol | Purpose |
|------|----------|---------|
| 9000 | gRPC | Client API and inter-node queries |
| 7000 | TCP | Raft consensus (leader election, log replication) |
| 8080 | HTTP | Health checks, Prometheus metrics (optional) |

---

## 5. Migration Strategy

### 5.1 Migration from Milestone 2 (Single Node)

**Scenario**: Existing production deployment on single 32 GB machine

**Goal**: Migrate to 3-node distributed cluster with zero data loss

**Steps**:

1. **Preparation** (2 hours)
   - Provision 3 new servers (32 GB RAM each)
   - Install GraphDB Milestone 3 binaries on all nodes
   - Configure Raft cluster (node IDs, peer addresses)

2. **Initial Snapshot** (30 min)
   - On existing Milestone 2 node: Create snapshot
   - Snapshot captures all nodes, edges, indices
   - File: `/var/lib/graphdb/snapshot_milestone2.json`

3. **Bootstrap Cluster** (30 min)
   - Start Node 1 with snapshot restored
   - Start Node 2 and Node 3 as fresh nodes
   - Raft replicates state from Node 1 to 2 and 3
   - All nodes now have full replica (no sharding yet)

4. **Enable Sharding** (1 hour)
   - Configure shard map: 9 shards across 3 nodes
   - Background process redistributes nodes to correct shards
   - Node 1 moves shards 3-8 to Node 2 and Node 3
   - Verify data consistency after redistribution

5. **Cutover** (15 min)
   - Update client applications to use distributed cluster
   - Point clients to load balancer (round-robin to any node)
   - Monitor for errors, rollback if needed

6. **Decommission Old Node** (15 min)
   - Stop Milestone 2 single-node instance
   - Keep snapshot as backup

**Total Migration Time**: ~4.5 hours (mostly automated)
**Downtime**: 15 minutes (during cutover)

### 5.2 Zero-Downtime Migration (Advanced)

**Approach**: Dual-write pattern

1. **Set up distributed cluster** (parallel to existing single node)
2. **Dual-write**: Application writes to both old and new
3. **Backfill**: Copy historical data from old to new
4. **Verify consistency**: Compare data between old and new
5. **Cutover reads**: Point reads to new cluster
6. **Stop dual-write**: Remove old single-node write path

**Total Migration Time**: 2-3 days (gradual rollout)
**Downtime**: 0 minutes

---

## 6. Performance Projections

### 6.1 Write Performance

| Metric | Single Node (M2) | 3-Node Cluster (M3) | Notes |
|--------|------------------|---------------------|-------|
| Write Latency (P50) | 1.3 μs | 10 ms | Raft replication overhead |
| Write Latency (P99) | 5 μs | 50 ms | Network + disk fsync |
| Throughput | 769K writes/sec | 100K writes/sec | Bottleneck: Raft leader |

**Bottleneck Analysis**:
- Raft requires majority acknowledgment before commit
- Leader serializes all writes through log
- Network latency (1-5ms) dominates

**Mitigation**:
- Batch writes (commit 100 ops in single Raft entry)
- Use SSDs for faster fsync
- Tune Raft batch size and timeouts

### 6.2 Read Performance

| Metric | Single Node (M2) | 3-Node Cluster (M3) | Notes |
|--------|------------------|---------------------|-------|
| Read Latency (local) | 82 ns | 1 μs | gRPC overhead |
| Read Latency (remote) | N/A | 5 ms | Network + local read |
| Query (scatter-gather) | 200 μs | 15 ms | Parallel across 3 shards |
| Throughput (reads) | 12M reads/sec | 3M reads/sec | Per-node: 1M reads/sec |

**Cluster-wide Read Throughput**: 3M reads/sec (3 nodes × 1M each)

### 6.3 Capacity

| Metric | Single Node (M2) | 3-Node Cluster (M3) | 5-Node Cluster |
|--------|------------------|---------------------|----------------|
| Max Nodes | 5M | 100M | 165M |
| Max Edges | 50M | 1B | 1.65B |
| Total RAM | 32 GB | 96 GB | 160 GB |
| Total Disk | 100 GB | 300 GB | 500 GB |

---

## 7. Observability & Monitoring

### 7.1 Prometheus Metrics

File: `pkg/metrics/prometheus.go`

```go
var (
    // Raft metrics
    raftLeaderElections = prometheus.NewCounter(...)
    raftLogSize = prometheus.NewGauge(...)
    raftApplyLatency = prometheus.NewHistogram(...)

    // Query metrics
    queryLatency = prometheus.NewHistogramVec(...)
    queriesTotal = prometheus.NewCounterVec(...)

    // Storage metrics
    nodeCount = prometheus.NewGauge(...)
    edgeCount = prometheus.NewGauge(...)
    cacheHitRate = prometheus.NewGauge(...)

    // Network metrics
    grpcRequestsTotal = prometheus.NewCounterVec(...)
    grpcRequestDuration = prometheus.NewHistogramVec(...)
)
```

### 7.2 Health Checks

**Endpoint**: `GET /health`

```json
{
  "status": "healthy",
  "raft": {
    "state": "leader",
    "leader": "node1:7000",
    "peers": 3,
    "last_log_index": 1000000
  },
  "storage": {
    "nodes": 33000000,
    "edges": 330000000,
    "memory_mb": 8500
  },
  "cluster": {
    "total_nodes": 100000000,
    "shards": 9,
    "replication_factor": 3
  }
}
```

---

## 8. Security Considerations

### 8.1 Network Security

**TLS for gRPC** (Production requirement):
```go
creds, _ := credentials.NewServerTLSFromFile("cert.pem", "key.pem")
server := grpc.NewServer(grpc.Creds(creds))
```

**Raft Transport Security**:
- Use Raft TLS transport for consensus traffic
- Mutual TLS authentication between cluster nodes

### 8.2 Authentication & Authorization

**Future Enhancement** (Milestone 4):
- Client authentication (mTLS or API keys)
- Role-based access control (RBAC)
- Query authorization (user can only access certain labels)

---

## 9. Future Enhancements (Milestone 4+)

### 9.1 Dynamic Shard Rebalancing

**Current**: Static shard assignment at cluster creation
**Future**: Automatic rebalancing when adding/removing nodes

**Approach**:
1. Detect new node joining cluster
2. Calculate new shard distribution
3. Stream shard data to new node in background
4. Atomically switch shard ownership
5. Delete migrated data from old node

**Challenge**: Avoid query disruption during rebalancing

### 9.2 Read Replicas

**Goal**: Increase read throughput without affecting write performance

**Approach**:
- Add read-only follower nodes (not part of Raft quorum)
- Replicate committed log entries asynchronously
- Clients can query read replicas (eventual consistency)

**Use Case**: Analytics workloads (tolerate stale reads)

### 9.3 Geo-Replication

**Goal**: Deploy clusters in multiple datacenters for disaster recovery

**Approach**:
- Primary cluster (us-east-1): Handles writes
- Secondary cluster (eu-west-1): Async replication
- Failover: Promote secondary to primary if primary fails

**Latency**: Cross-region replication adds 50-200ms

### 9.4 Compute Shipping

**Goal**: Reduce network traffic for complex traversals

**Approach**:
- Send traversal logic (e.g., BFS algorithm) to data nodes
- Execute locally on each shard
- Return only final results

**Example**:
```go
// Instead of fetching nodes one-by-one
// Ship this closure to each shard:
func(shard *GraphStorage) []*Node {
    return shard.BFS(startNodeID, maxDepth)
}
```

---

## 10. Success Criteria

### 10.1 Functional

- [ ] 3-node cluster achieves consensus and elects leader
- [ ] Writes replicate to all nodes via Raft
- [ ] Reads work on any node (local or remote)
- [ ] Scatter-gather queries return correct results
- [ ] BFS/DFS traversals work across shard boundaries
- [ ] Cluster survives single node failure with <5s downtime

### 10.2 Performance

- [ ] Write latency: P99 < 50ms
- [ ] Read latency (local): P99 < 5ms
- [ ] Read latency (remote): P99 < 20ms
- [ ] Cluster-wide write throughput: >100K writes/sec
- [ ] Cluster-wide read throughput: >1M reads/sec

### 10.3 Scalability

- [ ] 3-node cluster handles 100M nodes
- [ ] 5-node cluster handles 165M nodes
- [ ] Memory per node: <32 GB for 33M nodes
- [ ] Linear scalability up to 10 nodes (projected)

### 10.4 Operational

- [ ] Zero-downtime migration from Milestone 2
- [ ] Prometheus metrics for monitoring
- [ ] Health check endpoint
- [ ] Automated integration tests for cluster scenarios

---

## 11. References

### 11.1 External Libraries

- **Raft**: https://github.com/hashicorp/raft
- **gRPC**: https://grpc.io/docs/languages/go/
- **Protocol Buffers**: https://developers.google.com/protocol-buffers
- **Prometheus**: https://prometheus.io/docs/guides/go-application/

### 11.2 Related Documentation

- Milestone 1: In-memory storage with WAL
- Milestone 2: Disk-backed edges with LRU cache
- Raft paper: https://raft.github.io/raft.pdf
- Consistent hashing: https://en.wikipedia.org/wiki/Consistent_hashing

---

**Document Version**: 1.0
**Last Updated**: 2025-11-16
**Status**: Planning Phase
**Next Steps**: Create technical spikes (Raft, gRPC, Sharding)
