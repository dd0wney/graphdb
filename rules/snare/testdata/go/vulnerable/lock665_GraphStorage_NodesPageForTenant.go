// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4^, lines 71-96, function GraphStorage.NodesPageForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.RLock()
	ids := gs.membershipNodeIDsForTenantLocked(tid)
	gs.mu.RUnlock()

	var damage enumerationDamage
	page, next := pageFromSortedIDs(ids, afterID, limit, &damage, gs.cloneNodeAt)
	return page, next, damage.err("nodes-page")
}
