// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 543-666, function GraphStorage.patchNode.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
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
	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()

	// mmap mode: promote a base-resident node into the shard overlay (copy-on-write)
	// before the in-place mutation below. No-op (plain lookup) when mmap is off.
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(nodeID)
	node, err := gs.materializeNodeLocked(nodeID)
	gs.unlockShard(nodeID)
	if err != nil {
		gs.mu.Unlock()
		return err
	}

	// R2.1: snapshot pre-update state for observer dispatch. Only allocate
	// when observers are registered.
	var oldNode *Node
	if len(gs.observers) > 0 {
		oldNode = node.Clone()
	}

	if err := checkPatchWidths(node.Properties, set, remove); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Plan vector-index inserts before anything changes, on a preview of the
	// patched properties, so a bad vector refuses the whole patch and leaves
	// the node, its indexes and the HNSW index as they were. The expensive
	// HNSW remove+add runs after the lock is released (Track P item 3 / H2);
	// the plan holds decoded copies of the vectors, which is what makes that
	// off-lock insert race-free. A pure removal sets nothing, so it re-inserts
	// nothing, and with no vector index at all there is nothing to plan, so
	// the hot path skips the preview copy.
	var vectorPlans []vectorInsertPlan
	if len(set) > 0 && gs.vectorIndex.HasAnyIndex() {
		preview := &Node{ID: node.ID, TenantID: node.TenantID, Properties: patchedProperties(node.Properties, set, remove)}
		vectorPlans, err = gs.planNodeVectorInserts(preview)
		if err != nil {
			gs.mu.Unlock()
			return err
		}
	}

	// Update property indexes (global structures — under gs.mu.Lock).
	if err := gs.updatePropertyIndexes(nodeID, node, set); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Per-shard write lock (A4) excludes shard.RLock readers during the
	// in-place Node-struct mutation that follows. The removals walk
	// node.Properties for the index Remove calls before they delete, so
	// they stay inside the same window.
	//
	// A removed vector-indexed property must also leave the HNSW index, or
	// VectorSearch keeps returning the stale vector. HasIndexForTenant reads
	// the index map, so the removals are planned here and applied off-lock,
	// per key, so the node's other vector properties are untouched.
	tid := effectiveTenantID(node.TenantID)
	var vectorRemovals []string
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(nodeID)
	// A node created with nil properties has no map to write into.
	if node.Properties == nil && len(set) > 0 {
		node.Properties = make(map[string]Value, len(set))
	}
	for k, v := range set {
		node.Properties[k] = v
	}
	removed := gs.deleteNodePropertiesLocked(nodeID, node, remove)
	for _, key := range removed {
		if gs.vectorIndex.HasIndexForTenant(tid, key) {
			vectorRemovals = append(vectorRemovals, key)
		}
	}
	node.UpdatedAt = time.Now().Unix()
	gs.unlockShard(nodeID)

	// Enqueue to WAL under gs.mu (preserves WAL order); wait on durability
	// after releasing gs.mu so concurrent writers can fill the batch (group
	// commit, Track P item 1). The record names the removed keys, not the
	// keys that remain: replay merges Properties into the snapshot copy, so a
	// map cannot express a removal.
	walPending := gs.enqueueWAL(wal.OpUpdateNode, nodeUpdateRecord{
		NodeID:     nodeID,
		Properties: set,
		Removed:    removed,
	})

	// R2.1: snapshot post-update state before releasing the lock so the
	// observer sees a consistent view.
	var newNode *Node
	if oldNode != nil {
		newNode = node.Clone()
	}
	gs.mu.Unlock()

	// HNSW work off-lock (Track P item 3 / H2), before the WAL wait and
	// observer dispatch so the index matches the node before observers act.
	// RemoveVectorForTenant is idempotent; its errors are logged fail-soft
	// (the property is already removed, and the index is rebuilt from the
	// node set on restart).
	for _, prop := range vectorRemovals {
		if err := gs.vectorIndex.RemoveVectorForTenant(tid, prop, nodeID); err != nil {
			log.Printf("node_operations: vector index removal failed for prop %q node %d tenant %s: %v", prop, nodeID, tid, err)
		}
	}
	gs.applyNodeVectorInserts(vectorPlans)
	// A wait error wraps ErrWALWriteFailed; the update stays applied and
	// observers are still notified.
	err = gs.waitWALPending(wal.OpUpdateNode, walPending)
	if newNode != nil {
		gs.notifyNodeUpdated(context.Background(), newNode, oldNode)
	}
	return err
}
