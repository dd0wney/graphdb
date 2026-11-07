// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/batch_executor.go at b6fb6a4, lines 12-49, function Batch.Commit.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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
	if err := b.executeOps(); err != nil {
		return err
	}

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
