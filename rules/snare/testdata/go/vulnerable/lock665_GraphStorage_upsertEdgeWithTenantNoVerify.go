// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/edge_operations.go at b6fb6a4^, lines 673-726, function GraphStorage.upsertEdgeWithTenantNoVerify.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a shard-lock wrapper call (lockShard or rlockShard), then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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
		// ruleid: graphdb-lock-call-without-defer
		gs.lockShard(existing.ID)
		edge, err := gs.materializeEdgeLocked(existing.ID) // mmap mode: promote base edge
		if err != nil {
			gs.unlockShard(existing.ID)
			return nil, false, nil, fmt.Errorf("upsert edge %d: %w", existing.ID, err)
		}
		if err := checkPatchWidths(edge.Properties, properties, nil); err != nil {
			gs.unlockShard(existing.ID)
			return nil, false, nil, err
		}

		// Merge properties (new values override existing). An edge created
		// with nil properties has no map to merge into.
		if edge.Properties == nil && len(properties) > 0 {
			edge.Properties = make(map[string]Value, len(properties))
		}
		for k, v := range properties {
			edge.Properties[k] = v
		}
		edge.Weight = weight
		gs.unlockShard(existing.ID)

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
