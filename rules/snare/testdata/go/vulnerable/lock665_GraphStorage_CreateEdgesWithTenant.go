// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 91-133, function GraphStorage.CreateEdgesWithTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// CreateEdgesWithTenant creates many edges under one acquisition of gs.mu,
// mirroring CreateEdgeWithTenant (verify endpoints, then
// createEdgeWithTenantNoVerify) but amortizing the lock. WAL waits run once
// after unlock. Returns the new edge IDs in input order.
//
// On error mid-batch the lock is released, the WAL waits for already-created
// edges still run, and the IDs created so far are returned with the error —
// matching the single-create contract (a successful createEdgeWithTenantNoVerify
// is durable and not rolled back).
func (gs *GraphStorage) CreateEdgesWithTenant(tenantID string, specs []EdgeSpec) ([]uint64, error) {
	ids := make([]uint64, 0, len(specs))
	var pendings []*wal.Pending

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()
	for _, s := range specs {
		if err := gs.verifyNodeExistsForTenant(s.FromID, "source", tenantID); err != nil {
			gs.mu.Unlock()
			// The verification error, not a WAL wait error, is this call's
			// result — mirrors the mid-batch contract below.
			_ = gs.flushEdgeBatchWAL(pendings)
			return ids, err
		}
		if err := gs.verifyNodeExistsForTenant(s.ToID, "target", tenantID); err != nil {
			gs.mu.Unlock()
			_ = gs.flushEdgeBatchWAL(pendings)
			return ids, err
		}
		edge, p, err := gs.createEdgeWithTenantNoVerify(tenantID, s.FromID, s.ToID, s.Type, s.Properties, s.Weight)
		if err != nil {
			gs.mu.Unlock()
			_ = gs.flushEdgeBatchWAL(pendings)
			return ids, err
		}
		ids = append(ids, edge.ID)
		if p != nil {
			pendings = append(pendings, p)
		}
	}
	gs.mu.Unlock()

	err := gs.flushEdgeBatchWAL(pendings)
	return ids, err
}
