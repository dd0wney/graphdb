// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 624-692, function GraphStorage.applyNodePatch.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// applyNodePatch is the part of patchNode that runs under gs.mu. The deferred
// unlock matters: the Cypher executor and net/http recover panics, so a panic
// here that left gs.mu held would hang every later write, Close included.
func (gs *GraphStorage) applyNodePatch(nodeID uint64, set map[string]Value, remove []string) (nodePatch, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("patchNode")

	// mmap mode: promote a base-resident node into the shard overlay (copy-on-write)
	// before the in-place mutation below. No-op (plain lookup) when mmap is off.
	node, err := gs.materializeNode(nodeID)
	if err != nil {
		return nodePatch{}, err
	}

	// R2.1: snapshot pre-update state for observer dispatch. Only allocate
	// when observers are registered.
	var p nodePatch
	if len(gs.observers) > 0 {
		p.oldNode = node.Clone()
	}

	if err := checkPatchWidths(node.Properties, set, remove); err != nil {
		return nodePatch{}, err
	}

	// Plan vector-index inserts before anything changes, on a preview of the
	// patched properties, so a bad vector refuses the whole patch and leaves
	// the node, its indexes and the HNSW index as they were. The expensive
	// HNSW remove+add runs after the lock is released (Track P item 3 / H2);
	// the plan holds decoded copies of the vectors, which is what makes that
	// off-lock insert race-free. A pure removal sets nothing, so it re-inserts
	// nothing, and with no vector index at all there is nothing to plan, so
	// the hot path skips the preview copy.
	if len(set) > 0 && gs.vectorIndex.HasAnyIndex() {
		preview := &Node{ID: node.ID, TenantID: node.TenantID, Properties: patchedProperties(node.Properties, set, remove)}
		p.vectorPlans, err = gs.planNodeVectorInserts(preview)
		if err != nil {
			return nodePatch{}, err
		}
	}

	// Update property indexes (global structures — under gs.mu.Lock).
	if err := gs.updatePropertyIndexes(nodeID, node, set); err != nil {
		return nodePatch{}, err
	}

	p.tid = effectiveTenantID(node.TenantID)
	var removed []string
	removed, p.vectorRemovals = gs.writeNodePatchShard(node, set, remove, p.tid)

	// Enqueue to WAL under gs.mu (preserves WAL order); wait on durability
	// after releasing gs.mu so concurrent writers can fill the batch (group
	// commit, Track P item 1). The record names the removed keys, not the
	// keys that remain: replay merges Properties into the snapshot copy, so a
	// map cannot express a removal.
	p.walPending = gs.enqueueWAL(wal.OpUpdateNode, nodeUpdateRecord{
		NodeID:     nodeID,
		Properties: set,
		Removed:    removed,
	})

	// R2.1: snapshot post-update state before releasing the lock so the
	// observer sees a consistent view.
	if p.oldNode != nil {
		p.newNode = node.Clone()
	}
	return p, nil
}
