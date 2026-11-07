// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/mmap_snapshot_loader.go at b6fb6a4, lines 250-258, function GraphStorage.materializeNode.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// materializeNode is materializeNodeLocked under the node's shard lock, for a
// caller that holds gs.mu but not the shard. The deferred unlock keeps a panic
// in the mmap decode from leaving the shard locked.
func (gs *GraphStorage) materializeNode(id uint64) (*Node, error) {
	gs.lockShard(id)
	defer gs.unlockShard(id)
	panicPoint("materializeNode")
	return gs.materializeNodeLocked(id)
}
