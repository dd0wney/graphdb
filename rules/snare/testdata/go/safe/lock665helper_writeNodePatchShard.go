// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 694-726, function GraphStorage.writeNodePatchShard.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// writeNodePatchShard applies set and remove to node in place, and returns the
// keys it removed and the removed keys that are vector-indexed. Caller holds
// gs.mu.
//
// Per-shard write lock (A4) excludes shard.RLock readers during the
// in-place Node-struct mutation. The removals walk node.Properties for the
// index Remove calls before they delete, so they stay inside the same window.
//
// A removed vector-indexed property must also leave the HNSW index, or
// VectorSearch keeps returning the stale vector. HasIndexForTenant reads
// the index map, so the removals are planned here and applied off-lock,
// per key, so the node's other vector properties are untouched.
func (gs *GraphStorage) writeNodePatchShard(node *Node, set map[string]Value, remove []string, tid tenantid.TenantID) (removed, vectorRemovals []string) {
	gs.lockShard(node.ID)
	defer gs.unlockShard(node.ID)
	panicPoint("patchNode.shard")

	// A node created with nil properties has no map to write into.
	if node.Properties == nil && len(set) > 0 {
		node.Properties = make(map[string]Value, len(set))
	}
	for k, v := range set {
		node.Properties[k] = v
	}
	removed = gs.deleteNodePropertiesLocked(node.ID, node, remove)
	for _, key := range removed {
		if gs.vectorIndex.HasIndexForTenant(tid, key) {
			vectorRemovals = append(vectorRemovals, key)
		}
	}
	node.UpdatedAt = time.Now().Unix()
	return removed, vectorRemovals
}
