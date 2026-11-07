// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4^, lines 178-203, function GraphStorage.EdgesByTypePageForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// EdgesByTypePageForTenant returns up to `limit` edges belonging to `tenantID`
// with the given type and ID > afterID, in ascending-ID order, plus the next
// cursor (the last returned edge's ID, or 0 if this is the last page).
// afterID=0 starts from the beginning. Returns (nil, 0) when the tenant or
// edge type is not found, mirroring GetEdgesByTypeForTenant.
//
// Lock pattern: collect sorted IDs from the type index under gs.mu.RLock,
// release, then clone each edge under its per-shard RLock. Same non-atomic-
// snapshot tradeoff as EdgesPageForTenant and GetAllEdgesForTenant. The third
// return carries ADR 0003's completeness signal.
func (gs *GraphStorage) EdgesByTypePageForTenant(tenantID, edgeType string, afterID uint64, limit int) ([]*Edge, uint64, error) {
	tid := effectiveTenantID(tenantID)

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.RLock()
	ids := gs.membershipEdgeIDsByTypeLocked(tid, edgeType)
	gs.mu.RUnlock()

	if len(ids) == 0 {
		// Nothing to walk, so nothing could have been skipped.
		return nil, 0, nil
	}

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneEdgeAt)
	return page, next, damage.err("edges-by-type-page")
}
