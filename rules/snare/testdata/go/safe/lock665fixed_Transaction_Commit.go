// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/transaction_commit.go at b6fb6a4, lines 30-87, function Transaction.Commit.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
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

	c, err := tx.applyLocked()
	if err != nil {
		return err
	}

	// (3) Atomic durability — one fsync for the whole batch. Propagate the
	// error: a commit that did not become durable must fail loudly.
	if err := tx.appendWALBarriered(c.walEntries); err != nil {
		return fmt.Errorf("commit: WAL durability: %w", err)
	}

	// (4) Off-lock: HNSW vector inserts, then observer dispatch.
	tx.gs.applyNodeVectorInserts(c.vectorPlans)
	if c.haveObservers {
		ctx := context.Background()
		for _, n := range c.createdForNotify {
			tx.gs.notifyNodeCreated(ctx, n)
		}
		for _, u := range c.updatesForNotify {
			tx.gs.notifyNodeUpdated(ctx, u.newNode, u.oldNode)
		}
	}

	return nil
}
