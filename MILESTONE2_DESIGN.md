# Milestone 2: Disk-Backed Adjacency - Design Document

**Goal**: Scale from 2-3M nodes → 5M nodes on 32GB RAM by moving adjacency lists to disk

**Date**: 2025-11-14
**Approach**: Test-Driven Development (TDD)

---

## Current Architecture (Milestone 1)

### Memory Usage (for 5M nodes, avg degree 10):

```
Nodes: 5M × 2.4 KB = 12 GB
Edges: 50M × 1.1 KB = 55 GB
Adjacency lists (compressed): 50M × 8 bytes / 5.08 = 78 MB (compressed)
Adjacency lists (uncompressed): 50M × 8 bytes = 400 MB
```

**Total**: ~67-68 GB (exceeds 32GB limit)

### Current Storage:

```go
// In-memory storage
outgoingEdges map[uint64][]uint64       // 400 MB for 5M nodes
incomingEdges map[uint64][]uint64       // 400 MB for 5M nodes
compressedOutgoing map[uint64]*CompressedEdgeList  // 78 MB compressed
compressedIncoming map[uint64]*CompressedEdgeList  // 78 MB compressed
```

**Problem**: Even with compression, all edge lists are in RAM. Need to move to disk.

---

## Milestone 2 Architecture

### Strategy:

1. **Store adjacency lists in LSM** (disk-backed)
2. **LRU cache** for hot edge lists (keep frequently accessed in RAM)
3. **Lazy loading** (load from disk on demand)
4. **Write-through cache** (persist to LSM immediately)

### New Components:

#### 1. LSM-Backed Edge Store

```go
type EdgeStore struct {
    lsm *lsm.LSMStorage  // Existing LSM implementation
    cache *EdgeCache     // LRU cache for hot data
}

// Key format
"edges:out:{nodeID}" → CompressedEdgeList  // Outgoing edges
"edges:in:{nodeID}"  → CompressedEdgeList  // Incoming edges
```

#### 2. LRU Edge Cache

```go
type EdgeCache struct {
    maxSize int
    cache   map[string]*CacheEntry  // key → entry
    lru     *list.List              // Doubly-linked list for LRU
    mu      sync.RWMutex            // Thread-safe access
}

type CacheEntry struct {
    key    string
    value  *CompressedEdgeList
    size   int
    element *list.Element  // Position in LRU list
}
```

**Cache eviction policy**: Least Recently Used (LRU)
**Cache size**: Configurable (default 1000 edge lists = ~10-20MB)

#### 3. Lazy Loading

```go
func (es *EdgeStore) GetOutgoingEdges(nodeID uint64) ([]uint64, error) {
    key := fmt.Sprintf("edges:out:%d", nodeID)

    // 1. Check cache first
    if cached := es.cache.Get(key); cached != nil {
        return cached.Decompress(), nil
    }

    // 2. Load from LSM
    data, err := es.lsm.Get([]byte(key))
    if err != nil {
        return []uint64{}, nil  // No edges
    }

    // 3. Deserialize
    edgeList := DeserializeCompressedEdgeList(data)

    // 4. Add to cache
    es.cache.Put(key, edgeList)

    return edgeList.Decompress(), nil
}
```

---

## Memory Savings Calculation

### Before (Milestone 1):
```
5M nodes × 10 edges avg = 50M edges
Compressed edge lists: 78 MB (in RAM)
```

### After (Milestone 2):
```
50M edges total
Cache: 1000 hottest edge lists = ~10-20 MB (in RAM)
Rest: On disk in LSM

Memory reduction: 78 MB → 15 MB = 5.2x reduction
```

### With cache tuning:
```
Cache: 10,000 edge lists (top 0.2%) = ~100 MB
Still 80% memory savings
```

---

## TDD Implementation Plan

### Phase 1: EdgeStore with LSM Backend (Week 1)

**Tests to write first:**
1. ✅ `TestEdgeStore_StoreAndRetrieve` - Basic put/get
2. ✅ `TestEdgeStore_EmptyNode` - Node with no edges
3. ✅ `TestEdgeStore_LargeEdgeList` - 10,000 edges
4. ✅ `TestEdgeStore_ConcurrentAccess` - Thread safety
5. ✅ `TestEdgeStore_Persistence` - Survives restart

**Implementation steps:**
1. Create `pkg/storage/edgestore.go`
2. Implement `EdgeStore` with LSM backend
3. Serialization/deserialization for CompressedEdgeList
4. Integration with existing GraphStorage

### Phase 2: LRU Cache (Week 2)

**Tests to write first:**
1. ✅ `TestEdgeCache_BasicLRU` - Insert and evict
2. ✅ `TestEdgeCache_HitRate` - Cache effectiveness
3. ✅ `TestEdgeCache_Concurrent` - Thread-safe operations
4. ✅ `TestEdgeCache_MaxSize` - Eviction when full
5. ✅ `TestEdgeCache_MemoryTracking` - Size limits

**Implementation steps:**
1. Create `pkg/storage/edgecache.go`
2. Implement LRU eviction policy
3. Thread-safe access with RWMutex
4. Memory size tracking
5. Integration with EdgeStore

### Phase 3: Integration & Migration (Week 3)

**Tests to write first:**
1. ✅ `TestGraphStorage_DiskBacked` - End-to-end test
2. ✅ `TestGraphStorage_5MNodes` - 5M node capacity test
3. ✅ `TestGraphStorage_MemoryUsage` - Memory profiling
4. ✅ `TestGraphStorage_Performance` - Benchmark vs Milestone 1

**Implementation steps:**
1. Modify `GraphStorage` to use `EdgeStore`
2. Backward compatibility (optional in-memory mode)
3. Migration path for existing data
4. Performance tuning

### Phase 4: Benchmarks & Validation (Week 4)

**Benchmarks to create:**
1. ✅ `BenchmarkEdgeStore_Get` - Disk read performance
2. ✅ `BenchmarkEdgeStore_CacheHit` - Cache performance
3. ✅ `BenchmarkEdgeStore_CacheMiss` - Disk latency
4. ✅ `BenchmarkGraphStorage_5M` - Full 5M node test
5. ✅ `BenchmarkMemoryFootprint` - RAM usage validation

---

## Success Criteria

### Performance Targets:

| Metric | Target | Validation Method |
|--------|--------|-------------------|
| **Memory (5M nodes)** | < 25 GB | Memory profiling |
| **Cache hit rate** | > 80% | Benchmark logging |
| **Get latency (cache hit)** | < 500 ns | Benchmark |
| **Get latency (cache miss)** | < 100 μs | Benchmark (SSD) |
| **Thread safety** | No races | Race detector |

### Capacity Targets:

- ✅ Handle 5M nodes on 32GB RAM machine
- ✅ Handle 50M edges with avg degree 10
- ✅ Graceful degradation (cache tunable)

---

## API Changes (Minimal)

### Config Addition:

```go
type StorageConfig struct {
    // ... existing fields ...

    // New in Milestone 2
    EnableDiskBackedEdges bool   // Enable disk-backed adjacency
    EdgeCacheSize         int    // Max edge lists in cache (default 1000)
    EdgeStorePath         string // Path for edge LSM (default: dataDir/edges)
}
```

### Backward Compatibility:

- If `EnableDiskBackedEdges = false`, use existing in-memory implementation
- Migration tool to convert existing graphs to disk-backed format
- No breaking changes to public API

---

## File Structure

```
pkg/storage/
├── edgestore.go          # NEW - LSM-backed edge storage
├── edgestore_test.go     # NEW - TDD tests for EdgeStore
├── edgecache.go          # NEW - LRU cache implementation
├── edgecache_test.go     # NEW - TDD tests for cache
├── storage.go            # MODIFIED - Integration
├── storage_test.go       # MODIFIED - Add integration tests
└── compression.go        # EXISTING - Reuse CompressedEdgeList
```

---

## Risks & Mitigation

### Risk 1: Disk I/O Latency

**Impact**: Cache misses could slow queries by 100-1000x
**Mitigation**:
- Aggressive caching (tunable size)
- Prefetching (load neighboring nodes)
- SSD requirement for production

### Risk 2: Cache Thrashing

**Impact**: Poor cache hit rate if workload is random
**Mitigation**:
- Benchmark with realistic workloads
- Adaptive cache sizing
- Query pattern analysis

### Risk 3: Complexity

**Impact**: More moving parts = more bugs
**Mitigation**:
- TDD approach (tests first!)
- Comprehensive test coverage
- Gradual rollout (optional feature)

---

## Next Steps (TDD)

1. **Start with EdgeStore tests** (Phase 1)
2. **Watch tests fail** (no implementation yet)
3. **Implement EdgeStore** to make tests pass
4. **Refactor** and optimize
5. **Repeat** for EdgeCache (Phase 2)

**First Test to Write**: `TestEdgeStore_StoreAndRetrieve`

Let's begin! 🚀
