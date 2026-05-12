# S1 Storage Interface Extraction — Spike Design Doc

**Status**: IMPLEMENTED (2026-05-12)
**Date**: 2026-05-12
**Goal**: Design a pluggable `Storage` interface for GraphDB to support multiple backends and extensibility.

## Summary of Implementation
The `Storage` and `StorageReader` interfaces have been extracted into `pkg/storage/interface.go`. All major consumers (algorithms, query engine, API, GraphQL, and various utilities/commands) have been refactored to depend on these interfaces instead of the concrete `*storage.GraphStorage` implementation.

Furthermore, a second backend, **`BTreeGraphStorage`** (S2 spike), has been implemented using a custom B+Tree engine (`pkg/btree`). This proves the extensibility of the interface. The `Server` now supports choosing the backend via the `GRAPHDB_STORAGE_BACKEND` environment variable. Performance benchmarks quantify the trade-offs between memory-first and persistent-first backends.

## 1. Context

The current storage implementation (`pkg/storage`) is a concrete implementation (`GraphStorage`) that is used directly across the codebase. While it is highly optimized (partitioned shard locks, in-memory with WAL), its direct coupling makes it difficult to:
1. Swap the storage engine for different use cases (e.g., persistent-only, distributed, or memory-mapped).
2. Implement storage plugins or extensions.
3. Decouple the query engine and algorithms from the underlying storage layout.
4. Support the future replication rebuild (A8.1 follow-up) with a transport-agnostic interface.

## 2. Research Findings

### 2.1 Storage Surface Area
The `GraphStorage` struct currently exposes ~60-80 public methods. These can be grouped into:

- **Node Operations**: `CreateNode`, `GetNode`, `UpdateNode`, `DeleteNode`, `GetAllNodes`, `GetNodesByLabel` (and their `*ForTenant` variants).
- **Edge Operations**: `CreateEdge`, `GetEdge`, `UpdateEdge`, `DeleteEdge`, `UpsertEdge`, `FindEdgeBetween` (and `*ForTenant` variants).
- **Indexing**: `CreatePropertyIndex`, `DropPropertyIndex`, `FindNodesByProperty`, `FindNodesByPropertyPrefix`, `VectorSearch`.
- **Query/Traversal**: `GetOutgoingEdges`, `GetIncomingEdges`, `ForEachNode`, `GetAllLabels`.
- **Persistence/Admin**: `Snapshot`, `Close`, `GetStatistics`, `replayWAL`.

### 2.2 Consumers
- **API Handlers (`pkg/api`)**: Use tenant-scoped operations for REST endpoints.
- **GraphQL Resolvers (`pkg/graphql`)**: Use tenant-scoped lookups, label queries, and mutations.
- **Query Engine (`pkg/query`)**: Use index lookups, node scans (`GetAllNodesForTenant`), and relationship traversal.
- **Algorithms (`pkg/algorithms`)**: Use traversal methods, adjacency lists, and property access.
- **Parallel Traverser (`pkg/parallel`)**: Specifically optimized for the current `GraphStorage` structure.

## 3. Proposed Interface Architecture

Research into `pkg/algorithms/view.go` shows an existing `graphView` interface that already abstracts tenant-blind vs. tenant-scoped access. This pattern should be elevated to the primary `pkg/storage` interface.

### 3.1 `StorageReader` (Data Retrieval)
The reader focuses on fetching nodes, edges, and index data.

```go
type StorageReader interface {
    GetNode(id uint64, tenantID string) (*Node, error)
    GetEdge(id uint64, tenantID string) (*Edge, error)
    
    GetNodesByLabel(tenantID string, label string) ([]*Node, error)
    GetEdgesByType(tenantID string, edgeType string) ([]*Edge, error)
    
    GetOutgoingEdges(id uint64, tenantID string) ([]*Edge, error)
    GetIncomingEdges(id uint64, tenantID string) ([]*Edge, error)
    
    GetAllLabels() []string
    GetLabelsForTenant(tenantID string) []string
    
    // Indexing
    FindNodesByProperty(key string, value Value, tenantID string) ([]*Node, error)
    VectorSearch(tenantID string, vector []float32, limit int) ([]VectorSearchResult, error)
}
```

### 3.2 `StorageWriter` (Mutations)
The writer handles all state changes.

```go
type StorageWriter interface {
    CreateNode(tenantID string, labels []string, props map[string]Value) (*Node, error)
    UpdateNode(id uint64, tenantID string, props map[string]Value) error
    DeleteNode(id uint64, tenantID string) error
    
    CreateEdge(tenantID string, from, to uint64, edgeType string, props map[string]Value, weight float64) (*Edge, error)
    UpdateEdge(id uint64, tenantID string, props map[string]Value, weight *float64) error
    DeleteEdge(id uint64, tenantID string) error
    
    // Transactions
    Begin() (Transaction, error)
}
```

### 3.3 `Transaction`
```go
type Transaction interface {
    StorageReader
    StorageWriter
    Commit() error
    Rollback() error
}
```

## 4. Migration Plan & Strategy

### 4.1 Step 1: Interface Extraction
1. Create `pkg/storage/interface.go`.
2. Move `Node`, `Edge`, `Value`, and other shared types to a new `pkg/storage/types.go` if needed (currently they are already in `storage_types.go` and `types.go`).
3. Make `GraphStorage` implement the new `Storage` interface.

### 4.2 Step 2: Algorithm Migration
Update `pkg/algorithms/view.go` to use the new `StorageReader` interface instead of the private `graphView`. This is a low-risk starting point as the interface shape is nearly identical.

### 4.3 Step 3: Query Engine Migration
Update `pkg/query/executor.go` to take a `Storage` interface instead of `*storage.GraphStorage`. This will require updating all `executor_steps.go` to use interface methods.

### 4.4 Step 4: API and GraphQL Migration
Update `pkg/api` and `pkg/graphql` to depend on the interface. This will involve updating the `Server` struct and schema generators.

## 5. Performance & Constraints

### 5.1 Clone Elision
Methods like `WithNodeRefForTenant` (which avoids `Clone()` calls) are critical for performance in vector search post-filtering. These must be preserved in the interface, likely using a callback pattern:

```go
WithNodeRef(id uint64, tenantID string, fn func(*Node) error) error
```

### 5.2 Locking & Concurrency
The interface should not dictate the locking model. `GraphStorage` uses shard-level locks, but a different implementation (e.g., BoltDB or Pebble) might use different primitives. The `Transaction` interface will be the primary way to manage cross-operation consistency.

### 5.3 Multi-Tenancy
The interface explicitly includes `tenantID` in all methods. This ensures that every storage implementation is forced to handle tenancy as a first-class citizen, maintaining the security posture established in Audit Tracks A1-A9.

## 6. Recommendations

1. **Go for S1**: The extraction is highly feasible and has a clear path forward starting with `pkg/algorithms`.
2. **Standardize on `EffectiveTenantID`**: Ensure all interface implementations use the `tenantid` package for consistent handling of the "default" tenant.
3. **Benchmarking**: Before merging the full migration, run `BenchmarkVectorSearch` and `BenchmarkShortestPath` to quantify the interface overhead.

