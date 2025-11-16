# Cluso GraphDB

A high-performance, feature-rich graph database built from scratch in Go. Cluso combines modern storage techniques with powerful graph algorithms and multiple query interfaces.

## Features

### Core Database

- **LSM-Tree Storage** - Efficient write-heavy workloads with leveled compaction
- **Write-Ahead Logging (WAL)** - Durability and crash recovery
- **Bloom Filters** - Fast negative lookups
- **Block Cache** - LRU caching for read performance
- **Property Indexes** - Fast property-based lookups
- **Batched Operations** - Efficient bulk data loading

### Graph Algorithms

- **PageRank** - Node importance scoring with convergence detection
- **Betweenness Centrality** - Identify bridge nodes
- **Community Detection** - Label propagation algorithm
- **Shortest Path** - Bidirectional BFS and weighted Dijkstra
- **Graph Traversal** - DFS/BFS with configurable depth and direction

### 🔍 Query Interfaces

#### 1. Cypher-like Query Language

```cypher
MATCH (p:Person)-[:KNOWS]->(f:Person)
WHERE p.age > 25
RETURN p, f
```

#### 2. REST API Server

```bash
# Start the server
./bin/server --port 8080 --data ./data/server

# Query via HTTP
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (n:Person) RETURN n"}'
```

#### 3. Interactive CLI

```bash
./bin/cli

cluso> stats
cluso> query MATCH (p:Person) RETURN p
cluso> pagerank
```

#### 4. Beautiful TUI (Terminal UI)

```bash
./bin/tui
```

Interactive terminal interface with:

- Real-time dashboard with statistics
- 👥 Node browser with table navigation
- 🔍 Query console with syntax highlighting
- 🌐 ASCII graph visualization
- 📈 PageRank metrics with bar charts

### Distributed Features

- **ZeroMQ Replication** - Multiple replication patterns (PUB/SUB, PUSH/PULL, REQ/REP, DEALER/ROUTER)
- **Graph Partitioning** - Horizontal scaling with hash-based partitioning
- **Temporal Graphs** - Time-aware graph queries

### Performance Optimizations

- **Parallel Query Execution** - Worker pools for concurrent operations
- **Streaming Results** - Memory-efficient result iteration
- **Query Pipelines** - Composable query operations
- **In-Memory + Persistent** - Hybrid storage for optimal performance

## Architecture

```
┌─────────────────────────────────────────────────┐
│            Cluso GraphDB                         │
├─────────────────────────────────────────────────┤
│  User Interfaces                                 │
│  • Interactive TUI (Bubble Tea)                  │
│  • REST API Server                               │
│  • Query Language (Cypher-like)                  │
│  • Command-Line Interface                        │
├─────────────────────────────────────────────────┤
│  Query Layer (PARALLEL)                          │
│  • Lexer → Parser → Executor                     │
│  • Worker Pool                                   │
│  • Streaming Results                             │
│  • Query Pipelines                               │
├─────────────────────────────────────────────────┤
│  Algorithm Layer                                 │
│  • PageRank, Centrality, Community Detection    │
│  • Bidirectional BFS, Weighted Dijkstra         │
│  • Graph Traversal (DFS/BFS)                     │
├─────────────────────────────────────────────────┤
│  Storage Layer (LSM)                             │
│  • MemTable → Immutable MemTable → SSTables     │
│  • Bloom Filters + Block Cache                  │
│  • Leveled Compaction                            │
│  • Write-Ahead Logging (WAL)                     │
├─────────────────────────────────────────────────┤
│  Advanced Features                               │
│  • Temporal Graphs (time-aware queries)         │
│  • Graph Partitioning (horizontal scaling)      │
│  • ZeroMQ Replication (4 patterns)              │
└─────────────────────────────────────────────────┘
```

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/darraghdowney/cluso-graphdb
cd cluso-graphdb

# Build all binaries
go build -o bin/server ./cmd/server
go build -o bin/cli ./cmd/cli
go build -o bin/tui ./cmd/tui
go build -o bin/tui-demo ./cmd/tui-demo
```

### Try the Interactive TUI Demo

```bash
# Create demo database
./bin/tui-demo

# Launch the beautiful TUI
./bin/tui
```

The TUI includes:

- **Dashboard** - Real-time stats, uptime, query metrics
- **Nodes** - Browse all nodes in a table
- **Query** - Execute Cypher queries interactively
- **Graph** - Visualize your graph structure
- **Metrics** - PageRank analysis with visual bars

### Start the REST API Server

```bash
# Start server
./bin/server --port 8080 --data ./data/server

# In another terminal, run the API demo
go build -o bin/api-demo ./cmd/api-demo
./bin/api-demo
```

### Use the CLI

```bash
./bin/cli

# Available commands:
# - help, stats, demo
# - create-node, create-edge, list-nodes, get-node
# - neighbors, traverse, path
# - pagerank, betweenness
# - query (execute Cypher queries)
```

## 📖 Usage Examples

### Programmatic Usage

```go
package main

import (
    "github.com/darraghdowney/cluso-graphdb/pkg/storage"
    "github.com/darraghdowney/cluso-graphdb/pkg/algorithms"
    "github.com/darraghdowney/cluso-graphdb/pkg/query"
)

func main() {
    // Create graph
    graph, _ := storage.NewGraphStorage("./data")
    defer graph.Close()

    // Create nodes
    alice, _ := graph.CreateNode(
        []string{"Person"},
        map[string]storage.Value{
            "name": storage.StringValue("Alice"),
            "age":  storage.IntValue(30),
        },
    )

    bob, _ := graph.CreateNode(
        []string{"Person"},
        map[string]storage.Value{
            "name": storage.StringValue("Bob"),
            "age":  storage.IntValue(25),
        },
    )

    // Create edge
    graph.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)

    // Run PageRank
    opts := algorithms.PageRankOptions{
        MaxIterations: 100,
        DampingFactor: 0.85,
        Tolerance:     1e-6,
    }
    result, _ := algorithms.PageRank(graph, opts)

    // Execute query
    executor := query.NewExecutor(graph)
    lexer := query.NewLexer("MATCH (p:Person) WHERE p.age > 25 RETURN p")
    tokens, _ := lexer.Tokenize()
    parser := query.NewParser(tokens)
    parsedQuery, _ := parser.Parse()
    results, _ := executor.Execute(parsedQuery)
}
```

### Query Language Examples

```cypher
-- Find all people
MATCH (p:Person) RETURN p

-- Find friends of Alice
MATCH (p:Person {name: "Alice"})-[:KNOWS]->(f)
RETURN f

-- Find paths
MATCH (a:Person)-[:KNOWS*1..3]->(b:Person)
WHERE a.age > 25
RETURN a, b

-- Create nodes
CREATE (p:Person {name: "Alice", age: 30})

-- Create relationships
MATCH (a:Person {name: "Alice"}), (b:Person {name: "Bob"})
CREATE (a)-[:KNOWS]->(b)

-- Update properties
MATCH (p:Person {name: "Alice"})
SET p.age = 31

-- Delete nodes
MATCH (p:Person {name: "Alice"})
DELETE p
```

### REST API Examples

```bash
# Health check
curl http://localhost:8080/health

# Get metrics
curl http://localhost:8080/metrics

# Create node
curl -X POST http://localhost:8080/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["Person"],
    "properties": {"name": "Alice", "age": 30}
  }'

# Create edge
curl -X POST http://localhost:8080/edges \
  -H "Content-Type: application/json" \
  -d '{
    "from_node_id": 1,
    "to_node_id": 2,
    "type": "KNOWS",
    "weight": 1.0
  }'

# Execute query
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (p:Person) RETURN p"}'

# Find shortest path
curl -X POST http://localhost:8080/shortest-path \
  -H "Content-Type: application/json" \
  -d '{
    "start_node_id": 1,
    "end_node_id": 5,
    "max_depth": 10
  }'

# Run PageRank
curl -X POST http://localhost:8080/algorithms \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "pagerank",
    "parameters": {
      "iterations": 10,
      "damping_factor": 0.85
    }
  }'

# Batch create nodes
curl -X POST http://localhost:8080/nodes/batch \
  -H "Content-Type: application/json" \
  -d '{
    "nodes": [
      {"labels": ["Person"], "properties": {"name": "Alice"}},
      {"labels": ["Person"], "properties": {"name": "Bob"}}
    ]
  }'
```

## Benchmarks

### Available Benchmarks

```bash
# Basic operations benchmark
go build -o bin/benchmark ./cmd/benchmark
./bin/benchmark --nodes 10000 --edges 30000 --traversals 100

# Batched WAL benchmark
go build -o bin/benchmark-batched ./cmd/benchmark-batched
./bin/benchmark-batched --nodes 10000 --batch 100 --flush 100us

# Index performance
go build -o bin/benchmark-index ./cmd/benchmark-index
./bin/benchmark-index --nodes 10000

# LSM storage
go build -o bin/benchmark-lsm ./cmd/benchmark-lsm
./bin/benchmark-lsm --writes 10000 --reads 1000 --value-size 1024

# Graph storage
go build -o bin/benchmark-graph-storage ./cmd/benchmark-graph-storage
./bin/benchmark-graph-storage --nodes 10000 --edges 50000

# Parallel queries
go build -o bin/benchmark-parallel ./cmd/benchmark-parallel
./bin/benchmark-parallel --nodes 10000 --workers 8

# Algorithms
go build -o bin/benchmark-algorithms ./cmd/benchmark-algorithms
./bin/benchmark-algorithms --nodes 1000 --edges 5000

# Query language
go build -o bin/benchmark-query ./cmd/benchmark-query
./bin/benchmark-query --nodes 1000
```

### Performance Results

**Validated with 5M node capacity test** (see [detailed improvements](docs/LSM_PERFORMANCE_IMPROVEMENTS.md)):

| Metric | Performance | Notes |
|--------|-------------|-------|
| **Write Throughput** | 430K nodes/sec | Sustained rate over 5M nodes |
| **Read Throughput** | 50K+ reads/sec | Random reads, 10K sample |
| **Read Latency** | 19.7 µs average | P50, cold cache |
| **Cached Read Latency** | 2.7 µs average | 7.3x speedup on hot data |
| **Memory Efficiency** | 80 bytes/node | Stable, no leaks |
| **Total Memory (5M nodes)** | 385 MB | Including 50M edges |

**Additional Operations:**

| Operation | Throughput | Latency |
|-----------|-----------|---------|
| Property Lookup (indexed) | ~500K ops/sec | <2μs |
| Shortest Path (1K nodes) | ~1K queries/sec | ~1ms |
| PageRank (10K nodes) | Converges in <100ms | - |
| Query Execution | ~0.6-1.0x procedural | Minimal overhead |

## 📂 Project Structure

```
cluso-graphdb/
├── cmd/
│   ├── server/          # REST API server
│   ├── cli/             # Interactive CLI
│   ├── tui/             # Beautiful terminal UI
│   ├── tui-demo/        # TUI demo data setup
│   ├── api-demo/        # REST API demo client
│   ├── benchmark-*/     # Performance benchmarks
├── pkg/
│   ├── storage/         # LSM-tree storage engine
│   ├── algorithms/      # Graph algorithms
│   ├── query/           # Query language (lexer, parser, executor)
│   ├── api/             # REST API types and server
│   ├── partitioning/    # Graph partitioning
│   ├── replication/     # ZeroMQ replication
│   └── parallel/        # Parallel query execution
├── data/                # Database files (gitignored)
└── bin/                 # Compiled binaries (gitignored)
```

## Configuration

### Server Options

```bash
./bin/server --help

Flags:
  --port int        HTTP server port (default 8080)
  --data string     Data directory (default "./data/server")
```

### Storage Configuration

Default settings in code (customizable):

```go
opts := storage.Options{
    MemTableSize:  64 * 1024 * 1024,  // 64MB
    BlockSize:     4096,               // 4KB
    CacheSize:     100,                // 100 blocks
    BloomFPR:      0.01,               // 1% false positive rate
    BatchSize:     100,                // WAL batch size
    FlushInterval: 100 * time.Microsecond,
}
```

## 🧪 Testing

```bash
# Run all tests
go test ./...

# Test specific package
go test ./pkg/storage
go test ./pkg/algorithms
go test ./pkg/query

# Run with race detector
go test -race ./...

# Verbose output
go test -v ./...
```

## 🎨 TUI Features

The Terminal UI (built with Bubble Tea, Bubbles, and Lipgloss) provides:

1. **Dashboard View**
   - Real-time node/edge counts
   - Uptime tracking
   - Query statistics
   - Quick action guide

2. **Nodes View**
   - Table-based node browser
   - Keyboard navigation (↑/↓, j/k)
   - Display node IDs, labels, properties

3. **Query View**
   - Interactive query input
   - Execute Cypher-like queries
   - Display results with success/error messages
   - Query examples and syntax help

4. **Graph View**
   - ASCII art graph visualization
   - Shows node connections
   - Displays relationship types
   - Handles large graphs gracefully

5. **Metrics View**
   - Live PageRank computation
   - Top nodes by score
   - Visual bar charts
   - Performance metrics

**Keyboard Controls:**

- `Tab` / `Shift+Tab` - Navigate views
- `↑/↓` or `j/k` - Navigate lists
- `Enter` - Execute query
- `q` or `Ctrl+C` - Quit

## Advanced Features

### Temporal Graphs

Query graph state at specific points in time:

```go
// Get neighbors at timestamp
neighbors := graph.GetNeighborsAtTime(nodeID, timestamp)

// Time-aware traversal
visited := graph.TraverseAtTime(startID, maxDepth, direction, timestamp)
```

### Graph Partitioning

Horizontal scaling with hash-based partitioning:

```go
partitioner := partitioning.NewHashPartitioner(numPartitions)
partition := partitioner.GetPartition(nodeID)
```

### ZeroMQ Replication

Four replication patterns:

1. **PUB/SUB** - Broadcast replication
2. **PUSH/PULL** - Work distribution
3. **REQ/REP** - Synchronous replication
4. **DEALER/ROUTER** - Load-balanced replication

```go
// Leader setup
leader := replication.NewLeader("tcp://*:5555", replication.PatternPubSub)

// Follower setup
follower := replication.NewFollower("tcp://localhost:5555", replication.PatternPubSub)
```

## 🤝 Contributing

Contributions are welcome! Areas for improvement:

- Additional graph algorithms (community detection variants, centrality measures)
- Query language features (aggregations, subqueries, UNION)
- Storage optimizations (compression, tiered storage)
- Distributed consensus (Raft, Paxos)
- Web-based UI (React/Vue dashboard)
- Additional data types (geospatial, full-text search)

## 📄 License

Cluso GraphDB is released under the [MIT License](LICENSE).

### Community Edition (Free)

- All core features
- Unlimited nodes and edges
- Full API access
- Open source (MIT)
- Community support (GitHub Issues)
- Use for personal projects, open source, evaluation

### Professional Edition ($49/month)

- Everything in Community
- Commercial use license
- Email support (48h response)
- Priority bug fixes
- Performance consultation

### Enterprise Edition ($299/month)

- Everything in Professional
- Priority support (24h response)
- Architecture consultation
- Early access to distributed features
- Custom SLA available

For commercial licensing details, see [COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md)

## 🙏 Acknowledgments

Built with:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [ZeroMQ](https://github.com/pebbe/zmq4) - Distributed messaging

## 🚦 Roadmap

- [x] Core graph storage (nodes, edges, properties)
- [x] LSM-tree persistent storage
- [x] Write-ahead logging
- [x] Property indexes
- [x] Graph algorithms (PageRank, centrality, community)
- [x] Cypher-like query language
- [x] REST API server
- [x] Interactive CLI
- [x] Beautiful TUI
- [x] Parallel query execution
- [x] Temporal graphs
- [x] Graph partitioning
- [x] ZeroMQ replication
- [ ] Distributed consensus (Raft)
- [ ] Query optimizer
- [ ] Schema validation
- [ ] Full-text search
- [ ] Geospatial queries
- [ ] Web-based dashboard

---

**Built with ❤️ in Go**

For questions, issues, or feature requests, please open an issue on GitHub.
