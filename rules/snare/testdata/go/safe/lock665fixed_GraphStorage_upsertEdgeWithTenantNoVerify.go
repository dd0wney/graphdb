// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4, lines 707-743, function GraphStorage.upsertEdgeWithTenantNoVerify.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// upsertEdgeWithTenantNoVerify is the shared upsert core. Caller
// must hold gs.mu.Lock and (when relevant) have validated tenant
// ownership of the source/target nodes.
func (gs *GraphStorage) upsertEdgeWithTenantNoVerify(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, *wal.Pending, error) {
	// Reject non-finite weight (#328) before mutating anything. The create
	// branch funnels through persistEdgeLocked (which re-checks), but the
	// update branch mutates the existing edge in place, so guard here too.
	if err := validateEdgeWeight(weight); err != nil {
		return nil, false, nil, err
	}
	existing, err := gs.findEdgeBetweenLocked(fromID, toID, edgeType)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to check existing edge: %w", err)
	}

	if existing != nil {
		// Update existing edge under per-shard lock to exclude
		// concurrent GetEdge readers. A4-edges.
		edge, err := gs.mergeIntoExistingEdge(existing.ID, properties, weight)
		if err != nil {
			return nil, false, nil, err
		}

		// Enqueue under gs.mu (preserves WAL order); caller waits off-lock.
		walPending := gs.enqueueWAL(wal.OpUpdateEdge, edge)

		return edge.Clone(), false, walPending, nil
	}

	// Create new edge using shared helper
	edge, walPending, err := gs.createEdgeLocked(tenantID, fromID, toID, edgeType, properties, weight)
	if err != nil {
		return nil, false, nil, err
	}

	return edge.Clone(), true, walPending, nil
}
