// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_adjacency.go at b6fb6a4^, lines 91-115, function GraphStorage.cascadeDeleteIncomingEdge.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cascadeDeleteIncomingEdge deletes an incoming edge and removes it from the source node's outgoing list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteIncomingEdge(edgeID uint64) error {
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

	// Remove from source node's outgoing edges
	if err := gs.removeOutgoingEdge(edge.FromNodeID, edgeID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge %d: %w", edgeID, err)
	}
	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)
	// Remove from the per-tenant edge index — see cascadeDeleteOutgoingEdge.
	gs.removeEdgeFromTenantIndex(edge)
	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	return nil
}
