# GraphDB

[![Tests](https://github.com/dd0wney/graphdb/actions/workflows/test.yml/badge.svg)](https://github.com/dd0wney/graphdb/actions/workflows/test.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/dd0wney/graphdb)](https://go.dev/)
[![Release](https://img.shields.io/github/v/release/dd0wney/graphdb)](https://github.com/dd0wney/graphdb/releases)
[![License](https://img.shields.io/github/license/dd0wney/graphdb)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/dd0wney/graphdb)](https://goreportcard.com/report/github.com/dd0wney/graphdb)

A high-performance, feature-rich graph database built from scratch in Go. GraphDB combines modern storage techniques with powerful graph algorithms and multiple query interfaces.

## v0.2.0 Released! 🎉

**New in v0.2.0:** Vector Search with HNSW indexes for semantic similarity queries.

## What's New

**Try it in 30 seconds:**

```bash
# Docker (recommended)
docker run -p 8080:8080 dd0wney/graphdb:latest

# Or download pre-built binary
wget https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Linux_x86_64.tar.gz
tar -xzf graphdb_0.1.0_Linux_x86_64.tar.gz
./cluso-server --port 8080
```

**Available for:**
- **Docker**: Multi-arch images (amd64, arm64) on [Docker Hub](https://hub.docker.com/r/dd0wney/graphdb)
- **macOS**: arm64 (M1/M2/M3) & x86_64 (Intel)
- **Linux**: arm64 & x86_64
- **Windows**: arm64 & x86_64

**Performance Highlights:**
- **5M nodes + 50M edges** in just **73MB RAM** (15.31 bytes/node)
- **330K writes/sec** - sustained throughput over 5M nodes
- **1.5M lookups/sec** - blazing fast random reads
- **152µs average latency** (cold cache), **38µs** (hot cache)
- **10ms** - average shortest path query

## Features

### Core Database

- **LSM-Tree Storage** - Efficient write-heavy workloads with leveled compaction
- **Write-Ahead Logging (WAL)** - Durability and crash recovery
- **Bloom Filters** - Fast negative lookups
- **Block Cache** - LRU caching for read performance
- **Property Indexes** - Fast property-based lookups
- **Batched Operations** - Efficient bulk data loading

### Vector Search (New in v0.2.0!)

- **HNSW Indexes** - Hierarchical Navigable Small World graphs for fast k-NN search
- **Multiple Distance Metrics** - Cosine, Euclidean, and Dot Product similarity
- **Automatic Index Sync** - Vector indexes stay in sync with node CRUD operations
- **Label Filtering** - Filter vector search results by node labels
- **Tunable Parameters** - Configure M, efConstruction, and ef for recall/speed tradeoffs

### Graph Algorithms

- **Cycle Detection** - Find all cycles in the graph using DFS with three-color marking
- **Topological Validators** - DAG validation, topological sort, tree detection, connectivity, bipartite graphs
- **PageRank** - Node importance scoring with convergence detection
- **Betweenness Centrality** - Identify bridge nodes
- **Community Detection** - Label propagation algorithm
- **Shortest Path** - Bidirectional BFS and weighted Dijkstra
- **Graph Traversal** - DFS/BFS with configurable depth and direction

### Constraint Validation

- **Property Constraints** - Validate node properties (required, type, range)
- **Cardinality Constraints** - Validate edge counts (min/max, direction)
- **Validation Framework** - Multi-constraint validation with detailed violation reporting
- **Severity Levels** - Error, Warning, Info classifications

### Query Interfaces

#### 1. Cypher-like Query Language

```cypher
MATCH (p:Person)-[:KNOWS]->(f:Person)
WHERE p.age > 25
RETURN p, f
```

#### 2. REST API Server

```bash
# Start the server
docker run -p 8080:8080 dd0wney/graphdb:latest

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
- Node browser with table navigation
- Query console with syntax highlighting
- ASCII graph visualization
- PageRank metrics with bar charts

#### 5. Admin CLI (Security Management)

```bash
# Generate encryption key
./bin/graphdb-admin security init --generate-key

# Check security health
./bin/graphdb-admin security health --token="your-jwt-token"

# Rotate encryption keys
./bin/graphdb-admin security rotate-keys --token="your-jwt-token"

# Export audit logs
./bin/graphdb-admin security audit-export --output=audit.json
```

See the [CLI Admin documentation](docs/CLI-ADMIN.md) for complete usage.

## Documentation

Comprehensive API documentation is available at **[graphdb.pages.dev](https://dd0wney.github.io/graphdb/)**

- **[Interactive API Explorer (Swagger UI)](https://dd0wney.github.io/graphdb/swagger.html)** - Try out API endpoints directly in your browser
- **[API Reference (Redoc)](https://dd0wney.github.io/graphdb/redoc.html)** - Beautiful, searchable API documentation
- **[Quick Start Guide](https://dd0wney.github.io/graphdb/API.md)** - Complete guide with examples and best practices
- **[OpenAPI Specification](https://dd0wney.github.io/graphdb/openapi.yaml)** - Machine-readable API spec

### Security Documentation

- **[CLI Admin Guide](docs/CLI-ADMIN.md)** - Command-line tools for security management
- **[Security Quick Start](docs/SECURITY-QUICKSTART.md)** - Get started with encryption and security features
- **[Security Integration Summary](SECURITY-INTEGRATION-SUMMARY.md)** - Architecture and implementation details
- **[Encryption Architecture](docs/internals/design/ENCRYPTION_ARCHITECTURE.md)** - Deep dive into encryption design

The documentation covers:
- Authentication (JWT tokens and API keys)
- Encryption at rest and in transit
- Security management and monitoring
- Audit logging and compliance
- All REST API endpoints
- Request/response schemas
- Code examples in multiple languages
- Error handling and best practices
- Rate limits and performance tips

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
│               GraphDB                            │
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

### Using Docker (Easiest)

```bash
# Run the server
docker run -p 8080:8080 dd0wney/graphdb:latest

# In another terminal, test it
curl http://localhost:8080/health

# Create a node
curl -X POST http://localhost:8080/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["Person"], "properties": {"name": "Alice", "age": 30}}'
```

### Using Pre-built Binaries

Download from [GitHub Releases](https://github.com/dd0wney/graphdb/releases/latest):

```bash
# Linux (x86_64)
wget https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Linux_x86_64.tar.gz
tar -xzf graphdb_0.1.0_Linux_x86_64.tar.gz
./cluso-server --port 8080

# Linux (arm64)
wget https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Linux_arm64.tar.gz
tar -xzf graphdb_0.1.0_Linux_arm64.tar.gz
./cluso-server --port 8080

# macOS (Intel)
wget https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Darwin_x86_64.tar.gz
tar -xzf graphdb_0.1.0_Darwin_x86_64.tar.gz
./cluso-server --port 8080

# macOS (Apple Silicon)
wget https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Darwin_arm64.tar.gz
tar -xzf graphdb_0.1.0_Darwin_arm64.tar.gz
./cluso-server --port 8080

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/dd0wney/graphdb/releases/download/v0.1.0/graphdb_0.1.0_Windows_x86_64.zip" -OutFile "graphdb.zip"
Expand-Archive graphdb.zip
.\graphdb\cluso-server.exe --port 8080
```

### Building from Source

```bash
# Clone the repository
git clone https://github.com/dd0wney/graphdb
cd graphdb

# Build all binaries
go build -o bin/server ./cmd/server
go build -o bin/cli ./cmd/cli
go build -o bin/tui ./cmd/tui
go build -o bin/tui-demo ./cmd/tui-demo
```

> **Note on replication:**
> graphdb is single-node by design. The standalone replication binaries
> (`cmd/graphdb-{primary,replica}` + `nng` variants) and the
> `pkg/replication/` library were retired in A8.1 (PRs #129/#130/#133,
> 2026-05-12) because they pre-dated the multi-tenancy work and routed
> writes to the default tenant. Use `cmd/server` for any real
> deployment. Horizontal scale is a multi-quarter roadmap item — see
> `docs/internals/design/A8_1_SPIKE_2026-05-12.md` for the architectural decision and
> `docs/PRODUCTION_QUICKSTART.md` § "Scale Considerations" for the
> practical workarounds.

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

## Usage Examples

### Programmatic Usage

```go
package main

import (
    "github.com/dd0wney/cluso-graphdb/pkg/storage"
    "github.com/dd0wney/cluso-graphdb/pkg/algorithms"
    "github.com/dd0wney/cluso-graphdb/pkg/query"
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

    // Detect cycles in the graph
    cycles, _ := algorithms.DetectCycles(graph)
    stats := algorithms.AnalyzeCycles(cycles)
    fmt.Printf("Found %d cycles\n", stats.TotalCycles)

    // Quick check for cycles
    hasCycle, _ := algorithms.HasCycle(graph)
    fmt.Printf("Graph has cycles: %v\n", hasCycle)

    // Validate graph constraints
    validator := constraints.NewValidator()
    validator.AddConstraint(&constraints.PropertyConstraint{
        NodeLabel:    "Person",
        PropertyName: "age",
        Type:         storage.TypeInt,
        Required:     true,
    })
    validationResult, _ := validator.Validate(graph)
    fmt.Printf("Graph valid: %v\n", validationResult.Valid)

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

# Detect cycles in the graph
curl -X POST http://localhost:8080/algorithms \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "detect_cycles",
    "parameters": {
      "min_length": 2
    }
  }'

# Quick check if graph has any cycles
curl -X POST http://localhost:8080/algorithms \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "has_cycle"
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

### Vector Search Examples (New in v0.2.0!)

Vector search enables semantic similarity queries using HNSW indexes - perfect for AI/ML applications, recommendation systems, and semantic search.

```bash
# 1. Create a vector index for embeddings
curl -X POST http://localhost:8080/vector-indexes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "property_name": "embedding",
    "dimensions": 384,
    "metric": "cosine"
  }'

# 2. Create nodes with vector embeddings
curl -X POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["Document"],
    "properties": {
      "title": "Introduction to Graph Databases",
      "embedding": [0.1, 0.2, 0.3, ...]
    }
  }'

# 3. Perform semantic similarity search
curl -X POST http://localhost:8080/vector-search \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "property_name": "embedding",
    "query_vector": [0.15, 0.22, 0.31, ...],
    "k": 10,
    "include_nodes": true,
    "filter_labels": ["Document"]
  }'

# Response:
# {
#   "results": [
#     {"node_id": 123, "distance": 0.15, "score": 0.85, "node": {...}},
#     {"node_id": 456, "distance": 0.23, "score": 0.77, "node": {...}}
#   ],
#   "count": 10,
#   "time": "1.234ms"
# }

# List all vector indexes
curl -X GET http://localhost:8080/vector-indexes \
  -H "Authorization: Bearer $TOKEN"

# Delete a vector index
curl -X DELETE http://localhost:8080/vector-indexes/embedding \
  -H "Authorization: Bearer $TOKEN"
```

**Supported Distance Metrics:**

| Metric | Best For | Score Range |
|--------|----------|-------------|
| `cosine` | Text embeddings, normalized vectors | -1 to 1 |
| `euclidean` | Spatial data, image features | 0 to 1 |
| `dot_product` | Maximum inner product search | unbounded |

### Graph-Augmented Retrieval (`/v1/retrieve`)

`/v1/retrieve` composes hybrid search (FTS + LSA via RRF) with graph traversal to return ranked context chunks for LLM prompts. Each result includes a `source.path` array — the BFS path from the contributing seed — so downstream code can cite *why* a chunk is in context. Tenant-scoped end-to-end.

The endpoint shape matches LangChain's `BaseRetriever`, so it drops into existing RAG pipelines. See `examples/retrieve-curl.sh` and `examples/retrieve-langchain.py`.

```bash
curl -X POST http://localhost:8080/v1/retrieve \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "What does Alice know about graph databases?",
    "k": 10,
    "max_hops": 2,
    "max_tokens": 4096
  }'

# Response:
# {
#   "documents": [
#     {
#       "page_content": "Alice authored a paper on graph database internals...",
#       "metadata": {
#         "node_id": 42,
#         "score": 0.87,
#         "source": {
#           "node_id": 42,
#           "label": "Doc",
#           "path": [17, 23, 42]   // seed → Person Alice → this Doc
#         }
#       }
#     }
#   ],
#   "took_ms": 124
# }
```

**Request fields** (defaults in parentheses):

| Field | Type | Default | Notes |
|---|---|---|---|
| `query` | string | (required) | Free-text query, FTS + LSA seed retrieval |
| `k` | int | 10 | Max chunks returned (after token-budget drop) |
| `max_hops` | int | 2 | BFS depth from seeds; 0 = seeds only |
| `max_tokens` | int | 4096 | Token budget; chunks dropped whole, lowest score first |
| `alpha` | float | 0.7 | Weight on normalized seed score |
| `beta` | float | 0.3 | Weight on `exp(-d/τ)` graph-distance term |
| `tau` | float | 2.0 | Decay constant for distance falloff |
| `labels` | string[] | — | Optional seed-stage label filter |
| `include_node` | bool | false | Hydrate full node into `metadata.node` |

A hard 50-node cap prevents pathological expansion in dense graphs. See [`docs/internals/design/F2_GRAPHRAG_DESIGN.md`](docs/internals/design/F2_GRAPHRAG_DESIGN.md) for the design rationale and [`pkg/retrieval/retrieval_bench_test.go`](pkg/retrieval/retrieval_bench_test.go) for measured latency (p95=82µs on 1k nodes).

## Benchmarks

### Performance Results

**Capacity Test (5M nodes + 50M edges):**

Validated on GitHub Actions (AMD EPYC 7763, 16GB RAM):

| Metric | Performance | Notes |
|--------|-------------|-------|
| **Write Throughput** | 330K nodes/sec | Sustained rate over 5M nodes |
| **Read Throughput** | 1.5M lookups/sec | Random reads |
| **Cold Cache Latency** | 152µs average | Initial read |
| **Hot Cache Latency** | 38µs average | 4x speedup on cached data |
| **Memory Efficiency** | 15.31 bytes/node | Incredibly low overhead |
| **Total Memory (5M nodes)** | 73 MB | Including 50M edges |

**Unit Benchmarks (per operation):**

| Operation | Throughput | Latency |
|-----------|-----------|---------|
| CompressedEdgeList New | ~1.3M ops/sec | 4.6µs |
| EdgeStore CacheHit | ~9.4M ops/sec | 640ns |
| EdgeCache Hit/Miss | ~320M ops/sec | 19ns |
| Property Index Insert | ~30M ops/sec | 192ns |
| Property Index Lookup | ~14M ops/sec | 394ns |
| LSM Put | ~33M ops/sec | 179ns |
| LSM Get | ~122M ops/sec | 49ns |
| Node Creation | ~15K ops/sec | 401µs |
| Edge Creation | ~14K ops/sec | 413µs |
| GetNode | ~29M ops/sec | 207ns |
| Shortest Path | ~1K queries/sec | 10.25ms |
| PageRank (10K nodes) | Converges in <100ms | - |

**Storage Benchmarks:**

| Benchmark | Throughput | Latency |
|-----------|-----------|---------|
| Node Creation | 2,812 nodes/sec | 355µs |
| Edge Creation | 2,875 edges/sec | 347µs |
| Random Lookups | 1.5M lookups/sec | 0.65µs |
| Outgoing Edges Query | 449K queries/sec | 2.22µs |
| BFS Traversal | 22,845 traversals/sec | 0.04ms |

### Running Benchmarks

```bash
# Clone and build
git clone https://github.com/dd0wney/graphdb
cd graphdb

# Run all benchmarks
go test -bench=. ./...

# Run specific benchmark
go test -bench=BenchmarkGraphStorage ./pkg/storage

# With memory profiling
go test -bench=. -benchmem ./pkg/storage

# Run capacity test
cd pkg/storage
RUN_CAPACITY_TEST=1 go test -run Test5MNodeCapacity -v
```

## Project Structure

```
graphdb/
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

## Testing

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

# Run capacity test
cd pkg/storage
RUN_CAPACITY_TEST=1 go test -run Test5MNodeCapacity -v
```

## TUI Features

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

## Contributing

Contributions are welcome! Areas for improvement:

- Additional graph algorithms (community detection variants, centrality measures)
- Query language features (aggregations, subqueries, UNION)
- Storage optimizations (compression, tiered storage)
- Distributed consensus (Raft, Paxos)
- Web-based UI (React/Vue dashboard)
- Additional data types (geospatial, full-text search)

## License

GraphDB is released under the [MIT License](LICENSE).

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

For commercial licensing details, contact: [your-email]

## Acknowledgments

Built with:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [Mangos](https://go.nanomsg.org/mangos) - Pure-Go nanomsg/NNG implementation for replication transport

## Roadmap

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
- [x] v0.1.0 Release with multi-platform binaries
- [x] Docker Hub images
- [ ] Distributed consensus (Raft)
- [ ] Query optimizer
- [ ] Schema validation
- [ ] Full-text search
- [ ] Geospatial queries
- [ ] Web-based dashboard

## Why GraphDB?

Built for **BL Pay**, a next-generation payment platform that needed to:
- Map trust networks between users in real-time
- Detect fraud patterns across payment relationships
- Handle high-volume transaction graph updates
- Scale efficiently with minimal infrastructure

**Use cases perfect for GraphDB:**
- Fraud detection & prevention
- Social networks & trust graphs
- Payment network analysis
- Knowledge graphs for AI/LLM
- Recommendation engines
- IoT sensor networks
- Real-time analytics

---

**Built with Go**

For questions, issues, or feature requests, please open an [issue on GitHub](https://github.com/dd0wney/graphdb/issues).

**Try it now:** `docker run -p 8080:8080 dd0wney/graphdb:latest`
