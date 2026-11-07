// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 728-800, function GraphStorage.DeleteEdgeBetweenAcrossTenants.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteEdgeBetweenAcrossTenants deletes an edge between two nodes by type.
// Returns true if an edge was deleted, false if no matching edge existed.
//
// Cross-tenant (see FindEdgeBetweenAcrossTenants): can delete an edge owned by
// any tenant; no request path uses it. Add a *ForTenant variant for scoped
// callers before exposing edge deletion by endpoint.
func (gs *GraphStorage) DeleteEdgeBetweenAcrossTenants(fromID, toID uint64, edgeType string) (deleted bool, err error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. nil handle (no-matching-edge paths) => no-op wait. A wait
	// error (wrapping ErrWALWriteFailed) is reported only when the call has
	// no earlier error to report.
	var walPending *wal.Pending
	defer func() {
		if waitErr := gs.waitWALPending(wal.OpDeleteEdge, walPending); err == nil {
			err = waitErr
		}
	}()
	defer gs.mu.Unlock()

	// Find the edge first
	edgeIDs := gs.getEdgeIDsForNode(fromID, true)
	if edgeIDs == nil {
		return false, nil
	}

	// One-edge-at-a-time shard locking for the search loop. gs.mu.Lock
	// (held above) excludes other writers; lockShard excludes readers
	// per shard during each lookup. A4-edges.
	var edgeToDelete *Edge
	for _, edgeID := range edgeIDs {
		// ruleid: graphdb-lock-call-without-defer
		gs.rlockShard(edgeID)
		edge, err := gs.resolveEdgeRefLocked(edgeID)
		gs.runlockShard(edgeID)
		if err != nil {
			continue // PR B: distinguish unreadable
		}
		if edge.ToNodeID == toID && edge.Type == edgeType {
			edgeToDelete = edge
			break
		}
	}

	if edgeToDelete == nil {
		return false, nil
	}

	// Delete from edges shard under write lock.
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(edgeToDelete.ID)
	gs.deleteEdgeShardEntry(edgeToDelete.ID)
	gs.markEdgeDeletedLocked(edgeToDelete.ID) // mmap mode: mask the base-resident edge
	gs.unlockShard(edgeToDelete.ID)

	// Remove from global type index
	gs.removeEdgeFromTypeIndex(edgeType, edgeToDelete.ID)

	// Remove from tenant-scoped indexes
	gs.removeEdgeFromTenantIndex(edgeToDelete)

	// Remove from adjacency
	if err := gs.removeOutgoingEdge(fromID, edgeToDelete.ID); err != nil {
		return false, fmt.Errorf("failed to remove outgoing edge: %w", err)
	}
	if err := gs.removeIncomingEdge(toID, edgeToDelete.ID); err != nil {
		return false, fmt.Errorf("failed to remove incoming edge: %w", err)
	}

	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	// Enqueue under gs.mu; the deferred wait above blocks off-lock.
	walPending = gs.enqueueWAL(wal.OpDeleteEdge, edgeToDelete)

	return true, nil
}
