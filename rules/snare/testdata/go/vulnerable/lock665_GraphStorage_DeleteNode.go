// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 787-902, function GraphStorage.DeleteNode.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// DeleteNode deletes a node and all its edges.
//
// Tenant-blind. New callers should prefer DeleteNodeForTenant.
//
// Lock discipline (R2.1, S11 spike §7.4): defer-unlock was replaced with
// explicit unlock at every return path so notifyNodeDeleted can dispatch
// strictly after gs.mu.Lock is released. The deleted node's TenantID is
// captured under lock (from the lookup at line 514) and passed to the
// notify call after unlock — the node's data is not accessible by then.
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()

	// resolve overlay → base; the node's fields drive index removal below.
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		gs.mu.Unlock()
		return err
	}

	// Capture for OnNodeDeleted dispatch after unlock. node.TenantID is
	// stable for the lifetime of the node (immutable after creation).
	tenantID := node.TenantID

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		var err error
		outgoingEdgeIDs, err = gs.edgeStore.GetOutgoingEdges(nodeID)
		if err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		// Use getEdgeIDsForNode so that mmap mode picks up CSR-base edges
		// (not just the post-open overlay). In JSON mode mmapSnap == nil and
		// the helper falls back to the plain maps, so behaviour is unchanged.
		outgoingEdgeIDs = gs.getEdgeIDsForNode(nodeID, true)
		incomingEdgeIDs = gs.getEdgeIDsForNode(nodeID, false)
	}

	// Cascade delete all outgoing edges
	for _, edgeID := range outgoingEdgeIDs {
		if err := gs.cascadeDeleteOutgoingEdge(edgeID); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to cascade delete outgoing edge %d: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to cascade delete incoming edge %d: %w", edgeID, err)
		}
	}

	// Remove from global label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, nodeID)
	}

	// Remove from tenant-scoped indexes
	gs.removeNodeFromTenantIndex(node)

	// Remove from property indexes
	if err := gs.removeNodeFromPropertyIndexes(nodeID, node.Properties); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Remove from vector indexes (R1.2: routes by node.TenantID; empty
	// TenantID on legacy tenant-blind nodes falls back to tenantid.Default
	// inside RemoveNodeFromVectorIndexes).
	if err := gs.RemoveNodeFromVectorIndexes(nodeID, node.TenantID); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Delete node — per-shard write lock (A4) excludes shard.RLock
	// readers during the nodeShards delete. The cascade work above
	// (label/property/vector index removal, edge cascades) all touches
	// global structures under the gs.mu.Lock that's held throughout
	// this function.
	// ruleid: graphdb-lock-call-without-defer
	gs.lockShard(nodeID)
	gs.deleteNodeShardEntry(nodeID)
	gs.markNodeDeletedLocked(nodeID) // mmap mode: mask the base-resident node
	gs.unlockShard(nodeID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(nodeID); err != nil {
		gs.mu.Unlock()
		return fmt.Errorf("failed to clear adjacency for node %d: %w", nodeID, err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	// Enqueue to WAL under gs.mu (preserves WAL order); wait on durability after
	// releasing gs.mu so concurrent writers can fill the batch (Track P item 1).
	walPending := gs.enqueueWAL(wal.OpDeleteNode, node)

	gs.mu.Unlock()

	// A wait error wraps ErrWALWriteFailed; the delete stays applied and
	// observers are still notified.
	err = gs.waitWALPending(wal.OpDeleteNode, walPending)
	// R2.1: dispatch after lock release. See lock-discipline comment in
	// pkg/storage/observation.go.
	gs.notifyNodeDeleted(context.Background(), nodeID, tenantID)
	return err
}
