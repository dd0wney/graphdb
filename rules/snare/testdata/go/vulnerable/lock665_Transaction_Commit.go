// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/transaction_commit.go at b6fb6a4^, lines 30-203, function Transaction.Commit.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// Commit applies all buffered changes atomically and durably.
//
//  1. Validate the whole buffer (edge endpoints + update targets resolve to
//     this tenant) BEFORE mutating anything — a bad reference aborts with
//     nothing applied (all-or-none for reference errors).
//  2. Under gs.mu, persist created nodes → created edges → property updates via
//     the SHARED persist*Locked helpers (the same shard/global/tenant/vector/
//     property indexes + stats the direct write paths maintain), collecting the
//     WAL entries and vector plans.
//  3. Release gs.mu, then make the whole batch durable with a SINGLE fsync
//     (all-or-none) via appendWALBatch. Durability is always atomic: appendWAL-
//     Batch is the last step, so any earlier error writes no WAL and nothing
//     becomes durable.
//  4. Off-lock: apply the HNSW vector inserts, then dispatch observer
//     notifications (so auto-embed observers see committed nodes).
//
// Commit serializes on gs.mu (last-writer-wins; no conflict detection between
// concurrent transactions). Limitation: a malformed-vector error (wrong
// dimension) during step 2 aborts the commit and writes no WAL — so nothing is
// durable — but may leave a transient, non-durable in-memory partial (the same
// behaviour the direct create/update paths have on a mid-apply error). It is
// cleaned up by the absence of a WAL record on the next restart.
func (tx *Transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}
	if tx.committed || tx.rolledBack {
		return ErrTransactionAlreadyEnded
	}

	// ruleid: graphdb-lock-call-without-defer
	tx.gs.mu.Lock()

	// (1) Validate references before any mutation (all-or-none).
	if err := tx.validateLocked(); err != nil {
		tx.gs.mu.Unlock()
		return err
	}

	walEntries := make([]wal.BatchEntry, 0, len(tx.createdNodes)+len(tx.createdEdges)+len(tx.updatedNodes))
	var vectorPlans []vectorInsertPlan
	haveObservers := len(tx.gs.observers) > 0
	var createdForNotify []*Node
	type updateNotify struct{ oldNode, newNode *Node }
	var updatesForNotify []updateNotify

	// (2a) Created nodes — through the shared persist helper (indexes, stats,
	// vector plan), then a WAL entry. Iterate in creation (ascending-ID) order so
	// the HNSW vector-insert order is deterministic (see sortedTxIDs).
	for _, nodeID := range sortedTxIDs(tx.createdNodes) {
		node := tx.createdNodes[nodeID]
		plans, err := tx.gs.persistNodeLocked(node)
		if err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: persist node %d: %w", node.ID, err)
		}
		vectorPlans = append(vectorPlans, plans...)
		data, err := json.Marshal(node)
		if err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: marshal node %d: %w", node.ID, err)
		}
		walEntries = append(walEntries, wal.BatchEntry{OpType: wal.OpCreateNode, Data: data})
		if haveObservers {
			createdForNotify = append(createdForNotify, node.Clone())
		}
	}

	// (2b) Created edges — endpoints are now persisted (or pre-existing).
	// Ascending-ID order for deterministic persist + WAL ordering.
	for _, edgeID := range sortedTxIDs(tx.createdEdges) {
		edge := tx.createdEdges[edgeID]
		if err := tx.gs.persistEdgeLocked(edge); err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: persist edge %d: %w", edge.ID, err)
		}
		data, err := json.Marshal(edge)
		if err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: marshal edge %d: %w", edge.ID, err)
		}
		walEntries = append(walEntries, wal.BatchEntry{OpType: wal.OpCreateEdge, Data: data})
	}

	// (2c) Property updates to existing nodes. (Updates to nodes created in this
	// same transaction were merged into the buffered node by tx.UpdateNode, so
	// they ride the OpCreateNode entry above — updatedNodes holds only existing-
	// node updates.)
	for _, nodeID := range sortedTxIDs(tx.updatedNodes) {
		props := tx.updatedNodes[nodeID]
		// mmap mode: promote a base-resident node into the overlay (CoW) before
		// the property-index update + in-place mutation below.
		// ruleid: graphdb-lock-call-without-defer
		tx.gs.lockShard(nodeID)
		node, err := tx.gs.materializeNodeLocked(nodeID)
		tx.gs.unlockShard(nodeID)
		if err != nil {
			// validateLocked guaranteed existence + ownership; defensive only.
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: update target %d vanished: %w", nodeID, err)
		}
		var oldNode *Node
		if haveObservers {
			oldNode = node.Clone()
		}
		// Maintain property indexes BEFORE mutating node.Properties — the helper
		// reads the old value off the live node to Remove it, then Inserts the
		// new (mirrors the direct UpdateNode path). Omitting this left the
		// property index stale after a transaction update of an existing node
		// (the per-tenant-index/#288 class, in the dormant transaction path).
		if err := tx.gs.updatePropertyIndexes(nodeID, node, props); err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: update property indexes for node %d: %w", nodeID, err)
		}
		// ruleid: graphdb-lock-call-without-defer
		tx.gs.lockShard(nodeID)
		for k, v := range props {
			node.Properties[k] = v
		}
		node.UpdatedAt = time.Now().Unix()
		tx.gs.unlockShard(nodeID)

		// Re-index vectors for the updated node (parity with the direct
		// UpdateNode path).
		plans, err := tx.gs.planNodeVectorInserts(node)
		if err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: plan vectors for updated node %d: %w", nodeID, err)
		}
		vectorPlans = append(vectorPlans, plans...)

		data, err := json.Marshal(nodeUpdateRecord{NodeID: nodeID, Properties: props})
		if err != nil {
			tx.gs.mu.Unlock()
			return fmt.Errorf("commit: marshal update %d: %w", nodeID, err)
		}
		walEntries = append(walEntries, wal.BatchEntry{OpType: wal.OpUpdateNode, Data: data})
		if haveObservers {
			updatesForNotify = append(updatesForNotify, updateNotify{oldNode: oldNode, newNode: node.Clone()})
		}
	}

	tx.committed = true
	tx.active = false
	// Hold the commit barrier from before gs.mu is released until the WAL
	// batch has been appended: a CompactWAL boundary captured in between
	// would see this commit's state in the snapshot while its entries are
	// still unappended (LSN > boundary), and the surviving WAL would
	// re-apply them over the snapshot on recovery (M-1).
	// ruleid: graphdb-lock-call-without-defer
	tx.gs.txWALBarrier.RLock()
	tx.gs.mu.Unlock()

	// (3) Atomic durability — one fsync for the whole batch. Propagate the
	// error: a commit that did not become durable must fail loudly.
	walErr := tx.gs.noteWALWriteError(tx.gs.appendWALBatch(walEntries))
	tx.gs.txWALBarrier.RUnlock()
	if walErr != nil {
		return fmt.Errorf("commit: WAL durability: %w", walErr)
	}

	// (4) Off-lock: HNSW vector inserts, then observer dispatch.
	tx.gs.applyNodeVectorInserts(vectorPlans)
	if haveObservers {
		ctx := context.Background()
		for _, n := range createdForNotify {
			tx.gs.notifyNodeCreated(ctx, n)
		}
		for _, u := range updatesForNotify {
			tx.gs.notifyNodeUpdated(ctx, u.newNode, u.oldNode)
		}
	}

	return nil
}
