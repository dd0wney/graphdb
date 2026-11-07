// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_adjacency.go at b6fb6a4, lines 86-105, function GraphStorage.cascadeDeleteIncomingEdge.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cascadeDeleteIncomingEdge deletes an incoming edge and removes it from the source node's outgoing list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteIncomingEdge(edgeID uint64) error {
	edge, err := gs.detachEdgeFromShard(edgeID)
	if err != nil {
		return nil // PR B: an unreadable edge must not silently skip its cascade
	}

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
