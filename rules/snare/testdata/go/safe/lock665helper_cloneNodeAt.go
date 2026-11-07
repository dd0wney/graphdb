// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 139-156, function GraphStorage.cloneNodeAt.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// cloneNodeAt reads one node under its shard RLock and returns a copy the
// caller owns. It is the cloneAt closure the four page methods and their
// by-label variants share, lifted to a method so the four bodies do not each
// carry an identical literal — the previous four copies drifted apart only by
// their type.
func (gs *GraphStorage) cloneNodeAt(id uint64) (*Node, error) {
	gs.rlockShard(id)
	defer gs.runlockShard(id)
	panicPoint("cloneNodeAt")
	n, owned, err := gs.resolveNodeRefOwnedLocked(id)
	if err != nil {
		return nil, err
	}
	if !owned {
		n = n.Clone()
	}
	return n, nil
}
