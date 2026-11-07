// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4^, lines 130-156, function GraphStorage.NodesByLabelPageForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// NodesByLabelPageForTenant returns up to `limit` nodes belonging to
// `tenantID` with the given label and ID > afterID, in ascending-ID order,
// plus the next cursor (the last returned node's ID, or 0 if this is the last
// page). afterID=0 starts from the beginning. Returns (nil, 0) when the tenant
// or label is not found, mirroring GetNodesByLabelForTenant.
//
// Lock pattern: collect sorted IDs from the label index under gs.mu.RLock,
// release, then clone each node under its per-shard RLock. Same non-atomic-
// snapshot tradeoff as NodesPageForTenant and GetAllNodesForTenant. The third
// return carries ADR 0003's completeness signal, as on NodesPageForTenant.
func (gs *GraphStorage) NodesByLabelPageForTenant(tenantID, label string, afterID uint64, limit int) ([]*Node, uint64, error) {
	tid := effectiveTenantID(tenantID)

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.RLock()
	ids := gs.membershipNodeIDsByLabelLocked(tid, label)
	gs.mu.RUnlock()

	if len(ids) == 0 {
		// No IDs to walk, so nothing could have been skipped: the enumeration
		// is complete and the error is nil.
		return nil, 0, nil
	}

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneNodeAt)
	return page, next, damage.err("nodes-by-label-page")
}
