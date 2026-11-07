// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/transaction_commit.go at b6fb6a4, lines 100-209, function Transaction.applyLocked.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// applyLocked is steps 1 and 2 of Commit, under gs.mu. The deferred unlock
// matters: the Cypher executor and net/http recover panics, so a panic here
// that left gs.mu held would hang every later write, Close included.
//
// On success it returns with txWALBarrier read-held, and the caller must
// release it through appendWALBarriered. The barrier is taken before gs.mu is
// released: a CompactWAL boundary captured in between would see this commit's
// state in the snapshot while its entries are still unappended (LSN >
// boundary), and the surviving WAL would re-apply them over the snapshot on
// recovery (M-1). The order cannot be reversed either, because
// walLSNBarrieredLocked takes the barrier while it holds gs.mu.
func (tx *Transaction) applyLocked() (txCommit, error) {
	tx.gs.mu.Lock()
	defer tx.gs.mu.Unlock()
	panicPoint("Transaction.Commit")

	// (1) Validate references before any mutation (all-or-none).
	if err := tx.validateLocked(); err != nil {
		return txCommit{}, err
	}

	c := txCommit{
		walEntries:    make([]wal.BatchEntry, 0, len(tx.createdNodes)+len(tx.createdEdges)+len(tx.updatedNodes)),
		haveObservers: len(tx.gs.observers) > 0,
	}

	// (2a) Created nodes — through the shared persist helper (indexes, stats,
	// vector plan), then a WAL entry. Iterate in creation (ascending-ID) order so
	// the HNSW vector-insert order is deterministic (see sortedTxIDs).
	for _, nodeID := range sortedTxIDs(tx.createdNodes) {
		node := tx.createdNodes[nodeID]
		plans, err := tx.gs.persistNodeLocked(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: persist node %d: %w", node.ID, err)
		}
		c.vectorPlans = append(c.vectorPlans, plans...)
		data, err := json.Marshal(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal node %d: %w", node.ID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpCreateNode, Data: data})
		if c.haveObservers {
			c.createdForNotify = append(c.createdForNotify, node.Clone())
		}
	}

	// (2b) Created edges — endpoints are now persisted (or pre-existing).
	// Ascending-ID order for deterministic persist + WAL ordering.
	for _, edgeID := range sortedTxIDs(tx.createdEdges) {
		edge := tx.createdEdges[edgeID]
		if err := tx.gs.persistEdgeLocked(edge); err != nil {
			return txCommit{}, fmt.Errorf("commit: persist edge %d: %w", edge.ID, err)
		}
		data, err := json.Marshal(edge)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal edge %d: %w", edge.ID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpCreateEdge, Data: data})
	}

	// (2c) Property updates to existing nodes. (Updates to nodes created in this
	// same transaction were merged into the buffered node by tx.UpdateNode, so
	// they ride the OpCreateNode entry above — updatedNodes holds only existing-
	// node updates.)
	for _, nodeID := range sortedTxIDs(tx.updatedNodes) {
		props := tx.updatedNodes[nodeID]
		// mmap mode: promote a base-resident node into the overlay (CoW) before
		// the property-index update + in-place mutation below.
		node, err := tx.gs.materializeNode(nodeID)
		if err != nil {
			// validateLocked guaranteed existence + ownership; defensive only.
			return txCommit{}, fmt.Errorf("commit: update target %d vanished: %w", nodeID, err)
		}
		var oldNode *Node
		if c.haveObservers {
			oldNode = node.Clone()
		}
		// Maintain property indexes BEFORE mutating node.Properties — the helper
		// reads the old value off the live node to Remove it, then Inserts the
		// new (mirrors the direct UpdateNode path). Omitting this left the
		// property index stale after a transaction update of an existing node
		// (the per-tenant-index/#288 class, in the dormant transaction path).
		if err := tx.gs.updatePropertyIndexes(nodeID, node, props); err != nil {
			return txCommit{}, fmt.Errorf("commit: update property indexes for node %d: %w", nodeID, err)
		}
		tx.gs.writeTxNodeUpdate(node, props)

		// Re-index vectors for the updated node (parity with the direct
		// UpdateNode path).
		plans, err := tx.gs.planNodeVectorInserts(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: plan vectors for updated node %d: %w", nodeID, err)
		}
		c.vectorPlans = append(c.vectorPlans, plans...)

		data, err := json.Marshal(nodeUpdateRecord{NodeID: nodeID, Properties: props})
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal update %d: %w", nodeID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpUpdateNode, Data: data})
		if c.haveObservers {
			c.updatesForNotify = append(c.updatesForNotify, txUpdateNotify{oldNode: oldNode, newNode: node.Clone()})
		}
	}

	tx.committed = true
	tx.active = false
	tx.gs.txWALBarrier.RLock()
	return c, nil
}
