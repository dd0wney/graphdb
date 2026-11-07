// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_adjacency.go at b6fb6a4^, lines 61-89, function GraphStorage.cascadeDeleteOutgoingEdge.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cascadeDeleteOutgoingEdge deletes an outgoing edge and removes it from the target node's incoming list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteOutgoingEdge(edgeID uint64) error {
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(edgeID)
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		gs.unlockShard(edgeID)
		return nil // PR B: an unreadable edge must not silently skip its cascade
	}
	gs.deleteEdgeShardEntry(edgeID)
	gs.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
	gs.unlockShard(edgeID)

	// Remove from target node's incoming edges
	if err := gs.removeIncomingEdge(edge.ToNodeID, edgeID); err != nil {
		return fmt.Errorf("failed to remove incoming edge %d: %w", edgeID, err)
	}
	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)
	// Remove from the per-tenant edge index (type map + enumeration set + tenant
	// EdgeCount), mirroring the non-cascade DeleteEdge. Without this a cascaded
	// edge leaks the *ForTenant readers and CountEdgesForTenant in NORMAL
	// operation — the cascade-path sibling of the #288/#298 tenant-index gaps.
	// Caller (DeleteNode) holds gs.mu.Lock, which guards the tenant index.
	gs.removeEdgeFromTenantIndex(edge)
	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	return nil
}
