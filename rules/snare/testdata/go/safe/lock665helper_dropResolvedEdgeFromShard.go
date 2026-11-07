// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 852-862, function GraphStorage.dropResolvedEdgeFromShard.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// dropResolvedEdgeFromShard is detachEdgeFromShard for a caller that has
// already resolved the edge: it deletes the edge from its shard map and masks
// the base-resident copy in mmap mode, under the shard write lock. Caller holds
// gs.mu. The deferred unlock keeps a panic from leaving the shard write-locked.
func (gs *GraphStorage) dropResolvedEdgeFromShard(edgeID uint64) {
	gs.lockShard(edgeID)
	defer gs.unlockShard(edgeID)
	panicPoint("dropResolvedEdgeFromShard")
	gs.deleteEdgeShardEntry(edgeID)
	gs.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
}
