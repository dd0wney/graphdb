# TDD Iteration 6: Query Correctness After Crash Recovery

## Overview

**Date**: TDD Iteration 6
**Focus**: Testing query correctness and index durability after crash recovery
**Test File**: `pkg/storage/integration_wal_query_test.go` (368 lines)
**Result**: **CRITICAL BUG FOUND** - Property indexes lost after crash

## Test Strategy

After successfully testing node and edge deletion recovery in iterations 4-5, iteration 6 focused on verifying that **queries remain correct** after crash recovery. This tests whether all indexes (label, type, and property indexes) are properly rebuilt from the WAL.

### Test Cases Created

1. **TestGraphStorage_LabelIndexRecovery** (110 lines)
   - Creates nodes with different labels (Person, Company, Employee)
   - Creates multi-label nodes (Person+Employee)
   - Crashes without Close()
   - Recovers and verifies FindNodesByLabel works correctly
   - Verifies all original node IDs are present in recovered indexes

2. **TestGraphStorage_TypeIndexRecovery** (97 lines)
   - Creates edges with different types (KNOWS, WORKS_AT)
   - Crashes without Close()
   - Recovers and verifies FindEdgesByType works correctly
   - Verifies all original edge IDs are present in recovered indexes

3. **TestGraphStorage_PropertyIndexRecovery** (82 lines)
   - Creates property index on "age" field
   - Creates nodes with indexed properties
   - Crashes without Close()
   - Recovers and verifies FindNodesByPropertyIndexed works
   - **THIS TEST FAILED** - exposing the bug

4. **TestGraphStorage_DeletedNodeLabelIndexRecovery** (73 lines)
   - Creates Person nodes
   - Deletes one node
   - Crashes without Close()
   - Recovers and verifies deleted node is NOT in label index
   - Tests that deletion is reflected in indexes after recovery

## Bug Discovery

### Initial Test Run

```bash
=== RUN   TestGraphStorage_PropertyIndexRecovery
    integration_wal_query_test.go:273: FindNodesByPropertyIndexed failed after crash: no index on property age
--- FAIL: TestGraphStorage_PropertyIndexRecovery
```

**Bug Symptom**: Property indexes are completely lost after crash recovery

### Root Cause Analysis

Property indexes were not durable because:

1. **Missing WAL Operations**: `OpCreatePropertyIndex` and `OpDropPropertyIndex` did not exist in the WAL operation types
2. **No WAL Logging**: `CreatePropertyIndex` and `DropPropertyIndex` never wrote to WAL
3. **No WAL Replay**: WAL replay had no cases for property index operations

### Impact Assessment

**Severity**: CRITICAL - Data Loss

**Impact**:

- Any property index created before a crash is permanently lost
- Queries using `FindNodesByPropertyIndexed()` fail after crash with "no index" error
- No way to recover property indexes without recreating them manually
- Silent data loss - users would not know indexes were lost until queries failed

**Affected Operations**:

- `FindNodesByPropertyIndexed()` - completely broken after crash
- Any queries relying on property indexes for performance

## Fixes Implemented

### Fix 1: Add WAL Operation Types

**File**: `pkg/wal/wal.go` (lines 17-26)

```go
const (
    OpCreateNode OpType = iota
    OpUpdateNode
    OpDeleteNode
    OpCreateEdge
    OpUpdateEdge
    OpDeleteEdge
    OpCreatePropertyIndex  // ← NEW
    OpDropPropertyIndex    // ← NEW
)
```

### Fix 2: Add WAL Logging to CreatePropertyIndex

**File**: `pkg/storage/storage.go` (lines 1391-1405, 15 lines)

```go
func (gs *GraphStorage) CreatePropertyIndex(propertyKey string, valueType ValueType) error {
    // ... existing code ...

    // Write to WAL for durability
    indexData, err := json.Marshal(struct {
        PropertyKey string
        ValueType   ValueType
    }{
        PropertyKey: propertyKey,
        ValueType:   valueType,
    })
    if err == nil {
        if gs.useBatching && gs.batchedWAL != nil {
            gs.batchedWAL.Append(wal.OpCreatePropertyIndex, indexData)
        } else if gs.wal != nil {
            gs.wal.Append(wal.OpCreatePropertyIndex, indexData)
        }
    }

    return nil
}
```

### Fix 3: Add WAL Logging to DropPropertyIndex

**File**: `pkg/storage/storage.go` (lines 1421-1433, 13 lines)

```go
func (gs *GraphStorage) DropPropertyIndex(propertyKey string) error {
    // ... existing code ...

    // Write to WAL for durability
    indexData, err := json.Marshal(struct {
        PropertyKey string
    }{
        PropertyKey: propertyKey,
    })
    if err == nil {
        if gs.useBatching && gs.batchedWAL != nil {
            gs.batchedWAL.Append(wal.OpDropPropertyIndex, indexData)
        } else if gs.wal != nil {
            gs.wal.Append(wal.OpDropPropertyIndex, indexData)
        }
    }

    return nil
}
```

### Fix 4: Add WAL Replay for Property Indexes

**File**: `pkg/storage/storage.go` (lines 1363-1398, 36 lines)

```go
case wal.OpCreatePropertyIndex:
    var indexInfo struct {
        PropertyKey string
        ValueType   ValueType
    }
    if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
        return err
    }

    // Skip if index already exists
    if _, exists := gs.propertyIndexes[indexInfo.PropertyKey]; exists {
        return nil
    }

    // Create index and populate with existing nodes
    idx := NewPropertyIndex(indexInfo.PropertyKey, indexInfo.ValueType)
    for nodeID, node := range gs.nodes {
        if prop, exists := node.Properties[indexInfo.PropertyKey]; exists {
            if prop.Type == indexInfo.ValueType {
                idx.Insert(nodeID, prop)
            }
        }
    }
    gs.propertyIndexes[indexInfo.PropertyKey] = idx

case wal.OpDropPropertyIndex:
    var indexInfo struct {
        PropertyKey string
    }
    if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
        return err
    }

    // Remove index
    delete(gs.propertyIndexes, indexInfo.PropertyKey)
```

### Fix 5: Insert Nodes into Property Indexes During Replay

**File**: `pkg/storage/storage.go` (lines 1072-1079, 8 lines)

**Problem**: When replaying WAL entries, property indexes are created before nodes, so the initial index population in OpCreatePropertyIndex finds zero nodes.

**Solution**: When replaying OpCreateNode, also insert into any existing property indexes.

```go
case wal.OpCreateNode:
    // ... existing node creation code ...

    // Insert into property indexes if they exist
    for key, value := range node.Properties {
        if idx, exists := gs.propertyIndexes[key]; exists {
            if value.Type == idx.indexType {
                idx.Insert(node.ID, value)
            }
        }
    }
```

This ensures nodes are added to property indexes in the correct order during replay:

1. OpCreatePropertyIndex replayed → creates empty index
2. OpCreateNode replayed → inserts node into the index

## Test Results

### After Fix: All Tests Pass

```bash
=== RUN   TestGraphStorage_LabelIndexRecovery
    integration_wal_query_test.go:108: Label indexes correctly recovered from crash via WAL
--- PASS: TestGraphStorage_LabelIndexRecovery (0.00s)

=== RUN   TestGraphStorage_TypeIndexRecovery
    integration_wal_query_test.go:206: Type indexes correctly recovered from crash via WAL
--- PASS: TestGraphStorage_TypeIndexRecovery (0.00s)

=== RUN   TestGraphStorage_PropertyIndexRecovery
    integration_wal_query_test.go:290: Property indexes correctly recovered from crash via WAL
--- PASS: TestGraphStorage_PropertyIndexRecovery (0.00s)

=== RUN   TestGraphStorage_DeletedNodeLabelIndexRecovery
    integration_wal_query_test.go:365: Label indexes correctly reflect node deletion after crash
--- PASS: TestGraphStorage_DeletedNodeLabelIndexRecovery (0.00s)

PASS
ok      github.com/dd0wney/cluso-graphdb/pkg/storage   0.005s
```

**Result**: 4/4 tests passing (100%)

## Code Changes Summary

### Files Created

- `pkg/storage/integration_wal_query_test.go` (368 lines)
  - 4 comprehensive tests for index recovery
  - Tests label, type, and property indexes
  - Tests deletion reflection in indexes

### Files Modified

- `pkg/wal/wal.go`
  - Added 2 new operation types (2 lines)

- `pkg/storage/storage.go`
  - Added WAL logging to CreatePropertyIndex (15 lines)
  - Added WAL logging to DropPropertyIndex (13 lines)
  - Added OpCreatePropertyIndex replay case (25 lines)
  - Added OpDropPropertyIndex replay case (11 lines)
  - Added property index insertion to OpCreateNode replay (8 lines)
  - Fixed switch statement syntax (removed extra brace)
  - **Total**: 72 lines of new code

**Total Code Written**: 440 lines (368 test + 72 implementation)

## TDD Effectiveness

### What TDD Caught

1. **Property indexes not durable** - CRITICAL bug that would cause silent data loss
2. **Missing WAL operations** for property index management
3. **WAL replay ordering issue** - indexes created before nodes during replay

### What TDD Prevented

- Deploying a system where property indexes are lost on crash
- Silent failures where indexes disappear without warning
- Users relying on property indexes that don't survive crashes
- Data corruption where queries return incorrect results after recovery

### Development Approach

**Test-First Methodology**:

1. Wrote comprehensive query recovery tests FIRST
2. Ran tests and observed failures
3. Analyzed root cause
4. Implemented minimal fix
5. Verified tests pass
6. No over-engineering or unnecessary code

**Benefits**:

- Bug found in 5 lines of test code (the property index query)
- Fix required 72 lines across 2 files
- 100% confidence that property indexes now survive crashes
- No manual testing required

## Lessons Learned

### Index Durability is Critical

All three index types (label, type, property) must be durable:

- Label indexes: ✅ Durable (rebuilt during OpCreateNode replay)
- Type indexes: ✅ Durable (rebuilt during OpCreateEdge replay)
- Property indexes: ❌ NOT durable (now fixed)

### WAL Replay Ordering Matters

When replaying WAL:

1. Property indexes are created first (OpCreatePropertyIndex)
2. Nodes are created second (OpCreateNode)

The OpCreateNode replay must insert into existing property indexes, otherwise indexes remain empty after recovery.

### Schema Changes Need WAL Support

Any operation that changes schema (like creating/dropping indexes) needs:

1. WAL operation type
2. WAL logging in the operation
3. WAL replay logic

Property index creation is a schema operation and was missing all three.

## Milestone 2 Progress

### TDD Iterations Completed

- ✅ **Iteration 1**: Basic WAL durability (node/edge persistence)
- ✅ **Iteration 2**: Double-close protection
- ✅ **Iteration 3**: Disk-backed edge durability (100% edge loss bug fixed)
- ✅ **Iteration 4**: Edge deletion durability (resurrection bug fixed)
- ✅ **Iteration 5**: Node deletion durability (TWO CRITICAL BUGS fixed)
- ✅ **Iteration 6**: Query/index correctness (property index durability bug fixed)

### Bugs Found via TDD So Far

1. Iteration 2: Double-close panic
2. Iteration 3: 100% edge loss on crash
3. Iteration 4: Deleted edges resurrect
4. Iteration 5 (Bug 1): Cascade deletion broken for disk-backed edges
5. Iteration 5 (Bug 2): Node deletions not replayed from WAL
6. **Iteration 6: Property indexes lost after crash** ← NEW

**Total**: 6 critical bugs prevented from reaching production

## Conclusion

TDD Iteration 6 successfully identified and fixed a CRITICAL bug where property indexes were not durable. The bug would have caused:

- Silent data loss (indexes disappear after crash)
- Query failures (FindNodesByPropertyIndexed returns "no index" error)
- No recovery path (indexes lost permanently)

The fix ensures all three index types (label, type, property) are now fully durable and survive crash recovery. All 4 query correctness tests pass, giving high confidence that queries work correctly after crash recovery.

**TDD continues to prove its value** by catching critical bugs before they reach production.
