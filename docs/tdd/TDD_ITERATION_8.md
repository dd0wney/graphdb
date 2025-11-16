# TDD Iteration 8: Snapshot and WAL Truncation

## Overview

**Date**: TDD Iteration 8
**Focus**: Testing snapshot durability and WAL truncation after clean shutdown
**Test File**: `pkg/storage/integration_wal_snapshot_test.go` (469 lines)
**Result**: **CRITICAL BUG FOUND** - Property indexes lost after clean shutdown

## Test Strategy

After testing crash recovery scenarios in iterations 1-7, iteration 8 focused on the **clean shutdown path**: creating snapshots and truncating WAL.

The `Close()` method:
1. Creates a snapshot of all graph state
2. Truncates the WAL (clears it)
3. Closes the WAL

The critical question: Does everything get saved in the snapshot? Can we recover from snapshot alone after WAL truncation?

### Test Cases Created

1. **TestGraphStorage_SnapshotAndTruncate** (87 lines)
   - Creates 5 nodes
   - Closes cleanly (snapshot + WAL truncate)
   - Recovers from snapshot alone (empty WAL)
   - Verifies all nodes exist with correct properties
   - Tests that recovery works from snapshot when WAL is empty

2. **TestGraphStorage_SnapshotThenMoreOps** (96 lines)
   - Phase 1: Creates 3 nodes, closes cleanly
   - Phase 2: Recovers, creates 2 more nodes, crashes (no close)
   - Phase 3: Recovers from snapshot (3 nodes) + WAL (2 nodes)
   - Tests that snapshot + WAL replay work together correctly
   - Verifies nodes from snapshot and WAL are both recovered

3. **TestGraphStorage_MultipleSnapshotCycles** (117 lines)
   - Cycle 1: Creates 3 nodes, closes
   - Cycle 2: Recovers, creates 4 more, closes
   - Cycle 3: Recovers, creates 2 more, closes
   - Final: Recovers all 9 nodes
   - Tests repeated snapshot/truncate cycles
   - Verifies data accumulates correctly across cycles

4. **TestGraphStorage_EdgesDurableAcrossSnapshot** (97 lines)
   - Creates 2 nodes and 3 edges
   - Closes cleanly (snapshot + truncate)
   - Recovers from snapshot
   - Verifies edges exist with correct properties
   - Verifies adjacency lists are intact
   - Tests that edges survive snapshot/truncate

5. **TestGraphStorage_IndexesDurableAcrossSnapshot** (72 lines)
   - Creates property index on "age"
   - Creates 3 nodes with age property
   - Closes cleanly (snapshot + truncate)
   - Recovers from snapshot
   - **Tests property index queries work**
   - **THIS TEST FAILED** - exposing the bug

## Bug Discovery

### Initial Test Run

```bash
=== RUN   TestGraphStorage_IndexesDurableAcrossSnapshot
    integration_wal_snapshot_test.go:429: Created property index and 3 nodes, closed cleanly
    integration_wal_snapshot_test.go:453: Property index query failed after snapshot: no index on property age
--- FAIL: TestGraphStorage_IndexesDurableAcrossSnapshot (0.00s)
```

**Bug Symptom**: Property indexes are completely lost after clean shutdown

### Root Cause Analysis

The snapshot structure did NOT include property indexes:

```go
snapshot := struct {
    Nodes         map[uint64]*Node
    Edges         map[uint64]*Edge
    NodesByLabel  map[string][]uint64
    EdgesByType   map[string][]uint64
    OutgoingEdges map[uint64][]uint64
    IncomingEdges map[uint64][]uint64
    NextNodeID    uint64
    NextEdgeID    uint64
    Stats         Statistics
}{
    // ❌ PropertyIndexes missing!
}
```

**Why this happened**:
1. Snapshot saves nodes, edges, label indexes, type indexes
2. Snapshot does NOT save property indexes
3. On clean shutdown, WAL is truncated (cleared)
4. Property index metadata is lost permanently

**Impact**: Unlike iteration 6 where property indexes were lost on crash (but recoverable from WAL), this bug causes property indexes to be **permanently lost on every clean shutdown**.

### Impact Assessment

**Severity**: CRITICAL - Data Loss on Clean Shutdown

**Impact**:
- Property indexes lost on EVERY clean shutdown
- Users must manually recreate indexes after EVERY restart
- No recovery path - indexes gone permanently
- Queries fail silently with "no index" errors
- Performance degrades after every restart

**Affected Operations**:
- `FindNodesByPropertyIndexed()` - broken after every clean shutdown
- Any query relying on property indexes

**Comparison to Iteration 6 Bug**:
- Iteration 6: Property indexes lost on crash (recoverable from WAL)
- **Iteration 8**: Property indexes lost on clean shutdown (NOT recoverable)
- Iteration 8 bug is **worse** because it affects the normal shutdown path

## Fixes Implemented

### Fix 1: Create Serializable PropertyIndexSnapshot

**File**: `pkg/storage/storage.go` (lines 919-924, 6 lines)

```go
// PropertyIndexSnapshot is a serializable representation of a PropertyIndex
type PropertyIndexSnapshot struct {
	PropertyKey string
	IndexType   ValueType
	Index       map[string][]uint64
}
```

PropertyIndex has unexported fields that can't be JSON-serialized directly. Created a snapshot struct with exported fields.

### Fix 2: Serialize Property Indexes in Snapshot

**File**: `pkg/storage/storage.go` (lines 953-963, 11 lines)

```go
// Serialize property indexes
propertyIndexSnapshots := make(map[string]PropertyIndexSnapshot)
for key, idx := range gs.propertyIndexes {
	idx.mu.RLock()
	propertyIndexSnapshots[key] = PropertyIndexSnapshot{
		PropertyKey: idx.propertyKey,
		IndexType:   idx.indexType,
		Index:       idx.index,
	}
	idx.mu.RUnlock()
}
```

### Fix 3: Add PropertyIndexes to Snapshot Structure

**File**: `pkg/storage/storage.go` (lines 965-987, modifications)

```go
snapshot := struct {
	Nodes          map[uint64]*Node
	Edges          map[uint64]*Edge
	NodesByLabel   map[string][]uint64
	EdgesByType    map[string][]uint64
	OutgoingEdges  map[uint64][]uint64
	IncomingEdges  map[uint64][]uint64
	PropertyIndexes map[string]PropertyIndexSnapshot  // ← NEW
	NextNodeID     uint64
	NextEdgeID     uint64
	Stats          Statistics
}{
	Nodes:           gs.nodes,
	Edges:           gs.edges,
	NodesByLabel:    gs.nodesByLabel,
	EdgesByType:     gs.edgesByType,
	OutgoingEdges:   gs.outgoingEdges,
	IncomingEdges:   gs.incomingEdges,
	PropertyIndexes: propertyIndexSnapshots,  // ← NEW
	NextNodeID:      gs.nextNodeID,
	NextEdgeID:      gs.nextEdgeID,
	Stats:           stats,
}
```

### Fix 4: Deserialize Property Indexes on Load

**File**: `pkg/storage/storage.go` (lines 1031, 1051-1060, modifications)

```go
var snapshot struct {
	Nodes          map[uint64]*Node
	Edges          map[uint64]*Edge
	NodesByLabel   map[string][]uint64
	EdgesByType    map[string][]uint64
	OutgoingEdges  map[uint64][]uint64
	IncomingEdges  map[uint64][]uint64
	PropertyIndexes map[string]PropertyIndexSnapshot  // ← NEW
	NextNodeID     uint64
	NextEdgeID     uint64
	Stats          Statistics
}

// ... unmarshal ...

// Deserialize property indexes
gs.propertyIndexes = make(map[string]*PropertyIndex)
for key, idxSnapshot := range snapshot.PropertyIndexes {
	idx := &PropertyIndex{
		propertyKey: idxSnapshot.PropertyKey,
		indexType:   idxSnapshot.IndexType,
		index:       idxSnapshot.Index,
	}
	gs.propertyIndexes[key] = idx
}
```

## Test Results

### After Fix: All Tests Pass

```bash
=== RUN   TestGraphStorage_SnapshotAndTruncate
    All nodes correctly recovered from snapshot after WAL truncation
--- PASS: TestGraphStorage_SnapshotAndTruncate (0.00s)

=== RUN   TestGraphStorage_SnapshotThenMoreOps
    Correctly recovered 3 nodes from snapshot + 2 nodes from WAL replay
--- PASS: TestGraphStorage_SnapshotThenMoreOps (0.00s)

=== RUN   TestGraphStorage_MultipleSnapshotCycles
    Multiple snapshot cycles succeeded - all nodes recovered correctly
--- PASS: TestGraphStorage_MultipleSnapshotCycles (0.00s)

=== RUN   TestGraphStorage_EdgesDurableAcrossSnapshot
    Edges correctly recovered from snapshot with adjacency lists intact
--- PASS: TestGraphStorage_EdgesDurableAcrossSnapshot (0.00s)

=== RUN   TestGraphStorage_IndexesDurableAcrossSnapshot
    Indexes correctly recovered from snapshot
--- PASS: TestGraphStorage_IndexesDurableAcrossSnapshot (0.00s)

PASS
ok      github.com/dd0wney/cluso-graphdb/pkg/storage   0.003s
```

**Result**: 5/5 tests passing (100%)

## Code Changes Summary

### Files Created
- `pkg/storage/integration_wal_snapshot_test.go` (469 lines)
  - 5 comprehensive tests for snapshot durability
  - Tests nodes, edges, indexes, multiple cycles
  - Tests snapshot + WAL replay interaction

### Files Modified
- `pkg/storage/storage.go`
  - Created PropertyIndexSnapshot struct (6 lines)
  - Added property index serialization to Snapshot() (11 lines)
  - Added PropertyIndexes field to snapshot struct (modifications)
  - Added property index deserialization to loadFromDisk() (10 lines)
  - **Total**: 27 lines of new code + struct modifications

**Total Code Written**: 496 lines (469 test + 27 implementation)

## TDD Effectiveness

### What TDD Caught

1. **Property indexes not durable on clean shutdown** - CRITICAL bug
2. **Snapshot structure incomplete** - missing critical data
3. **Silent data loss** - indexes lost without warning

### What TDD Prevented

- Deploying a system where property indexes disappear on every restart
- Users losing indexes every time they cleanly shut down the database
- Performance degradation after every restart
- Silent failures where queries return "no index" errors

### Development Approach

**Test-First Methodology**:
1. Wrote comprehensive snapshot tests FIRST
2. Ran tests and observed property index test failure
3. Analyzed root cause (missing from snapshot)
4. Implemented minimal fix
5. Verified all tests pass
6. No over-engineering

**Benefits**:
- Bug found in 72 lines of test code
- Fix required 27 lines across 1 file
- 100% confidence that property indexes now survive clean shutdowns
- Multiple snapshot cycles tested automatically

## Lessons Learned

### Complete Snapshots Are Critical

A snapshot must include **all state**:
- ✅ Nodes
- ✅ Edges
- ✅ Label indexes
- ✅ Type indexes
- ❌ Property indexes (was missing - now fixed)

Any missing state is permanently lost after WAL truncation.

### Serialization Challenges

PropertyIndex has unexported fields (lowercase), making direct JSON serialization impossible. The solution:
1. Create a serializable snapshot struct with exported fields
2. Copy data into snapshot format
3. Serialize snapshot
4. Deserialize into runtime format

### Clean Shutdown vs. Crash

Two recovery paths have different durability requirements:
- **Crash recovery**: Snapshot + WAL replay
- **Clean shutdown**: Snapshot only (WAL is truncated)

Clean shutdown path is MORE critical because WAL is gone. Everything must be in the snapshot.

### Test Coverage Importance

Previous iterations tested crash recovery (WAL replay) extensively, which **masked** this bug:
- Property indexes worked fine after crash (recovered from WAL)
- Property indexes **failed** after clean shutdown (not in snapshot)

Testing the clean shutdown path was essential to find this bug.

## Milestone 2 Progress

### TDD Iterations Completed

- ✅ **Iteration 1**: Basic WAL durability (node/edge persistence)
- ✅ **Iteration 2**: Double-close protection
- ✅ **Iteration 3**: Disk-backed edge durability (100% edge loss bug fixed)
- ✅ **Iteration 4**: Edge deletion durability (resurrection bug fixed)
- ✅ **Iteration 5**: Node deletion durability (TWO CRITICAL BUGS fixed)
- ✅ **Iteration 6**: Query/index correctness (property index WAL durability bug fixed)
- ✅ **Iteration 7**: Batched WAL durability (NO BUGS - implementation correct)
- ✅ **Iteration 8**: Snapshot durability (property index snapshot bug fixed)

### Bugs Found via TDD So Far

1. Iteration 2: Double-close panic
2. Iteration 3: 100% edge loss on crash
3. Iteration 4: Deleted edges resurrect
4. Iteration 5 (Bug 1): Cascade deletion broken for disk-backed edges
5. Iteration 5 (Bug 2): Node deletions not replayed from WAL
6. Iteration 6: Property indexes lost after crash (WAL missing)
7. Iteration 7: No bugs found
8. **Iteration 8: Property indexes lost after clean shutdown (snapshot missing)** ← NEW

**Total**: 7 critical bugs prevented from reaching production

### Property Index Durability Summary

Property indexes now have **complete durability**:
- ✅ Iteration 6: Property indexes survive crashes (WAL replay)
- ✅ Iteration 8: Property indexes survive clean shutdowns (snapshot)

Both recovery paths are now tested and working.

## Conclusion

TDD Iteration 8 successfully identified and fixed a CRITICAL bug where property indexes were permanently lost on every clean shutdown. The bug would have caused:
- Silent data loss on normal database shutdowns
- Users manually recreating indexes after every restart
- Performance degradation after every restart
- Query failures with "no index" errors

The fix ensures property indexes are fully durable across both recovery paths:
- Crash recovery: Property indexes recovered from WAL
- Clean shutdown: Property indexes recovered from snapshot

All 5 snapshot durability tests pass, giving high confidence that snapshots are complete and recovery works correctly.

**TDD continues to prove its value** by catching critical bugs in the clean shutdown path that were masked by crash recovery tests.
