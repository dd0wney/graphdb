// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 857-879, function GraphStorage.DeleteNode.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteNode deletes a node and all its edges.
//
// Tenant-blind. New callers should prefer DeleteNodeForTenant.
//
// Lock discipline (R2.1, S11 spike §7.4): deleteNodeUnderLock holds gs.mu
// with a deferred unlock, so notifyNodeDeleted dispatches strictly after
// gs.mu.Lock is released. The deleted node's TenantID is
// captured under lock (from the resolveNodeRefLocked lookup) and passed to the
// notify call after unlock — the node's data is not accessible by then.
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	walPending, tenantID, err := gs.deleteNodeUnderLock(nodeID)
	if err != nil {
		return err
	}

	// A wait error wraps ErrWALWriteFailed; the delete stays applied and
	// observers are still notified.
	err = gs.waitWALPending(wal.OpDeleteNode, walPending)
	// R2.1: dispatch after lock release. See lock-discipline comment in
	// pkg/storage/observation.go.
	gs.notifyNodeDeleted(context.Background(), nodeID, tenantID)
	return err
}
