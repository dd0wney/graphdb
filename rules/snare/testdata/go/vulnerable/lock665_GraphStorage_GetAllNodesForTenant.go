// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/tenant_operations.go at b6fb6a4^, lines 211-256, function GraphStorage.GetAllNodesForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.RLock()
	ids := gs.membershipNodeIDsForTenantLocked(tid)
	gs.mu.RUnlock()

	nodes := make([]*Node, 0, len(ids))
	var damage enumerationDamage
	for _, id := range ids {
		// ruleid: graphdb-lock-call-without-defer
		gs.rlockShard(id)
		node, owned, err := gs.resolveNodeRefOwnedLocked(id)
		exists := err == nil
		// owned (mmap-base) nodes are already heap-safe — no Clone and safe to keep after unlock.
		if exists && !owned {
			node = node.Clone()
		}
		gs.runlockShard(id)
		if exists {
			nodes = append(nodes, node)
			continue
		}
		damage.note(id, err)
	}

	return nodes, damage.err("all-nodes")
}
