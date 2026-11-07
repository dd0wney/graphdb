// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 209-225, function GraphStorage.detachEdgeFromShard.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// detachEdgeFromShard looks the edge up and, if found, deletes it from its
// shard map and masks the base-resident copy in mmap mode, all under one hold
// of the shard write lock. DeleteEdge and the DeleteNode cascades share it.
// Caller holds gs.mu. The deferred unlock keeps a panic from leaving the shard
// write-locked.
func (gs *GraphStorage) detachEdgeFromShard(edgeID uint64) (*Edge, error) {
	gs.lockShard(edgeID)
	defer gs.unlockShard(edgeID)
	panicPoint("detachEdgeFromShard")
	edge, err := gs.resolveEdgeRefLocked(edgeID)
	if err != nil {
		return nil, err
	}
	gs.deleteEdgeShardEntry(edgeID)
	gs.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
	return edge, nil
}
