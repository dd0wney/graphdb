// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 173-197, function GraphStorage.NodesByLabelPageForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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

	ids := gs.nodeIDsByLabel(tid, label)

	if len(ids) == 0 {
		// No IDs to walk, so nothing could have been skipped: the enumeration
		// is complete and the error is nil.
		return nil, 0, nil
	}

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneNodeAt)
	return page, next, damage.err("nodes-by-label-page")
}
