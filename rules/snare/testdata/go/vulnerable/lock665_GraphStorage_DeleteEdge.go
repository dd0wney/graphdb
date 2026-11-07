// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 215-268, function GraphStorage.DeleteEdge.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteEdge deletes an edge by ID.
//
// Tenant-blind. New callers should prefer DeleteEdgeForTenant.
func (gs *GraphStorage) DeleteEdge(edgeID uint64) (err error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. nil handle (not-found path) => no-op wait. A wait error
	// (wrapping ErrWALWriteFailed) is reported only when the call has no
	// earlier error to report.
	var walPending *wal.Pending
	defer func() {
		if waitErr := gs.waitWALPending(wal.OpDeleteEdge, walPending); err == nil {
			err = waitErr
		}
	}()
	defer gs.mu.Unlock()

	// Lookup + delete on edgeShards under the per-shard write lock so
	// concurrent GetEdge readers see a consistent map. gs.mu.Lock above
	// excludes other writers; lockShard excludes the readers. A4-edges.
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(edgeID)
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		gs.unlockShard(edgeID)
		return fmt.Errorf("edge %d not found: %w", edgeID, err)
	}
	fromID := edge.FromNodeID
	toID := edge.ToNodeID
	gs.deleteEdgeShardEntry(edgeID)
	gs.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
	gs.unlockShard(edgeID)

	// Remove from global type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)

	// Remove from tenant-scoped indexes
	gs.removeEdgeFromTenantIndex(edge)

	// Remove from adjacency (disk-backed or in-memory)
	if err := gs.removeOutgoingEdge(fromID, edgeID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge: %w", err)
	}
	if err := gs.removeIncomingEdge(toID, edgeID); err != nil {
		return fmt.Errorf("failed to remove incoming edge: %w", err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

	// Enqueue under gs.mu; the deferred wait above blocks off-lock.
	walPending = gs.enqueueWAL(wal.OpDeleteEdge, edge)

	return nil
}
