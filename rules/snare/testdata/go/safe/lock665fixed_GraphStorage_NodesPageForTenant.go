// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 75-98, function GraphStorage.NodesPageForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// NodesPageForTenant returns up to `limit` nodes belonging to `tenantID` with
// ID > afterID, in ascending-ID order, plus the next cursor (the last returned
// node's ID, or 0 if this is the last page). afterID=0 starts from the
// beginning of the tenant's node set.
//
// Lock pattern mirrors GetAllNodesForTenant: collect the sorted ID slice under
// gs.mu.RLock, release, then clone each node under its per-shard RLock. This
// means the result is not an atomic snapshot — a node deleted between ID
// collection and clone is skipped — but the global lock is not held across the
// clone loop, so writers are not stalled.
//
// The third return is the ADR 0003 completeness signal: nil when every record
// the scan touched decoded, and otherwise an error wrapping ErrRecordUnreadable
// that names the ones that did not. The page itself still holds every node that
// decoded.
func (gs *GraphStorage) NodesPageForTenant(tenantID string, afterID uint64, limit int) ([]*Node, uint64, error) {
	tid := effectiveTenantID(tenantID)

	ids := gs.nodeIDsForTenant(tid)

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneNodeAt)
	return page, next, damage.err("nodes-page")
}
