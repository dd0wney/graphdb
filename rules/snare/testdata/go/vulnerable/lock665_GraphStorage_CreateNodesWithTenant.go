// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 57-94, function GraphStorage.CreateNodesWithTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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
	ids := make([]uint64, 0, len(specs))
	created := make([]*Node, 0, len(specs))
	var pendings []*wal.Pending
	var plans []vectorInsertPlan

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()
	for _, s := range specs {
		node, wp, vps, err := gs.createNodeLocked(tenantID, s.Labels, s.Properties)
		if err != nil {
			gs.mu.Unlock()
			// The creation error, not a WAL wait error, is this call's result —
			// mirrors the pre-existing mid-batch contract below.
			_ = gs.flushNodeBatchEffects(plans, pendings, created)
			return ids, err
		}
		ids = append(ids, node.ID)
		created = append(created, node)
		if wp != nil {
			pendings = append(pendings, wp)
		}
		plans = append(plans, vps...)
	}
	gs.mu.Unlock()

	err := gs.flushNodeBatchEffects(plans, pendings, created)
	return ids, err
}
