// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/tenant_operations.go at b6fb6a4, lines 252-285, function GraphStorage.GetAllEdgesForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// GetAllEdgesForTenant returns all edges belonging to a specific tenant.
//
// Edge analogue of GetAllNodesForTenant: it enumerates the tenant's own edge
// IDs from tenantEdgeIDs (O(tenant size)) instead of scanning every shard
// across all tenants and filtering (the former O(total-DB) cross-tenant
// amplification that also stalled writers — Track P / H4). IDs are collected
// under gs.mu.RLock, sorted ascending for deterministic pagination, then
// cloned under per-shard RLocks after release. Same non-atomic-snapshot
// tradeoff as GetAllNodesForTenant: an edge deleted between collection and
// clone is skipped.
//
// Edge analogue of GetAllNodesForTenant for the error return too (ADR 0003):
// an absent edge is skipped silently, an edge that will not decode is skipped
// and named in the returned error, and the slice holds every edge that did
// decode.
func (gs *GraphStorage) GetAllEdgesForTenant(tenantID string) ([]*Edge, error) {
	tid := effectiveTenantID(tenantID)

	ids := gs.edgeIDsForTenant(tid)

	edges := make([]*Edge, 0, len(ids))
	var damage enumerationDamage
	for _, id := range ids {
		// cloneEdgeAt covers one iteration: see GetAllNodesForTenant.
		edge, err := gs.cloneEdgeAt(id)
		if err == nil {
			edges = append(edges, edge)
			continue
		}
		damage.note(id, err)
	}

	return edges, damage.err("all-edges")
}
