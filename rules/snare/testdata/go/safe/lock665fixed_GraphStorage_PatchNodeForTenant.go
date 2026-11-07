// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 563-578, function GraphStorage.PatchNodeForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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
	if err := gs.checkNodeTenant(nodeID, tenantID); err != nil {
		return err
	}
	return gs.patchNode(nodeID, set, remove)
}
