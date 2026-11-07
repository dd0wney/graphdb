// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 767-785, function GraphStorage.DeleteNodeForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteNodeForTenant deletes a node and all its edges, scoped to the
// given tenant. Returns ErrNodeNotFound on missing or cross-tenant
// (same rationale as GetNodeForTenant).
func (gs *GraphStorage) DeleteNodeForTenant(nodeID uint64, tenantID string) error {
	// Tenant check under per-shard read lock (A4), then delegate to
	// DeleteNode which acquires the write lock. The brief lock-drop
	// window between the two is benign: tenant IDs are immutable after
	// node creation (no API to change them) and node IDs don't get
	// recycled, so the only race is "node deleted by another goroutine
	// before our delete" — which DeleteNode handles correctly by
	// returning ErrNodeNotFound.
	// ruleid: graphdb-lock-call-without-defer
	gs.rlockShard(nodeID)
	if _, err := gs.getNodeRefForTenant(nodeID, tenantID); err != nil {
		gs.runlockShard(nodeID)
		return err
	}
	gs.runlockShard(nodeID)
	return gs.DeleteNode(nodeID)
}
