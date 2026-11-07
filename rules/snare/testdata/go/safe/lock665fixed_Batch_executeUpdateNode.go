// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/batch_executor.go at b6fb6a4, lines 179-253, function Batch.executeUpdateNode.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (b *Batch) executeUpdateNode(op batchOp) error {
	// mmap mode: promote a base-resident node into the overlay (CoW) before mutating.
	node, err := b.graph.materializeNode(op.nodeID)
	if err != nil {
		return fmt.Errorf("node %d not found: %w", op.nodeID, err)
	}

	// Pre-mutation snapshot for the observer (must be captured before the merge).
	var oldNode *Node
	if b.haveObservers {
		oldNode = node.Clone()
	}

	if err := checkPatchWidths(node.Properties, op.properties, nil); err != nil {
		return err
	}
	// A node created with nil properties has no map to merge into.
	if node.Properties == nil && len(op.properties) > 0 {
		node.Properties = make(map[string]Value, len(op.properties))
	}

	// Update property indexes (remove old, add new). Gated on type-match,
	// mirroring updatePropertyIndexes: only type-matching values are indexed,
	// so a mismatched old value was never inserted (Remove would error
	// "not found") and a mismatched new value is not indexable (skip).
	for key, oldValue := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists && oldValue.Type == idx.indexType {
			if err := idx.Remove(node.ID, oldValue); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}

	// Update properties
	for key, value := range op.properties {
		node.Properties[key] = value
	}

	// Re-index
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists && value.Type == idx.indexType {
			if err := idx.Insert(node.ID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
	}

	// Re-index vectors for the updated node (parity with UpdateNode; G2: a batch
	// update of a vector property left the stale vector in the HNSW index).
	// Planned under the lock (validates dimension), applied off-lock in Commit.
	plans, err := b.graph.planNodeVectorInserts(node)
	if err != nil {
		return fmt.Errorf("failed to plan vectors for updated node %d: %w", op.nodeID, err)
	}
	b.vectorPlans = append(b.vectorPlans, plans...)
	if b.haveObservers {
		b.updatedForNotify = append(b.updatedForNotify, batchUpdateNotify{oldNode: oldNode, newNode: node.Clone()})
	}

	// Write to WAL for durability
	if b.graph.hasWAL() {
		updateData, err := json.Marshal(nodeUpdateRecord{
			NodeID:     op.nodeID,
			Properties: op.properties,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal node update %d for WAL: %w", op.nodeID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpUpdateNode, updateData); walErr != nil {
			return fmt.Errorf("failed to append node update to WAL: %w", walErr)
		}
	}

	return nil
}
