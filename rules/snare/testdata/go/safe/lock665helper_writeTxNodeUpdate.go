// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/transaction_commit.go at b6fb6a4, lines 211-226, function GraphStorage.writeTxNodeUpdate.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// writeTxNodeUpdate merges props into node in place under the node's shard
// lock. Caller holds gs.mu.
func (gs *GraphStorage) writeTxNodeUpdate(node *Node, props map[string]Value) {
	gs.lockShard(node.ID)
	defer gs.unlockShard(node.ID)
	panicPoint("Transaction.Commit.shard")

	// A node created with nil properties has no map to merge into.
	if node.Properties == nil && len(props) > 0 {
		node.Properties = make(map[string]Value, len(props))
	}
	for k, v := range props {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()
}
