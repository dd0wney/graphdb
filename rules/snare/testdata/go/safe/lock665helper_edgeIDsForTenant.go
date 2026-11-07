// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 122-129, function GraphStorage.edgeIDsForTenant.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// edgeIDsForTenant returns the sorted IDs of the tenant's edges. It is shared
// by EdgesPageForTenant and GetAllEdgesForTenant.
func (gs *GraphStorage) edgeIDsForTenant(tid tenantid.TenantID) []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("edgeIDsForTenant")
	return gs.membershipEdgeIDsForTenantLocked(tid)
}
