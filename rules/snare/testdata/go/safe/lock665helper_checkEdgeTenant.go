// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 196-207, function GraphStorage.checkEdgeTenant.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// checkEdgeTenant reports ErrEdgeNotFound when edgeID is missing or belongs to
// another tenant. It holds the edge's shard read lock for the check only, with
// a deferred unlock: the Cypher executor and net/http recover panics, so a
// panic that left the read lock held would block every later write to the
// shard. DeleteEdgeForTenant and PatchEdgeForTenant share it.
func (gs *GraphStorage) checkEdgeTenant(edgeID uint64, tenantID string) error {
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)
	panicPoint("checkEdgeTenant")
	_, err := gs.getEdgeRefForTenant(edgeID, tenantID)
	return err
}
