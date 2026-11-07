// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 382-394, function GraphStorage.PatchEdgeForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// PatchEdgeForTenant sets the keys in set, removes the keys in remove, and
// sets the weight when it is not nil, as one write. This is the storage half
// of JSON Merge Patch on PUT /edges/{id}. A key in both is set and then
// removed. Returns ErrEdgeNotFound on missing or cross-tenant.
func (gs *GraphStorage) PatchEdgeForTenant(edgeID uint64, set map[string]Value, remove []string, weight *float64, tenantID string) error {
	// Tenant validation under the shard read lock; release it before
	// patchEdge takes the global write lock. Same lock-drop rationale as
	// DeleteEdgeForTenant.
	if err := gs.checkEdgeTenant(edgeID, tenantID); err != nil {
		return err
	}
	return gs.patchEdge(edgeID, set, remove, weight)
}
