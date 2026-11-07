// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 105-112, function GraphStorage.nodeIDsForTenant.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// nodeIDsForTenant returns the sorted IDs of the tenant's nodes. It is shared
// by NodesPageForTenant and GetAllNodesForTenant.
func (gs *GraphStorage) nodeIDsForTenant(tid tenantid.TenantID) []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("nodeIDsForTenant")
	return gs.membershipNodeIDsForTenantLocked(tid)
}
