// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 827-842, function GraphStorage.DeleteNodeForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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
	if err := gs.checkNodeTenant(nodeID, tenantID); err != nil {
		return err
	}
	return gs.DeleteNode(nodeID)
}
