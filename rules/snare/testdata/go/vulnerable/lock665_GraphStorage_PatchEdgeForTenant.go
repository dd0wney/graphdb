// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 345-360, function GraphStorage.PatchEdgeForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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
	// ruleid: graphdb-lock-call-without-defer
	gs.rlockShard(edgeID)
	if _, err := gs.getEdgeRefForTenant(edgeID, tenantID); err != nil {
		gs.runlockShard(edgeID)
		return err
	}
	gs.runlockShard(edgeID)
	return gs.patchEdge(edgeID, set, remove, weight)
}
