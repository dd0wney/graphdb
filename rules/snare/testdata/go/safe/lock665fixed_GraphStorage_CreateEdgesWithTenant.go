// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 91-111, function GraphStorage.CreateEdgesWithTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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
	b, err := gs.createEdgesUnderLock(tenantID, specs)
	if err != nil {
		// The verification error, not a WAL wait error, is this call's
		// result — mirrors the mid-batch contract below.
		_ = gs.flushEdgeBatchWAL(b.pendings)
		return b.ids, err
	}

	err = gs.flushEdgeBatchWAL(b.pendings)
	return b.ids, err
}
