// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/batch_executor.go at b6fb6a4, lines 51-81, function Batch.executeOps.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// executeOps runs every queued operation under gs.mu. The deferred unlock
// matters: the Cypher executor and net/http recover panics, so a panic in an
// operation that left gs.mu held would hang every later write, Close included.
func (b *Batch) executeOps() error {
	b.graph.mu.Lock()
	defer b.graph.mu.Unlock()
	panicPoint("Batch.Commit")

	b.haveObservers = len(b.graph.observers) > 0
	b.haveVectorIndex = b.graph.vectorIndex.HasAnyIndex()

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
			return err
		}
	}
	return nil
}
