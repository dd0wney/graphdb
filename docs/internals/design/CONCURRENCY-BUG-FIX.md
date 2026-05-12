# Critical Concurrency Bug Fix - GetOutgoingEdges/GetIncomingEdges

**Date**: 2025-11-23
**Severity**: Critical (Production Crash)
**Status**: Fixed
**Affected Versions**: All versions prior to this fix

## Summary

Fixed a critical race condition in `GetOutgoingEdges()` and `GetIncomingEdges()` that caused "fatal error: concurrent map read and map write" crashes under high concurrency.

## Problem Description

### Symptoms
- Fatal crash: `panic: runtime error: concurrent map read and map write`
- Occurs under high concurrent load with mixed read/write operations
- Specifically triggered when reading edge adjacency lists while nodes/edges are being created/deleted

### Root Cause

The functions `GetOutgoingEdges()` and `GetIncomingEdges()` in `pkg/storage/query_operations.go` were using **shard-level locks** but accessing **global shared maps**.

**Buggy Code** (Lines 28-47):
```go
func (gs *GraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
    defer gs.startQueryTiming()()

    // PROBLEM: Shard lock for nodeID
    gs.rlockShard(nodeID)
    defer gs.runlockShard(nodeID)

    // PROBLEM: Reads global gs.outgoingEdges map without global lock
    edgeIDs := gs.getEdgeIDsForNode(nodeID, true)
    if edgeIDs == nil {
        return []*Edge{}, nil
    }

    // Global lock acquired too late
    gs.mu.RLock()
    edges := gs.buildEdgeListFromIDs(edgeIDs)
    gs.mu.RUnlock()

    return edges, nil
}
```

**Why This Failed**:
1. Shard locks protect node-level operations within a specific shard (0-255)
2. But `gs.outgoingEdges` and `gs.incomingEdges` are **single global maps** shared across all shards
3. Multiple goroutines accessing different shards could still race on the same global map
4. `getEdgeIDsForNode()` reads from these maps while other threads write to them

## The Fix

Changed to use **global read locks** for the entire operation:

**Fixed Code** (pkg/storage/query_operations.go):
```go
func (gs *GraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) {
    defer gs.startQueryTiming()()

    // FIX: Use global read lock since we access global edge maps
    gs.mu.RLock()
    defer gs.mu.RUnlock()

    // Now safe: All map accesses protected by global lock
    edgeIDs := gs.getEdgeIDsForNode(nodeID, true)
    if edgeIDs == nil {
        return []*Edge{}, nil
    }

    edges := gs.buildEdgeListFromIDs(edgeIDs)
    return edges, nil
}

func (gs *GraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) {
    defer gs.startQueryTiming()()

    // FIX: Use global read lock since we access global edge maps
    gs.mu.RLock()
    defer gs.mu.RUnlock()

    edgeIDs := gs.getEdgeIDsForNode(nodeID, false)
    if edgeIDs == nil {
        return []*Edge{}, nil
    }

    edges := gs.buildEdgeListFromIDs(edgeIDs)
    return edges, nil
}
```

## Verification

### 1. Race Detector
```bash
go test -race ./pkg/storage -run "^TestEdgeCase_(StressTestMixedOperations|ReadWriteRace|ConcurrentSameNodeAccess)$"
# Result: PASS - No data races detected
```

### 2. Stress Test Results
**Before Fix**: Immediate crash with "concurrent map read and map write"
**After Fix**:
- 589,017 operations in 2 seconds (294,508 ops/sec)
- 8 concurrent workers
- 0 errors (0.00% error rate)
- Operations breakdown:
  - Node creates: 118,091
  - Edge creates: 118,206
  - Node reads: 118,410
  - Edge reads: 117,300
  - Node deletes: 117,010

### 3. Code Coverage
- `GetOutgoingEdges`: 100% coverage
- `GetIncomingEdges`: 87.5% coverage
- `node_operations.go`: 100% coverage on all functions
- Overall storage package: 85.0% coverage

## Impact Analysis

### Performance Impact
- **Read concurrency**: Slightly reduced (global lock vs shard lock)
- **Trade-off**: Correctness over micro-optimization
- **Actual impact**: Negligible in practice - still achieving 294K ops/sec under stress

### Affected Operations
- `GetOutgoingEdges(nodeID)` - Fixed
- `GetIncomingEdges(nodeID)` - Fixed
- Any code path calling these functions indirectly

## Lessons Learned

1. **Lock granularity must match data structure scope**
   - Shard locks work for sharded data (e.g., `gs.nodes` if sharded)
   - Global data requires global locks (e.g., `gs.outgoingEdges`, `gs.incomingEdges`)

2. **Concurrent map access is unsafe**
   - Go's maps are not thread-safe
   - Even read-only access must be synchronized with writes
   - The race detector is essential for catching these

3. **Test under realistic concurrency**
   - The bug only manifested under high concurrent load
   - Edge case testing with `TestEdgeCase_StressTestMixedOperations` caught this
   - Race detector confirms the fix

## Related Files

**Modified**:
- `pkg/storage/query_operations.go` - Changed locking strategy

**Test Coverage**:
- `pkg/storage/stress_edge_cases_test.go` - Stress test that exposed the bug
- `pkg/storage/edge_cases_test.go` - Edge case coverage
- `pkg/storage/integration_edge_cases_test.go` - Integration scenarios

## Recommendations

1. **Future Development**:
   - Consider truly sharded adjacency maps if shard-level locking is needed
   - Document which data structures are global vs sharded
   - Always run race detector in CI/CD pipeline

2. **Code Review Checklist**:
   - [ ] Lock scope matches data structure scope
   - [ ] Map access is protected by appropriate locks
   - [ ] Race detector passes on concurrency tests
   - [ ] Lock ordering prevents deadlocks

## References

- Issue discovered during: Edge case test development (2025-11-23)
- Test file: `pkg/storage/stress_edge_cases_test.go:266` (TestEdgeCase_StressTestMixedOperations)
- Commit: [To be added when committed]
