// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/transaction_commit.go at b6fb6a4, lines 228-237, function Transaction.appendWALBarriered.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// appendWALBarriered appends the commit's WAL batch and then releases the
// txWALBarrier read lock that applyLocked returned with. The release is
// deferred, so a panic in the WAL or in encryption cannot leave the barrier
// held: walLSNBarrieredLocked would then block on it while holding gs.mu, and
// Snapshot and Close would hang.
func (tx *Transaction) appendWALBarriered(entries []wal.BatchEntry) error {
	defer tx.gs.txWALBarrier.RUnlock()
	panicPoint("Transaction.Commit.wal")
	return tx.gs.noteWALWriteError(tx.gs.appendWALBatch(entries))
}
