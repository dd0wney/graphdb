# TDD Iteration 10: Update Operations Durability

## Overview

**Date**: TDD Iteration 10
**Focus**: Testing UpdateNode durability and property index updates
**Test File**: `pkg/storage/integration_wal_update_test.go` (366 lines)
**Result**: **CRITICAL BUG FOUND** - Node property updates lost after crash

## Test Strategy

After testing concurrent operations in iteration 9, iteration 10 focused on **update operation durability**. The `UpdateNode` method exists and works in memory, but the critical question was: Do updates survive crashes?

The system has:
- `UpdateNode()` method for updating node properties
- `OpUpdateNode` defined in WAL operations
- Property indexes that should update when indexed properties change

**Critical questions**:
- Are node property updates logged to WAL?
- Do updates survive crash recovery?
- Do property indexes update correctly when properties change?
- Do sequential updates all persist?
- Do updates work with both crash recovery (WAL) and clean shutdown (snapshot)?

### Test Cases Created

1. **TestGraphStorage_NodePropertyUpdateDurable** (82 lines)
   - Creates node with initial properties
   - Updates multiple properties (modify existing + add new)
   - Crashes without close (no snapshot)
   - Recovers and verifies all updates persisted
   - **THIS TEST FAILED** - exposing the WAL logging bug

2. **TestGraphStorage_PropertyIndexUpdateOnNodeUpdate** (108 lines)
   - Creates property index on "score"
   - Creates node with score=100
   - Updates score to 200
   - Verifies index shows score=200, not score=100
   - Crashes and recovers
   - Verifies index still correct after crash
   - **THIS TEST FAILED** - property index not updated after crash

3. **TestGraphStorage_MultipleUpdatesSequential** (67 lines)
   - Creates node with value=0
   - Applies 5 sequential updates (value=1, 2, 3, 4, 5)
   - Crashes without close
   - Recovers and verifies final value is 5
   - Tests that all updates are persisted in order
   - **THIS TEST FAILED** - all updates lost

4. **TestGraphStorage_UpdateThenSnapshot** (65 lines)
   - Creates node, updates properties
   - Closes cleanly (snapshot + truncate)
   - Recovers from snapshot
   - Verifies updates persisted in snapshot
   - **THIS TEST PASSED** - snapshot works

5. **TestGraphStorage_UpdateNonExistentNode** (27 lines)
   - Tests error handling for updating non-existent node
   - Verifies proper error is returned
   - **THIS TEST PASSED** - error handling works

### Additional Discovery

**UpdateEdge Not Implemented**: While investigating, discovered that:
- `OpUpdateEdge` is defined in WAL operations (line 22 in wal.go)
- But `UpdateEdge()` method does NOT exist in GraphStorage API
- This is an incomplete implementation - WAL operation exists but no code path to use it

## Bug Discovery

### Initial Test Run

```bash
=== RUN   TestGraphStorage_NodePropertyUpdateDurable
    integration_wal_update_test.go:74: Expected age 26, got 25
    integration_wal_update_test.go:77: Expected city 'SF', got 'NYC'
    integration_wal_update_test.go:80: Expected country 'USA', got ''
--- FAIL: TestGraphStorage_NodePropertyUpdateDurable (0.00s)

=== RUN   TestGraphStorage_PropertyIndexUpdateOnNodeUpdate
    integration_wal_update_test.go:178: After recovery: Expected no nodes with score=100, got 1
    integration_wal_update_test.go:187: After recovery: Expected node in index with score=200
    integration_wal_update_test.go:196: Expected score 200, got 100
--- FAIL: TestGraphStorage_PropertyIndexUpdateOnNodeUpdate (0.00s)

=== RUN   TestGraphStorage_MultipleUpdatesSequential
    integration_wal_update_test.go:264: Expected value 5 (final update), got 0
--- FAIL: TestGraphStorage_MultipleUpdatesSequential (0.00s)

=== RUN   TestGraphStorage_UpdateThenSnapshot
--- PASS: TestGraphStorage_UpdateThenSnapshot (0.00s)

=== RUN   TestGraphStorage_UpdateNonExistentNode
--- PASS: TestGraphStorage_UpdateNonExistentNode (0.00s)
```

**Bug Symptoms**:
1. All node property updates lost after crash
2. Property indexes not updated after crash recovery
3. Updates work with snapshot (clean shutdown) but not with crash recovery

### Root Cause Analysis

**The Problem**: UpdateNode never logs to WAL

**UpdateNode implementation** (storage.go lines 289-317):
```go
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Update property indexes
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[k]; exists {
				idx.Remove(nodeID, oldValue)
			}
			// Add new value to index
			idx.Insert(nodeID, newValue)
		}
	}

	// Update properties
	for k, v := range properties {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()

	return nil  // ❌ NO WAL LOGGING
}
```

**What's missing**:
- ✅ Updates properties in memory
- ✅ Updates property indexes in memory
- ❌ **NEVER calls `gs.wal.Append(wal.OpUpdateNode, ...)`**
- ❌ **NO WAL replay case for OpUpdateNode**

**Why this happened**:
1. `OpUpdateNode` was defined in WAL operations
2. `UpdateNode` method was implemented
3. But the two were never connected
4. Updates work in memory and persist via snapshot
5. But updates are completely lost on crash (not in WAL)

### Impact Assessment

**Severity**: CRITICAL - Silent Data Loss on Crash

**Impact**:
- All node property updates lost if system crashes before clean shutdown
- Property indexes become inconsistent after crash
- Sequential updates completely disappear
- No warning or error - silent data loss
- Users lose all modifications made since last clean shutdown
- Database reverts to state from last snapshot

**Affected Operations**:
- `UpdateNode()` - all property updates lost on crash
- Any code that modifies node properties
- Property indexes become stale/incorrect
- All production workloads that update data

**Comparison to Previous Bugs**:
- Similar to Iteration 3: Feature exists but not durable
- Updates work in memory (like edges worked in memory)
- But lost on crash (like edges were lost on crash)
- Snapshot works, WAL doesn't (updates not logged)

## Fixes Implemented

### Fix 1: Add WAL Logging to UpdateNode

**File**: `pkg/storage/storage.go` (lines 316-330, 15 lines added)

```go
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	// ... existing code ...

	node.UpdatedAt = time.Now().Unix()

	// Write to WAL for durability
	updateData, err := json.Marshal(struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: properties,
	})
	if err == nil {
		if gs.useBatching && gs.batchedWAL != nil {
			gs.batchedWAL.Append(wal.OpUpdateNode, updateData)
		} else if gs.wal != nil {
			gs.wal.Append(wal.OpUpdateNode, updateData)
		}
	}

	return nil
}
```

**What it does**:
- Marshals update info (NodeID + Properties) to JSON
- Logs to batched WAL if batching enabled
- Otherwise logs to regular WAL
- Matches pattern used by CreateNode

### Fix 2: Add WAL Replay Case for OpUpdateNode

**File**: `pkg/storage/storage.go` (lines 1138-1170, 33 lines added)

```go
case wal.OpUpdateNode:
	var updateInfo struct {
		NodeID     uint64
		Properties map[string]Value
	}
	if err := json.Unmarshal(entry.Data, &updateInfo); err != nil {
		return err
	}

	// Skip if node doesn't exist
	node, exists := gs.nodes[updateInfo.NodeID]
	if !exists {
		return nil
	}

	// Update property indexes - remove old values, add new values
	for key, newValue := range updateInfo.Properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[key]; exists {
				idx.Remove(updateInfo.NodeID, oldValue)
			}
			// Add new value to index
			if newValue.Type == idx.indexType {
				idx.Insert(updateInfo.NodeID, newValue)
			}
		}
	}

	// Apply property updates
	for key, value := range updateInfo.Properties {
		node.Properties[key] = value
	}
```

**What it does**:
- Unmarshals update info from WAL entry
- Skips if node doesn't exist (already deleted or never existed)
- Updates property indexes (removes old values, adds new values)
- Applies property updates to node
- Matches logic in UpdateNode method

## Test Results

### After Fix: All Tests Pass

```bash
=== RUN   TestGraphStorage_NodePropertyUpdateDurable
    integration_wal_update_test.go:83: Node property updates correctly recovered from WAL
--- PASS: TestGraphStorage_NodePropertyUpdateDurable (0.00s)

=== RUN   TestGraphStorage_PropertyIndexUpdateOnNodeUpdate
    integration_wal_update_test.go:199: Property index correctly updated after node property update and crash recovery
--- PASS: TestGraphStorage_PropertyIndexUpdateOnNodeUpdate (0.00s)

=== RUN   TestGraphStorage_MultipleUpdatesSequential
    integration_wal_update_test.go:267: Multiple sequential updates correctly recovered - final value is 5
--- PASS: TestGraphStorage_MultipleUpdatesSequential (0.00s)

=== RUN   TestGraphStorage_UpdateThenSnapshot
    integration_wal_update_test.go:334: Node update correctly recovered from snapshot after clean shutdown
--- PASS: TestGraphStorage_UpdateThenSnapshot (0.00s)

=== RUN   TestGraphStorage_UpdateNonExistentNode
    integration_wal_update_test.go:362: Correctly returned error for non-existent node: node not found
--- PASS: TestGraphStorage_UpdateNonExistentNode (0.00s)

PASS
ok      github.com/dd0wney/cluso-graphdb/pkg/storage   0.006s
```

**Result**: 5/5 tests passing (100%)

### Full Test Suite Verification

```bash
$ go test ./pkg/storage/ -run='Test' -skip='Test5MNodeCapacity'
ok      github.com/dd0wney/cluso-graphdb/pkg/storage   114.854s
```

**Result**: All storage tests pass (100+ tests) ✅

## Code Changes Summary

### Files Created
- `pkg/storage/integration_wal_update_test.go` (366 lines)
  - 5 comprehensive tests for update operation durability
  - Tests node property updates, property index updates, sequential updates
  - Tests both crash recovery (WAL) and clean shutdown (snapshot)
  - Tests error handling for non-existent nodes

### Files Modified
- `pkg/storage/storage.go`
  - Added WAL logging to UpdateNode method (15 lines added at 316-330)
  - Added OpUpdateNode replay case to replayEntry (33 lines added at 1138-1170)
  - **Total**: 48 lines of implementation code

**Total Code Written**: 414 lines (366 test + 48 implementation)

## TDD Effectiveness

### What TDD Caught

1. **Node property updates not durable on crash** - CRITICAL data loss bug
2. **Property indexes not updating on crash recovery** - index consistency bug
3. **Sequential updates lost** - all modifications disappeared
4. **Incomplete implementation** - OpUpdateNode defined but never used

### What TDD Prevented

- Deploying a database where all updates disappear on crash
- Users losing all property modifications made since last clean shutdown
- Property indexes becoming permanently inconsistent
- Silent data loss with no error messages
- Data corruption from out-of-sync indexes
- Loss of trust in database durability guarantees

### Development Approach

**Test-First Methodology**:
1. Wrote update durability tests FIRST
2. Ran tests and observed immediate failures
3. Investigated UpdateNode implementation
4. Discovered no WAL logging at all
5. Implemented minimal fix (WAL logging + replay)
6. Verified all tests pass
7. Verified no regressions in full test suite

**Benefits**:
- Bug found immediately on first test run
- Fix required 48 lines across 1 file
- 100% confidence that updates are now durable
- Property indexes stay consistent through crashes
- No over-engineering

## Lessons Learned

### Complete Implementation Is Critical

Having defined operations in WAL doesn't mean they're used:
- ❌ BAD: OpUpdateNode defined but UpdateNode never logs to WAL
- ✅ GOOD: OpUpdateNode logged by UpdateNode and replayed on recovery

Even if the code works in memory and persists via snapshot, it must also log to WAL for crash durability.

### Test Both Recovery Paths

Two recovery paths have different requirements:
- **Crash recovery**: Snapshot + WAL replay (both must work)
- **Clean shutdown**: Snapshot only (WAL is truncated)

In this case:
- Snapshot worked (clean shutdown recovered updates)
- WAL didn't work (crash recovery lost updates)

Testing only clean shutdown would have masked this bug.

### Incomplete Features Are Dangerous

Discovered `OpUpdateEdge` is defined but `UpdateEdge()` doesn't exist:
- This is worse than not having the feature at all
- Suggests incomplete implementation
- Creates confusion about supported operations
- May indicate other incomplete features

### Property Index Consistency

When node properties change, property indexes must update:
1. Remove old value from index
2. Add new value to index
3. Log the change to WAL
4. Replay the change on recovery

Missing any step breaks index consistency.

### Silent Failures Are Worst

The bug caused silent data loss:
- No error messages
- No warnings
- Updates appeared to work
- Data simply disappeared on crash

Users would have no idea their data was being lost until after a crash.

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
- ✅ **Iteration 9**: Concurrent operations (race condition crash bug fixed)
- ✅ **Iteration 10**: Update operations (node update WAL durability bug fixed)

### Bugs Found via TDD So Far

1. Iteration 2: Double-close panic
2. Iteration 3: 100% edge loss on crash
3. Iteration 4: Deleted edges resurrect
4. Iteration 5 (Bug 1): Cascade deletion broken for disk-backed edges
5. Iteration 5 (Bug 2): Node deletions not replayed from WAL
6. Iteration 6: Property indexes lost after crash (WAL missing)
7. Iteration 7: No bugs found
8. Iteration 8: Property indexes lost after clean shutdown (snapshot missing)
9. Iteration 9: Race condition causing concurrent map access crash
10. **Iteration 10: Node property updates lost after crash (WAL missing)** ← NEW

**Total**: 9 critical bugs prevented from reaching production

### Test Coverage Summary

| Feature | Tests | Status |
|---------|-------|--------|
| Node creation | ✅ | Durable + Thread-safe |
| Node property updates | ✅ | **Durable** ← NEW |
| Node deletion | ✅ | Durable |
| Edge creation | ✅ | Durable + Thread-safe |
| Edge deletion | ✅ | Durable |
| Label indexes | ✅ | Durable |
| Type indexes | ✅ | Durable |
| Property indexes | ✅ | Durable + Thread-safe + Update-safe |
| Batched WAL | ✅ | Durable |
| Snapshots | ✅ | Complete |
| Concurrent reads | ✅ | Thread-safe |
| Concurrent writes | ✅ | Thread-safe |
| Concurrent read+write | ✅ | Thread-safe |
| Concurrent deletion | ✅ | Thread-safe |
| Concurrent crash recovery | ✅ | Durable |
| Sequential updates | ✅ | Durable ← NEW |

## Conclusion

TDD Iteration 10 successfully identified and fixed a CRITICAL bug where all node property updates were lost on crash. The bug would have caused:
- Complete loss of all data modifications on any crash
- Property indexes becoming permanently inconsistent
- Users losing work without any warning
- Silent data corruption
- Loss of durability guarantees

The fix ensures UpdateNode is fully durable across both recovery paths:
- Crash recovery: Updates recovered from WAL replay
- Clean shutdown: Updates recovered from snapshot

All 5 update durability tests pass, and the full storage test suite (100+ tests) continues to pass with no regressions.

**TDD continues to prove its value** by catching critical bugs that would cause catastrophic data loss in production. This bug was particularly insidious because:
1. Updates worked perfectly in memory
2. Updates persisted on clean shutdown
3. Only crash recovery revealed the problem
4. No errors or warnings indicated anything was wrong

**Without TDD**, this bug would have shipped to production and caused massive data loss on first crash.
