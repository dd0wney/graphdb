// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 131-137, function GraphStorage.edgeIDsByType.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// edgeIDsByType is the collector for EdgesByTypePageForTenant.
func (gs *GraphStorage) edgeIDsByType(tid tenantid.TenantID, edgeType string) []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("EdgesByTypePageForTenant")
	return gs.membershipEdgeIDsByTypeLocked(tid, edgeType)
}
