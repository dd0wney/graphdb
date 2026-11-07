// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 523-541, function GraphStorage.PatchNodeForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// PatchNodeForTenant sets the keys in set and removes the keys in remove, as
// one write with one WAL record, so a crash cannot leave half of it applied.
// This is the storage half of JSON Merge Patch on PUT /nodes/{id}. A key in
// both is set and then removed, the order replay applies. Returns
// ErrNodeNotFound on missing or cross-tenant.
func (gs *GraphStorage) PatchNodeForTenant(nodeID uint64, set map[string]Value, remove []string, tenantID string) error {
	// Check ownership under the shard read lock, release it, then take the
	// write locks in patchNode. The window between is benign: tenant IDs are
	// immutable after creation and node IDs don't recycle, so the only race is
	// "node deleted by another goroutine first", which patchNode reports as
	// ErrNodeNotFound.
	// ruleid: graphdb-lock-call-without-defer
	gs.rlockShard(nodeID)
	if _, err := gs.getNodeRefForTenant(nodeID, tenantID); err != nil {
		gs.runlockShard(nodeID)
		return err
	}
	gs.runlockShard(nodeID)
	return gs.patchNode(nodeID, set, remove)
}
