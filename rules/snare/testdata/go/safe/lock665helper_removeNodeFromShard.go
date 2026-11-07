// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 975-984, function GraphStorage.removeNodeFromShard.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// removeNodeFromShard deletes the node from its shard map and, in mmap mode,
// masks the base-resident copy. Caller holds gs.mu. The deferred shard unlock
// keeps a panic from leaving the shard write-locked.
func (gs *GraphStorage) removeNodeFromShard(nodeID uint64) {
	gs.lockShard(nodeID)
	defer gs.unlockShard(nodeID)
	panicPoint("DeleteNode.shard")
	gs.deleteNodeShardEntry(nodeID)
	gs.markNodeDeletedLocked(nodeID) // mmap mode: mask the base-resident node
}
