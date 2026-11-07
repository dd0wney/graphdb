// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 165-183, function GraphStorage.DeleteEdgeForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteEdgeForTenant deletes an edge by ID, scoped to the given tenant.
// Returns ErrEdgeNotFound on missing or cross-tenant.
//
// See GetNodeForTenant in node_operations.go for the rationale on the
// unified missing-vs-cross-tenant error.
func (gs *GraphStorage) DeleteEdgeForTenant(edgeID uint64, tenantID string) error {
	// Tenant validation under shard read lock; release before delegating
	// to DeleteEdge which acquires the global write lock. The lock-drop
	// window is benign — tenant IDs are immutable after creation, and
	// "edge deleted by another goroutine" surfaces as ErrEdgeNotFound
	// from DeleteEdge.
	// ruleid: graphdb-lock-call-without-defer
	gs.rlockShard(edgeID)
	if _, err := gs.getEdgeRefForTenant(edgeID, tenantID); err != nil {
		gs.runlockShard(edgeID)
		return err
	}
	gs.runlockShard(edgeID)
	return gs.DeleteEdge(edgeID)
}
