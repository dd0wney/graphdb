// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 580-613, function GraphStorage.patchNode.
// Label: safe.
// Reason: The function after PR #665. The lock section moved into a helper with a deferred unlock, or the explicit unlock is gone.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// patchNode is the one node property write path: UpdateNode, RemoveNodeProperties
// and PatchNodeForTenant all come here, so the locks, the indexes, the vector
// work and the WAL record cannot drift apart between them.
//
// Lock discipline (R2.1, S11 spike §7.4): notifyNodeUpdated dispatches
// after gs.mu.Lock is released. The oldNode / newNode clones are taken
// inside the lock window (when the live shard pointer is safe) and are
// only allocated when observers are registered — observerless callers pay
// zero clone cost.
func (gs *GraphStorage) patchNode(nodeID uint64, set map[string]Value, remove []string) error {
	p, err := gs.applyNodePatch(nodeID, set, remove)
	if err != nil {
		return err
	}

	// HNSW work off-lock (Track P item 3 / H2), before the WAL wait and
	// observer dispatch so the index matches the node before observers act.
	// RemoveVectorForTenant is idempotent; its errors are logged fail-soft
	// (the property is already removed, and the index is rebuilt from the
	// node set on restart).
	for _, prop := range p.vectorRemovals {
		if err := gs.vectorIndex.RemoveVectorForTenant(p.tid, prop, nodeID); err != nil {
			log.Printf("node_operations: vector index removal failed for prop %q node %d tenant %s: %v", prop, nodeID, p.tid, err)
		}
	}
	gs.applyNodeVectorInserts(p.vectorPlans)
	// A wait error wraps ErrWALWriteFailed; the update stays applied and
	// observers are still notified.
	err = gs.waitWALPending(wal.OpUpdateNode, p.walPending)
	if p.newNode != nil {
		gs.notifyNodeUpdated(context.Background(), p.newNode, p.oldNode)
	}
	return err
}
