# Missing Tests for Milestone 1 Validation

## Priority 1: Query Statistics Tests (CRITICAL)

### Test: TrackQueryTime Functionality

**File**: `pkg/query/executor_test.go` (add to existing file)
**Purpose**: Verify query timing is tracked

```go
// TestQueryStatistics_TrackQueryTime verifies trackQueryTime() is called
func TestQueryStatistics_TrackQueryTime(t *testing.T) {
    // Should verify that query execution time is recorded
    // Before: stats.TotalQueries = 0
    // After: stats.TotalQueries = 1, stats.AvgQueryTime > 0
}

// TestQueryStatistics_TotalQueriesIncrement verifies counter increments
func TestQueryStatistics_TotalQueriesIncrement(t *testing.T) {
    // Execute multiple queries
    // Verify: stats.TotalQueries increments for each query
}

// TestQueryStatistics_AvgQueryTimeCalculation verifies average time
func TestQueryStatistics_AvgQueryTimeCalculation(t *testing.T) {
    // Execute queries with known delays
    // Verify: AvgQueryTime = sum(times) / count
}

// TestQueryStatistics_Concurrent verifies thread-safety
func TestQueryStatistics_Concurrent(t *testing.T) {
    // Execute queries concurrently from multiple goroutines
    // Verify: TotalQueries and AvgQueryTime are accurate
    // Verify: No data races (run with -race flag)
}

// BenchmarkQueryStatistics_Overhead measures tracking overhead
func BenchmarkQueryStatistics_Overhead(b *testing.B) {
    // Compare query execution time with/without tracking
    // Measure overhead of statistics collection
}
```

**What to verify**:

- [ ] TotalQueries increments after each query
- [ ] AvgQueryTime is calculated correctly
- [ ] Statistics are thread-safe (no race conditions)
- [ ] Query statistics don't corrupt query results
- [ ] Stats persist across multiple queries

---

## Priority 2: Sharded Locking Benchmarks (IMPORTANT)

### Test: Sharded Locking Performance

**File**: `pkg/storage/storage_test.go` (create new section)
**Purpose**: Validate 100x concurrency improvement claim

```go
// BenchmarkShardedLocking_vs_GlobalLock compares sharded vs global locking
func BenchmarkShardedLocking_vs_GlobalLock(b *testing.B) {
    // Create two storage instances:
    // 1. With sharded locks (current implementation)
    // 2. With global lock only (for comparison)
    //
    // Benchmark concurrent node creation:
    // - 100 concurrent goroutines
    // - Each creates 100 nodes
    // - Compare throughput: sharded should be ~100x faster
    //
    // Report:
    // - Operations per second (both implementations)
    // - Improvement ratio
    // - Lock contention metrics
}

// BenchmarkHighConcurrency_HighContention tests heavy contention scenario
func BenchmarkHighConcurrency_HighContention(b *testing.B) {
    // 256+ goroutines (more than shards)
    // All accessing same node/edges frequently
    // Measure throughput with high contention
}

// TestShardLockDistribution verifies even load distribution
func TestShardLockDistribution(t *testing.T) {
    // Create nodes with IDs that hash to different shards
    // Verify shard distribution is even
    // Ensure no "hot" shards with all contention
}

// BenchmarkConcurrentNodeCreation benchmarks typical workload
func BenchmarkConcurrentNodeCreation(b *testing.B) {
    // Multiple goroutines creating nodes concurrently
    // Measure throughput with varying goroutine counts:
    // - 10, 50, 100, 256 goroutines
    // Expect throughput to improve with sharding up to ~256
}

// TestShardLockContentionMetrics measures contention
func TestShardLockContentionMetrics(t *testing.T) {
    // Run concurrent operations
    // Measure lock wait times
    // Verify wait times are much lower than global lock
}
```

**What to verify**:

- [ ] Sharded locking is ~100x faster than global locking
- [ ] Performance scales with number of goroutines
- [ ] Load is distributed across shards
- [ ] No contention hotspots
- [ ] High goroutine counts (100+) work efficiently

---

## Priority 3: Cache Statistics Enhancements (NICE TO HAVE)

### Test: Cache Hit Ratio Improvement

**File**: `pkg/lsm/cache_test.go` (add to existing file)
**Purpose**: Validate cache improves read performance

```go
// BenchmarkCache_HitVsMiss measures performance difference
func BenchmarkCache_HitVsMiss(b *testing.B) {
    // Populate cache with known keys
    // Benchmark:
    // 1. Repeated accesses (hits) - should be very fast
    // 2. New accesses (misses) - should be slower
    // Report hit/miss ratio and latency difference
}

// TestCache_HitRateImprovement verifies hit rate over time
func TestCache_HitRateImprovement(t *testing.T) {
    // Simulate typical workload:
    // - 80% repeated keys (locality of reference)
    // - 20% new keys
    // Verify hit rate >= 80%
}
```

---

## Priority 4: Integration Tests (NICE TO HAVE)

### Test: End-to-End Milestone 1 Features

**File**: `pkg/integration/milestone1_test.go` (new file)
**Purpose**: Verify all features work together

```go
// TestMilestone1_AllFeatures_Integration tests all features
func TestMilestone1_AllFeatures_Integration(t *testing.T) {
    // 1. Create graph with edge compression enabled
    // 2. Create 1000 nodes with ~20 edges each
    // 3. Verify compression ratio >= 2.0 (conservative estimate)
    // 4. Execute concurrent queries (50+ goroutines)
    // 5. Verify query statistics are tracked
    // 6. Verify cache hit rates > 70%
    // 7. Run with -race flag
    // 8. Verify no crashes or data races
}
```

---

## Test Data and Fixtures

### Create helper functions

**File**: `pkg/query/executor_test.go`

```go
// createQueryWithDelay creates a query that takes measurable time
func createQueryWithDelay(duration time.Duration) *Query {
    // Use a query that will take predictable amount of time
}

// measureQueryExecutionTime runs query and returns time elapsed
func measureQueryExecutionTime(executor *Executor, query *Query) time.Duration {
    start := time.Now()
    executor.Execute(query)
    return time.Since(start)
}
```

---

## Test Execution Plan

### Phase 1: Query Statistics (Immediate)

```bash
# Add these 4 tests to pkg/query/executor_test.go
go test -v ./pkg/query -run QueryStatistics
go test -race ./pkg/query -run QueryStatistics
```

### Phase 2: Sharded Locking (This week)

```bash
# Add benchmark to pkg/storage/storage_test.go
go test -bench=Sharded -benchmem ./pkg/storage
go test -bench=HighConcurrency -benchmem ./pkg/storage
```

### Phase 3: Integration Tests (Next)

```bash
# Add integration test
go test -v -race ./pkg/integration -run Milestone1
```

### Phase 4: Full Validation (Final)

```bash
# Run comprehensive test suite
go test -v -race ./... -run "Compress|Cache|Statistics|Sharded"
go test -bench=. -benchmem ./pkg/storage ./pkg/lsm
```

---

## Success Criteria

| Test | Success Criteria | Evidence |
|------|------------------|----------|
| Query Statistics | TotalQueries increments, AvgQueryTime calculated | Test passes with assertions |
| Sharded Locking | 100x faster than global lock with 100+ goroutines | Benchmark shows throughput ratio |
| Cache Hit/Miss | Hit rate > 70% with typical workload | Cache statistics test passes |
| Integration | All features work together, no races | Integration test passes with -race |

---

## Estimated Effort

| Test | Complexity | Time | Priority |
|------|-----------|------|----------|
| Query Statistics | Low | 1-2 hours | CRITICAL |
| Sharded Locking Benchmark | Medium | 2-3 hours | HIGH |
| Cache Enhancement | Low | 1 hour | MEDIUM |
| Integration | Medium | 1-2 hours | MEDIUM |

**Total Estimated Time**: 5-8 hours to complete all missing tests

---

## Files to Modify/Create

1. **Modify**: `pkg/query/executor_test.go` - Add 4 query statistics tests
2. **Modify**: `pkg/storage/storage_test.go` - Add 5 sharded locking benchmarks
3. **Modify**: `pkg/lsm/cache_test.go` - Add 2 cache performance tests
4. **Create**: `pkg/integration/milestone1_test.go` - Add integration test

---

## Validation Checklist

- [ ] Query statistics tests pass
- [ ] Query statistics tests pass with -race flag
- [ ] Sharded locking benchmarks show ~100x improvement
- [ ] Cache hit rate > 70% with typical workload
- [ ] All integration tests pass
- [ ] No new race conditions detected
- [ ] All benchmarks complete without panics
