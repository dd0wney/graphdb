// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 844-855, function GraphStorage.checkNodeTenant.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// checkNodeTenant reports ErrNodeNotFound when nodeID is missing or belongs to
// another tenant. It holds the node's shard read lock for the check only, with
// a deferred unlock: the Cypher executor and net/http recover panics, so a
// panic that left the read lock held would block every later write to the
// shard. PatchNodeForTenant and DeleteNodeForTenant share it.
func (gs *GraphStorage) checkNodeTenant(nodeID uint64, tenantID string) error {
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)
	panicPoint("checkNodeTenant")
	_, err := gs.getNodeRefForTenant(nodeID, tenantID)
	return err
}
