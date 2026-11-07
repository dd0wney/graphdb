// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 49-57, function GraphStorage.createNodeUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// createNodeUnderLock is createNodeLocked inside gs.mu. The deferred unlock
// matters: the Cypher executor and net/http recover panics, so a panic here
// that left gs.mu held would hang every later write, Close included.
func (gs *GraphStorage) createNodeUnderLock(tenantID string, labels []string, properties map[string]Value) (*Node, *wal.Pending, []vectorInsertPlan, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("CreateNodeWithTenant")
	return gs.createNodeLocked(tenantID, labels, properties)
}
