// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 21-49, function GraphStorage.CreateNodeWithTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// CreateNodeWithTenant creates a new node for a specific tenant.
//
// Lock discipline (R2.1, S11 spike §7.4): the gs.mu.Lock is released
// before notifyNodeCreated runs so observer code never executes under
// any storage lock. Direct unlock + nil-check on err mirrors what
// `defer gs.mu.Unlock()` would do but lets the notify call land after
// the lock release.
func (gs *GraphStorage) CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error) {
	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()
	node, walPending, vectorPlans, err := gs.createNodeLocked(tenantID, labels, properties)
	gs.mu.Unlock()
	// Run the HNSW insert(s) OUTSIDE gs.mu (Track P item 3 / H2) — the O(log N)
	// traversal + O(M^2) pruning no longer serialize behind the global write
	// lock. Ordered before the WAL wait and the observer dispatch so a node's
	// declared vector is searchable before any observer (e.g. auto-embed) acts.
	gs.applyNodeVectorInserts(vectorPlans)
	// Wait for WAL durability OUTSIDE gs.mu so concurrent writers can fill the
	// same batch (group commit, Track P item 1). nil handle = synchronous path
	// (already durable). A wait error wraps ErrWALWriteFailed; the node is
	// already applied and notified, so it is still returned alongside the
	// error rather than rolled back.
	if waitErr := gs.waitWALPending(wal.OpCreateNode, walPending); err == nil {
		err = waitErr
	}
	if node != nil {
		gs.notifyNodeCreated(context.Background(), node)
	}
	return node, err
}
