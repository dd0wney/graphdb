// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/tenant_operations.go at b6fb6a4, lines 211-250, function GraphStorage.GetAllNodesForTenant.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// GetAllNodesForTenant returns all nodes belonging to a specific tenant.
//
// It enumerates the tenant's own node IDs from tenantNodeIDs (O(tenant
// size)) rather than scanning every shard across all tenants and filtering
// (the former O(total-DB) cross-tenant amplification — Track P / H4). IDs
// are collected under gs.mu.RLock then cloned under per-shard RLocks after
// release, so the global lock is not held across the clone loop and writers
// are not stalled (the A4 read pattern, see GetNodeForTenant).
//
// Consequence of the lock split: the result is no longer an atomic snapshot
// of the tenant at a single instant — a node deleted between the ID
// collection and its clone is simply skipped (same tradeoff A4 accepted for
// GetNode). IDs are returned in ascending order for deterministic pagination.
//
// A node that is ABSENT and a node that will not DECODE are now separated
// (ADR 0003). The first is the tradeoff above and is skipped silently; the
// second is damage on disk, is still skipped so one bad record cannot make the
// whole tenant unreadable, and is named in the returned error. The slice holds
// every node that decoded either way.
func (gs *GraphStorage) GetAllNodesForTenant(tenantID string) ([]*Node, error) {
	tid := effectiveTenantID(tenantID)

	ids := gs.nodeIDsForTenant(tid)

	nodes := make([]*Node, 0, len(ids))
	var damage enumerationDamage
	for _, id := range ids {
		// cloneNodeAt covers one iteration: the shard read lock, the resolve
		// and the clone (owned mmap-base nodes are already heap-safe and are
		// not cloned again), released by defer.
		node, err := gs.cloneNodeAt(id)
		if err == nil {
			nodes = append(nodes, node)
			continue
		}
		damage.note(id, err)
	}

	return nodes, damage.err("all-nodes")
}
