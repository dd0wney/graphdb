// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 842-850, function GraphStorage.resolveEdgeRefShared.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// resolveEdgeRefShared is resolveEdgeRefLocked under the edge's shard read
// lock. The deferred unlock keeps a panic from leaving the shard read-locked,
// which would block every later write to the shard.
func (gs *GraphStorage) resolveEdgeRefShared(edgeID uint64) (*Edge, error) {
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)
	panicPoint("DeleteEdgeBetweenAcrossTenants")
	return gs.resolveEdgeRefLocked(edgeID)
}
