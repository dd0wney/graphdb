// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/pagination.go at b6fb6a4, lines 114-120, function GraphStorage.nodeIDsByLabel.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// nodeIDsByLabel is the collector for NodesByLabelPageForTenant.
func (gs *GraphStorage) nodeIDsByLabel(tid tenantid.TenantID, label string) []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	panicPoint("NodesByLabelPageForTenant")
	return gs.membershipNodeIDsByLabelLocked(tid, label)
}
