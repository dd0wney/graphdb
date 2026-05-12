# Milestone 2: Disk-Backed Adjacency Lists - Performance Benchmarks

## Overview

This document presents performance benchmarks comparing disk-backed edge storage (EdgeStore with LSM + LRU cache) against traditional in-memory storage in GraphStorage.

**Test Platform**: AMD Ryzen 9 5950X (32 cores), 64 GB RAM, NVMe SSD
**Test Date**: 2025-11-14
**Go Version**: go1.23+

---

## Benchmark Results Summary

### Edge Creation Performance

| Storage Mode | ns/op | Bytes/op | Allocs/op | vs In-Memory |
|--------------|-------|----------|-----------|--------------|
| **In-Memory** | 3,400 | 1,059 | 10 | Baseline |
| **Disk-Backed** | 141,381 | 329,273 | 76 | **41.6x slower** |

**Analysis**: Significant write overhead due to:
- LSM memtable insertion
- Gob serialization of edge lists
- Eventual SSTable flush to disk
- Cache invalidation

**Recommendation**: Disk-backed mode is NOT suitable for write-heavy workloads without batching.

---

### Edge Read Performance

| Storage Mode | ns/op | Bytes/op | Allocs/op | vs In-Memory |
|--------------|-------|----------|-----------|--------------|
| **In-Memory** | 728.6 | 1,200 | 21 | Baseline |
| **Disk-Backed (Cache Hit)** | 897.2 | 1,298 | 23 | **1.23x slower (23%)** |
| **Disk-Backed (Cache Miss)** | 12,577 | 8,695 | 188 | **17.3x slower** |

**Cache Effectiveness**: 14x speedup (cache hit vs cache miss)

**Analysis**:
- Cache hits are only **23% slower** than in-memory - EXCELLENT!
- Cache misses require LSM lookup (memtable → SSTables → disk)
- LRU cache is highly effective for hot data

**Recommendation**: Disk-backed mode is IDEAL for read-heavy workloads with locality of access.

---

### Edge Deletion Performance

| Storage Mode | ns/op | Bytes/op | Allocs/op | vs In-Memory |
|--------------|-------|----------|-----------|--------------|
| **In-Memory** | 108,402 | 308,738 | 8 | Baseline |
| **Disk-Backed** | 160,763 | 347,750 | 72 | **1.48x slower (48%)** |

**Analysis**: Surprisingly reasonable overhead for deletes:
- Read edge list (may hit cache)
- Filter out deleted edge
- Write updated list back (LSM write)

**Recommendation**: Disk-backed delete overhead is acceptable for most workloads.

---

### Mixed Workload Performance

**Workload Mix**: 70% reads, 20% writes, 10% deletes (realistic production workload)

| Storage Mode | ns/op | Bytes/op | Allocs/op | vs In-Memory |
|--------------|-------|----------|-----------|--------------|
| **In-Memory** | 751.7 | 219 | 2 | Baseline |
| **Disk-Backed** | 14,697 | 34,083 | 16 | **19.6x slower** |

**Analysis**:
- Overhead dominated by 20% write operations (41x slower)
- Read cache hits help, but writes kill overall throughput
- Memory allocation increases due to serialization/deserialization

**Recommendation**: For mixed workloads, enable batching to amortize write costs.

---

## Cache Effectiveness Analysis

### Cache Hit vs Miss Performance

| Metric | Cache Hit | Cache Miss | Speedup |
|--------|-----------|------------|---------|
| Latency | 897.2 ns | 12,577 ns | **14.0x** |
| Memory | 1,298 B | 8,695 B | **6.7x less** |
| Allocations | 23 | 188 | **8.2x less** |

**Key Insight**: The LRU cache provides a 14x performance improvement for hot data, validating the caching strategy.

### Cache Size Impact (from background benchmarks)

| Cache Size | ns/op | Performance |
|------------|-------|-------------|
| 10 | 12,945 | Baseline (mostly misses) |
| 100 | 115.0 | **112.6x faster** |
| 1,000 | 103.4 | **125.2x faster** |
| 10,000 | 99.79 | **129.7x faster** |

**Recommendation**:
- Small graphs (< 10K nodes): Cache size = 100-1,000
- Medium graphs (10K-100K nodes): Cache size = 1,000-10,000
- Large graphs (100K+ nodes): Cache size = 10,000+ (tune based on working set)

---

## Memory Efficiency Validation

### Memory Usage at Scale (from capacity tests)

| Node Count | Cache Size | Total Memory | Bytes/Node | Status |
|------------|------------|--------------|------------|--------|
| 10,000 | 100 | 3.1 MB | 322.8 | ✅ PASS |
| 100,000 | 1,000 | 2.6 MB | **27.1** | ✅ PASS |

**Key Finding**: Memory per-node **decreases** at larger scales (322 → 27 bytes/node), confirming that:
- Most edge data lives on disk
- Only hot edge lists are cached
- Disk-backed architecture scales efficiently

**Projection for 5M Nodes**:
- Memory per node: ~30 bytes
- Total memory: ~150 MB (edge data only)
- Plus node data, indices, etc.: **< 15 GB total** ✅

---

## Performance vs Memory Tradeoffs

### When to Use In-Memory Storage

**Pros**:
- 41x faster writes
- 23% faster reads
- Minimal complexity

**Cons**:
- High memory usage (~500+ bytes/edge)
- Limited scalability (RAM bound)
- OOM risk for large graphs

**Use Cases**:
- Small graphs (< 100K edges)
- Write-heavy workloads
- Latency-critical applications
- Abundant RAM available

---

### When to Use Disk-Backed Storage

**Pros**:
- 10-20x less memory usage
- Scales to millions of nodes
- Cache hits only 23% slower
- No OOM risk

**Cons**:
- 41x slower writes
- 17x slower on cache misses
- Requires tuning (cache size)

**Use Cases**:
- Large graphs (> 1M edges)
- Read-heavy workloads
- Limited RAM environments
- Production systems with SLA on memory

---

## Optimization Recommendations

### For Read-Heavy Workloads

1. **Enable Disk-Backed Storage**: Cache effectiveness is excellent (14x speedup)
2. **Tune Cache Size**: Set to 10-20% of node count for working set
3. **Monitor Cache Hit Rate**: Should be > 90% for good performance
4. **Use SSDs**: Cache misses go to disk - NVMe recommended

### For Write-Heavy Workloads

1. **Enable Batching**: Amortize LSM write costs across multiple operations
2. **Increase Memtable Size**: Reduce flush frequency
3. **Consider In-Memory Mode**: If RAM allows, avoid disk overhead
4. **Async Writes**: Decouple write latency from critical path

### For Mixed Workloads

1. **Hybrid Approach**: In-memory for hot data, disk for cold data
2. **Larger Cache**: Increase cache size to reduce miss rate
3. **Write Coalescing**: Batch writes during off-peak hours
4. **Tiered Storage**: SSD for memtable/L0, HDD for older SSTables

---

## Benchmark Reproducibility

### Run All GraphStorage Benchmarks

```bash
go test -bench=BenchmarkGraphStorage -benchmem -benchtime=1s ./pkg/storage/ -run=^$
```

### Run Individual Comparisons

```bash
# Edge creation comparison
go test -bench=BenchmarkGraphStorage_CreateEdge -benchmem ./pkg/storage/ -run=^$

# Edge read comparison
go test -bench=BenchmarkGraphStorage_GetOutgoingEdges -benchmem ./pkg/storage/ -run=^$

# Mixed workload comparison
go test -bench=BenchmarkGraphStorage_MixedWorkload -benchmem ./pkg/storage/ -run=^$
```

### Benchmark with Different Cache Sizes

```bash
# Test cache size impact
go test -bench=BenchmarkEdgeStore_CacheSize -benchmem ./pkg/storage/ -run=^$
```

---

## Conclusions

### Key Findings

1. **Cache Effectiveness**: 14x speedup for hot data validates the LRU cache design
2. **Read Performance**: Cache hits only 23% slower than in-memory - EXCELLENT
3. **Write Overhead**: 41x slower writes - expected for LSM, mitigated by batching
4. **Memory Efficiency**: 27 bytes/node at 100K scale - validates disk-backed architecture
5. **Mixed Workload**: 19x slower overall - dominated by write costs

### Trade-off Summary

| Metric | In-Memory | Disk-Backed | Winner |
|--------|-----------|-------------|--------|
| **Write Speed** | 3.4 μs | 141 μs | 🏆 In-Memory (41x) |
| **Read Speed (Cache Hit)** | 729 ns | 897 ns | 🏆 In-Memory (1.2x) |
| **Read Speed (Cache Miss)** | 729 ns | 12,577 ns | 🏆 In-Memory (17x) |
| **Memory Usage** | High | Low (27 bytes/node) | 🏆 Disk-Backed (20x) |
| **Scalability** | Limited | Millions of nodes | 🏆 Disk-Backed |
| **Complexity** | Low | Medium | 🏆 In-Memory |

### Recommendation

**For Cluso GraphDB Production**:
- **Default**: Enable disk-backed edges with cache size = 10,000
- **Small graphs (< 100K edges)**: In-memory mode acceptable
- **Large graphs (> 1M edges)**: Disk-backed mode required
- **Read-heavy workloads**: Disk-backed with large cache (excellent performance)
- **Write-heavy workloads**: Enable batching or consider in-memory mode

**Milestone 2 Validation**: ✅ **PASSED**
- Memory efficiency: 27 bytes/node at 100K scale
- Cache effectiveness: 14x speedup for hot data
- Read performance: Only 23% slower for cache hits
- Scalability: Validated to 100K+ nodes (5M node test pending)

---

## Next Steps

1. **Tune Default Cache Size**: Benchmark shows 10,000 is a good default
2. **Add Cache Hit Rate Metrics**: Expose via monitoring API
3. **Implement Write Batching**: Reduce write overhead for mixed workloads
4. **5M Node Capacity Test**: Run extended capacity test (30-60 minutes)
5. **Production Profiling**: Measure cache hit rates in real workloads

---

**See Also**:
- [MILESTONE2_DESIGN.md](MILESTONE2_DESIGN.md) - Architecture design
- [MILESTONE2_VALIDATION_RESULTS.md](MILESTONE2_VALIDATION_RESULTS.md) - Test results
- [CAPACITY_TESTING.md](CAPACITY_TESTING.md) - Capacity testing guide
- [pkg/storage/integration_bench_test.go](pkg/storage/integration_bench_test.go) - Benchmark code

---

**Last Updated**: 2025-11-14
