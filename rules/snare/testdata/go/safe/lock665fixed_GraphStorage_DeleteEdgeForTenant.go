// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 179-194, function GraphStorage.DeleteEdgeForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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
	if err := gs.checkEdgeTenant(edgeID, tenantID); err != nil {
		return err
	}
	return gs.DeleteEdge(edgeID)
}
