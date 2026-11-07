// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 199-215, function GraphStorage.EdgesPageForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// EdgesPageForTenant returns up to `limit` edges belonging to `tenantID` with
// ID > afterID, in ascending-ID order, plus the next cursor (the last returned
// edge's ID, or 0 if this is the last page). afterID=0 starts from the
// beginning of the tenant's edge set.
//
// Lock pattern mirrors GetAllEdgesForTenant and NodesPageForTenant: collect
// sorted IDs under gs.mu.RLock, release, then clone under per-shard RLocks.
// The third return carries ADR 0003's completeness signal.
func (gs *GraphStorage) EdgesPageForTenant(tenantID string, afterID uint64, limit int) ([]*Edge, uint64, error) {
	tid := effectiveTenantID(tenantID)

	ids := gs.edgeIDsForTenant(tid)

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneEdgeAt)
	return page, next, damage.err("edges-page")
}
