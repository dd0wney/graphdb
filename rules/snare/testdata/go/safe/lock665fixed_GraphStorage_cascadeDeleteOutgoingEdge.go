// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_adjacency.go at b6fb6a4, lines 61-84, function GraphStorage.cascadeDeleteOutgoingEdge.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cascadeDeleteOutgoingEdge deletes an outgoing edge and removes it from the target node's incoming list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteOutgoingEdge(edgeID uint64) error {
	edge, err := gs.detachEdgeFromShard(edgeID)
	if err != nil {
		return nil // PR B: an unreadable edge must not silently skip its cascade
	}

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
