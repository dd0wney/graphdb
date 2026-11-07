// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 881-973, function GraphStorage.deleteNodeUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// deleteNodeUnderLock is the part of DeleteNode that runs under gs.mu. It
// returns the WAL handle to wait on and the deleted node's TenantID for the
// observer dispatch, both used after the unlock. The deferred unlock matters:
// the Cypher executor and net/http recover panics, so a panic here that left
// gs.mu held would hang every later write, Close included.
func (gs *GraphStorage) deleteNodeUnderLock(nodeID uint64) (*wal.Pending, string, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("DeleteNode")

	// resolve overlay → base; the node's fields drive index removal below.
	node, err := gs.resolveNodeRefLocked(nodeID)
	if err != nil {
		return nil, "", err
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
			return nil, "", fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
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
			return nil, "", fmt.Errorf("failed to cascade delete outgoing edge %d: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			return nil, "", fmt.Errorf("failed to cascade delete incoming edge %d: %w", edgeID, err)
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
		return nil, "", err
	}

	// Remove from vector indexes (R1.2: routes by node.TenantID; empty
	// TenantID on legacy tenant-blind nodes falls back to tenantid.Default
	// inside RemoveNodeFromVectorIndexes).
	if err := gs.RemoveNodeFromVectorIndexes(nodeID, node.TenantID); err != nil {
		return nil, "", err
	}

	// Delete node — per-shard write lock (A4) excludes shard.RLock
	// readers during the nodeShards delete. The cascade work above
	// (label/property/vector index removal, edge cascades) all touches
	// global structures under the gs.mu.Lock that's held throughout
	// this function.
	gs.removeNodeFromShard(nodeID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(nodeID); err != nil {
		return nil, "", fmt.Errorf("failed to clear adjacency for node %d: %w", nodeID, err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	// Enqueue to WAL under gs.mu (preserves WAL order); wait on durability after
	// releasing gs.mu so concurrent writers can fill the batch (Track P item 1).
	return gs.enqueueWAL(wal.OpDeleteNode, node), tenantID, nil
}
