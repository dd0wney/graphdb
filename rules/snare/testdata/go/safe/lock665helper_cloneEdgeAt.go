// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 158-171, function GraphStorage.cloneEdgeAt.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cloneEdgeAt is cloneNodeAt for an edge.
func (gs *GraphStorage) cloneEdgeAt(id uint64) (*Edge, error) {
	gs.rlockShard(id)
	defer gs.runlockShard(id)
	panicPoint("cloneEdgeAt")
	e, owned, err := gs.resolveEdgeRefOwnedLocked(id)
	if err != nil {
		return nil, err
	}
	if !owned {
		e = e.Clone()
	}
	return e, nil
}
