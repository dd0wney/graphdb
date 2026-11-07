// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 21-47, function GraphStorage.CreateNodeWithTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// CreateNodeWithTenant creates a new node for a specific tenant.
//
// Lock discipline (R2.1, S11 spike §7.4): the gs.mu.Lock is released
// before notifyNodeCreated runs so observer code never executes under
// any storage lock. createNodeUnderLock holds gs.mu with a deferred unlock,
// so the notify call lands after the lock release and a panic still
// releases it.
func (gs *GraphStorage) CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error) {
	node, walPending, vectorPlans, err := gs.createNodeUnderLock(tenantID, labels, properties)
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
