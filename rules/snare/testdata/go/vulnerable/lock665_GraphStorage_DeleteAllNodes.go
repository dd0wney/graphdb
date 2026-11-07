// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 956-1042, function GraphStorage.DeleteAllNodes.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteAllNodes removes every node, edge, and index from the graph and truncates
// the WAL, then writes an empty snapshot so a restart doesn't replay the old data.
// Intended for full-reload scenarios where the caller will repopulate the graph
// immediately after.
func (gs *GraphStorage) DeleteAllNodes() error {
	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()

	for i := range gs.nodeShards {
		gs.nodeShards[i] = make(map[uint64]*Node)
	}
	for i := range gs.edgeShards {
		gs.edgeShards[i] = make(map[uint64]*Edge)
	}
	gs.nodesByLabel = make(labelIndex)
	gs.edgesByType = make(labelIndex)
	gs.outgoingEdges = make(map[uint64][]uint64)
	gs.incomingEdges = make(map[uint64][]uint64)
	gs.propertyIndexes = make(map[string]*PropertyIndex)
	gs.tenantNodesByLabel = make(map[tenantid.TenantID]labelIndex)
	gs.tenantEdgesByType = make(map[tenantid.TenantID]labelIndex)
	gs.tenantStats = make(map[tenantid.TenantID]*TenantStats)
	if gs.useEdgeCompression {
		gs.compressedOutgoing = make(map[uint64]*CompressedEdgeList)
		gs.compressedIncoming = make(map[uint64]*CompressedEdgeList)
	}

	// The truncate below no longer resets the WAL LSN — it is monotonic for
	// the life of the data directory (see pkg/wal's RaiseLSNTo) — so the
	// boundary right after this truncate can equal, and typically does
	// equal, the boundary a JSON sync point recorded moments before this
	// call ran. Drop the sync point first so that equality cannot make a
	// later Close look clean; the empty snapshot written at the end records
	// a fresh one if it lands.
	gs.invalidateJSONSnapshotSyncLocked()

	// Truncate whichever WAL variant is active so replay doesn't restore deleted data.
	if gs.wal != nil {
		if err := gs.wal.Truncate(); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("truncate WAL: %w", err)
		}
	}
	if gs.batchedWAL != nil {
		if err := gs.batchedWAL.Truncate(); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("truncate batched WAL: %w", err)
		}
	}
	if gs.compressedWAL != nil {
		if err := gs.compressedWAL.Truncate(); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("truncate compressed WAL: %w", err)
		}
	}

	// mmap mode: the shards we just cleared are only an overlay on the mmap base.
	// Without dropping the base, forEachNodeUnlocked (used by the Snapshot below)
	// still enumerates every base node — re-persisting the data we just deleted —
	// and reads keep serving it, because resolveNodeRefLocked falls through to the
	// base for any node that isn't tombstoned and clearing the shards adds no
	// tombstones. Drop the base so the clear is real. Mirrors Close()
	// (persistence.go); same copy-on-read safety invariant. (#416)
	if gs.mmapSnap != nil {
		if err := gs.mmapSnap.close(); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("unmap snapshot base during clear: %w", err)
		}
		gs.mmapSnap = nil
		for i := range gs.deletedNodes {
			gs.deletedNodes[i] = make(map[uint64]struct{})
			gs.deletedEdges[i] = make(map[uint64]struct{})
		}
	}

	gs.mu.Unlock()

	// Write an empty snapshot so a process restart doesn't reload from the old
	// snapshot.json + WAL.  Snapshot() acquires its own read lock so we release
	// the write lock first.
	if gs.dataDir != "" {
		if err := gs.Snapshot(); err != nil {
			return fmt.Errorf("write empty snapshot after clear: %w", err)
		}
	}

	return nil
}
