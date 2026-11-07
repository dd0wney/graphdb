// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 1059-1131, function GraphStorage.clearAllUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// clearAllUnderLock is the part of DeleteAllNodes that runs under gs.mu. The
// deferred unlock matters: the Cypher executor and net/http recover panics, so
// a panic here that left gs.mu held would hang every later call, Close included.
func (gs *GraphStorage) clearAllUnderLock() error {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("DeleteAllNodes")

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
			return fmt.Errorf("truncate WAL: %w", err)
		}
	}
	if gs.batchedWAL != nil {
		if err := gs.batchedWAL.Truncate(); err != nil {
			return fmt.Errorf("truncate batched WAL: %w", err)
		}
	}
	if gs.compressedWAL != nil {
		if err := gs.compressedWAL.Truncate(); err != nil {
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
			return fmt.Errorf("unmap snapshot base during clear: %w", err)
		}
		gs.mmapSnap = nil
		for i := range gs.deletedNodes {
			gs.deletedNodes[i] = make(map[uint64]struct{})
			gs.deletedEdges[i] = make(map[uint64]struct{})
		}
	}

	return nil
}
