// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 65-86, function GraphStorage.CreateNodesWithTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// CreateNodesWithTenant creates many nodes under one acquisition of gs.mu,
// mirroring CreateNodeWithTenant's per-node logic (createNodeLocked) but
// amortizing the global write lock across the whole batch. Post-lock steps
// (vector inserts, WAL durability wait, observer notify) run once after unlock,
// exactly as the single-node path does. Returns the new IDs in input order.
//
// On error mid-batch the lock is released and the IDs created so far are
// returned alongside the error — the nodes already published before the failure
// stay durable (their WAL entries were enqueued under the lock), matching the
// single-create contract where a successful createNodeLocked is not rolled back.
func (gs *GraphStorage) CreateNodesWithTenant(tenantID string, specs []NodeSpec) ([]uint64, error) {
	b, err := gs.createNodesUnderLock(tenantID, specs)
	if err != nil {
		// The creation error, not a WAL wait error, is this call's result —
		// mirrors the pre-existing mid-batch contract below.
		_ = gs.flushNodeBatchEffects(b.plans, b.pendings, b.created)
		return b.ids, err
	}

	err = gs.flushNodeBatchEffects(b.plans, b.pendings, b.created)
	return b.ids, err
}
