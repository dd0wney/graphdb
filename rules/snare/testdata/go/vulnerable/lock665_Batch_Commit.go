// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/batch_executor.go at b6fb6a4^, lines 12-73, function Batch.Commit.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// Commit executes all batched operations atomically.
//
// Mirrors Transaction.Commit's structure: persist + maintain every index under
// gs.mu while COLLECTING the off-lock work (HNSW vector inserts, observer
// snapshots), then release the lock and apply that work off-lock (Track P H2 —
// HNSW inserts must not run under gs.mu; observers see committed state). The
// per-op WAL writes stay under the lock, unchanged.
func (b *Batch) Commit() error {
	// ruleid: graphdb-lock-call-without-defer
	b.graph.mu.Lock()

	b.haveObservers = len(b.graph.observers) > 0
	b.haveVectorIndex = b.graph.vectorIndex.HasAnyIndex()

	// Execute all operations
	for _, op := range b.ops {
		var err error
		switch op.opType {
		case opCreateNode:
			err = b.executeCreateNode(op)
		case opCreateEdge:
			err = b.executeCreateEdge(op)
		case opUpdateNode:
			err = b.executeUpdateNode(op)
		case opDeleteNode:
			err = b.executeDeleteNode(op)
		case opDeleteEdge:
			err = b.executeDeleteEdge(op)
		}
		if err != nil {
			b.graph.mu.Unlock()
			return err
		}
	}

	b.graph.mu.Unlock()

	// Off-lock: apply the collected HNSW vector inserts + node-delete vector
	// removals, then dispatch observer notifications, so auto-embed / event hooks
	// see committed nodes (parity with the direct paths + Transaction.Commit).
	// HNSW mutation must not run under gs.mu (Track P H2); RemoveNodeFromVector-
	// Indexes only takes vi.mu, so it is safe here.
	b.graph.applyNodeVectorInserts(b.vectorPlans)
	for _, d := range b.vectorNodeDeletes {
		// Errors are swallowed inside RemoveNodeFromVectorIndexes (a node need
		// not be in every index); it returns nil today, so this stays fail-soft.
		_ = b.graph.RemoveNodeFromVectorIndexes(d.id, d.tenantID)
	}
	if b.haveObservers {
		ctx := context.Background()
		for _, n := range b.createdForNotify {
			b.graph.notifyNodeCreated(ctx, n)
		}
		for _, u := range b.updatedForNotify {
			b.graph.notifyNodeUpdated(ctx, u.newNode, u.oldNode)
		}
		for _, d := range b.deletedForNotify {
			b.graph.notifyNodeDeleted(ctx, d.id, d.tenantID)
		}
	}

	return nil
}
