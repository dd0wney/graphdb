package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// Commit executes all batched operations atomically.
//
// Mirrors Transaction.Commit's structure: persist + maintain every index under
// gs.mu while COLLECTING the off-lock work (HNSW vector inserts, observer
// snapshots), then release the lock and apply that work off-lock (Track P H2 —
// HNSW inserts must not run under gs.mu; observers see committed state). The
// per-op WAL writes stay under the lock, unchanged.
func (b *Batch) Commit() error {
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

func (b *Batch) executeCreateNode(op batchOp) error {
	node := &Node{
		ID:     op.nodeID,
		Labels: op.labels,
		// Batch creation is tenant-blind, so nodes land in the default
		// tenant — matching CreateNode -> CreateNodeWithTenant(DefaultTenantID).
		// Without this stamp the node is owned by "" and every *ForTenant
		// reader (GetNodeForTenant, the edge TenantID filter) rejects it as
		// cross-tenant. (Track Q / Q3: bulk-imported data was invisible to
		// the tenant-strict API every current consumer uses.)
		TenantID:   DefaultTenantID,
		Properties: op.properties,
	}

	// Persist through the SHARED helper so the batch maintains every index the
	// canonical create does — shard, global label, per-tenant, property indexes,
	// stats, AND the vector-insert plan the inline batch code used to skip (G1:
	// batch-imported nodes were silently unsearchable). persistNodeLocked plans
	// vectors first, so a bad-dimension vector aborts before the WAL write below
	// (parity with Transaction.Commit). Vectors are applied off-lock in Commit.
	plans, err := b.graph.persistNodeLocked(node)
	if err != nil {
		return err
	}
	b.vectorPlans = append(b.vectorPlans, plans...)
	if b.haveObservers {
		b.createdForNotify = append(b.createdForNotify, node.Clone())
	}

	// Write to WAL for durability
	if b.graph.hasWAL() {
		nodeData, err := json.Marshal(node)
		if err != nil {
			return fmt.Errorf("failed to marshal node %d for WAL: %w", node.ID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpCreateNode, nodeData); walErr != nil {
			return fmt.Errorf("failed to append node to WAL: %w", walErr)
		}
	}

	return nil
}

func (b *Batch) executeCreateEdge(op batchOp) error {
	// Reject non-finite weight (#328) before any in-memory mutation — the WAL
	// marshal below would otherwise fail after the edge is already in the shard
	// map (a partial apply).
	if err := validateEdgeWeight(op.weight); err != nil {
		return err
	}
	edge := &Edge{
		ID:         op.edgeID,
		FromNodeID: op.fromNodeID,
		ToNodeID:   op.toNodeID,
		Type:       op.edgeType,
		// Same default-tenant stamp as executeCreateNode: the *ForTenant edge
		// readers filter the global adjacency by edge.TenantID, so an unstamped
		// ("") edge is silently dropped for the default tenant.
		TenantID:   DefaultTenantID,
		Properties: op.properties,
		Weight:     op.weight,
	}

	b.graph.storeEdgeInShard(edge)
	atomic.AddUint64(&b.graph.stats.EdgeCount, 1)

	// Update edge type index
	addToLabelIndex(b.graph.edgesByType, edge.Type, edge.ID)

	// Update adjacency lists
	b.graph.outgoingEdges[edge.FromNodeID] = append(b.graph.outgoingEdges[edge.FromNodeID], edge.ID)
	b.graph.incomingEdges[edge.ToNodeID] = append(b.graph.incomingEdges[edge.ToNodeID], edge.ID)

	// Maintain the per-tenant edge indexes (tenantEdgesByType + tenantEdgeIDs
	// + tenant stats), mirroring persistEdgeLocked -> addEdgeToTenantIndex.
	b.graph.addEdgeToTenantIndex(edge)

	// Write to WAL for durability
	if b.graph.hasWAL() {
		edgeData, err := json.Marshal(edge)
		if err != nil {
			return fmt.Errorf("failed to marshal edge %d for WAL: %w", edge.ID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpCreateEdge, edgeData); walErr != nil {
			return fmt.Errorf("failed to append edge to WAL: %w", walErr)
		}
	}

	return nil
}

func (b *Batch) executeUpdateNode(op batchOp) error {
	// mmap mode: promote a base-resident node into the overlay (CoW) before mutating.
	b.graph.lockShard(op.nodeID)
	node, exists := b.graph.materializeNodeLocked(op.nodeID)
	b.graph.unlockShard(op.nodeID)
	if !exists {
		return fmt.Errorf("node %d not found", op.nodeID)
	}

	// Pre-mutation snapshot for the observer (must be captured before the merge).
	var oldNode *Node
	if b.haveObservers {
		oldNode = node.Clone()
	}

	// Update property indexes (remove old, add new). Gated on type-match,
	// mirroring updatePropertyIndexes: only type-matching values are indexed,
	// so a mismatched old value was never inserted (Remove would error
	// "not found") and a mismatched new value is not indexable (skip).
	for key, oldValue := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists && oldValue.Type == idx.indexType {
			if err := idx.Remove(node.ID, oldValue); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}

	// Update properties
	for key, value := range op.properties {
		node.Properties[key] = value
	}

	// Re-index
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists && value.Type == idx.indexType {
			if err := idx.Insert(node.ID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
	}

	// Re-index vectors for the updated node (parity with UpdateNode; G2: a batch
	// update of a vector property left the stale vector in the HNSW index).
	// Planned under the lock (validates dimension), applied off-lock in Commit.
	plans, err := b.graph.planNodeVectorInserts(node)
	if err != nil {
		return fmt.Errorf("failed to plan vectors for updated node %d: %w", op.nodeID, err)
	}
	b.vectorPlans = append(b.vectorPlans, plans...)
	if b.haveObservers {
		b.updatedForNotify = append(b.updatedForNotify, batchUpdateNotify{oldNode: oldNode, newNode: node.Clone()})
	}

	// Write to WAL for durability
	if b.graph.hasWAL() {
		updateData, err := json.Marshal(struct {
			NodeID     uint64
			Properties map[string]Value
		}{
			NodeID:     op.nodeID,
			Properties: op.properties,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal node update %d for WAL: %w", op.nodeID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpUpdateNode, updateData); walErr != nil {
			return fmt.Errorf("failed to append node update to WAL: %w", walErr)
		}
	}

	return nil
}

func (b *Batch) executeDeleteNode(op batchOp) error {
	node, exists := b.graph.resolveNodeRefLocked(op.nodeID)
	if !exists {
		return nil // Skip non-existent nodes
	}

	// Remove from the global label index (O(1) per label), keeping empty
	// buckets so labels stay registered (see removeFromLabelIndex).
	for _, label := range node.Labels {
		removeFromLabelIndexKeepEmpty(b.graph.nodesByLabel, label, op.nodeID)
	}

	// Remove from the per-tenant indexes (label index + enumeration set +
	// tenant NodeCount), mirroring the non-batch DeleteNode. Without this the
	// *ForTenant readers and CountNodes*ForTenant keep the deleted node — the
	// delete-side counterpart of the #288 create-path gap (CC6). Pure index
	// maintenance: no WAL write (the single OpDeleteNode logged below drives
	// replay's own cascade via cascadeDelete*).
	b.graph.removeNodeFromTenantIndex(node)

	// Remove from property indexes. Gated on type-match (see executeUpdateNode):
	// a mismatched value was never indexed, so Remove would error "not found".
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists && value.Type == idx.indexType {
			if err := idx.Remove(op.nodeID, value); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}

	// Delete edges. Each cascaded edge must fully leave the indexes, matching
	// DeleteEdge: per-tenant edge index + tenant EdgeCount (removeEdgeFromTenant-
	// Index), the global edgesByType bucket, and the OPPOSITE endpoint's
	// adjacency list (this node's own adjacency is dropped wholesale below).
	// Look the edge up before deleting its shard entry so its Type/endpoints are
	// still available. Pure index maintenance: no per-edge WAL (the single
	// OpDeleteNode below drives replay's cascade). G3.
	// Use getEdgeIDsForNode so mmap mode picks up CSR-base edges (not just
	// the post-open overlay). JSON mode: mmapSnap == nil → falls back to
	// plain maps, behaviour unchanged.
	outgoing := b.graph.getEdgeIDsForNode(op.nodeID, true)
	for _, edgeID := range outgoing {
		if edge, ok := b.graph.resolveEdgeRefLocked(edgeID); ok {
			b.graph.removeEdgeFromTenantIndex(edge)
			removeFromLabelIndexKeepEmpty(b.graph.edgesByType, edge.Type, edgeID)
			// a->X: drop the edge from X's incoming adjacency overlay.
			b.graph.incomingEdges[edge.ToNodeID] = removeEdgeFromList(b.graph.incomingEdges[edge.ToNodeID], edgeID)
		}
		b.graph.deleteEdgeShardEntry(edgeID)
		b.graph.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	incoming := b.graph.getEdgeIDsForNode(op.nodeID, false)
	for _, edgeID := range incoming {
		if edge, ok := b.graph.resolveEdgeRefLocked(edgeID); ok {
			b.graph.removeEdgeFromTenantIndex(edge)
			removeFromLabelIndexKeepEmpty(b.graph.edgesByType, edge.Type, edgeID)
			// Y->a: drop the edge from Y's outgoing adjacency.
			b.graph.outgoingEdges[edge.FromNodeID] = removeEdgeFromList(b.graph.outgoingEdges[edge.FromNodeID], edgeID)
		}
		b.graph.deleteEdgeShardEntry(edgeID)
		b.graph.markEdgeDeletedLocked(edgeID) // mmap mode: mask the base-resident edge
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	delete(b.graph.outgoingEdges, op.nodeID)
	delete(b.graph.incomingEdges, op.nodeID)

	// Delete node with atomic decrement
	b.graph.deleteNodeShardEntry(op.nodeID)
	b.graph.markNodeDeletedLocked(op.nodeID) // mmap mode: mask the base-resident node
	atomicDecrementWithUnderflowProtection(&b.graph.stats.NodeCount)

	// Drop the node's vectors from the HNSW index off-lock in Commit (parity
	// with DeleteNode's RemoveNodeFromVectorIndexes; E). Wholesale per-node
	// (the whole node is gone), unlike RemoveNodeProperties' per-key removal.
	// Gated on haveVectorIndex so non-vector batches allocate nothing.
	if b.haveVectorIndex {
		b.vectorNodeDeletes = append(b.vectorNodeDeletes, batchDeleteNotify{id: op.nodeID, tenantID: node.TenantID})
	}

	// Dispatch OnNodeDeleted off-lock in Commit (parity with DeleteNode; G4).
	if b.haveObservers {
		b.deletedForNotify = append(b.deletedForNotify, batchDeleteNotify{id: op.nodeID, tenantID: node.TenantID})
	}

	// Write to WAL for durability
	if b.graph.hasWAL() {
		nodeData, err := json.Marshal(node)
		if err != nil {
			return fmt.Errorf("failed to marshal node deletion %d for WAL: %w", op.nodeID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpDeleteNode, nodeData); walErr != nil {
			return fmt.Errorf("failed to append node deletion to WAL: %w", walErr)
		}
	}

	return nil
}

func (b *Batch) executeDeleteEdge(op batchOp) error {
	edge, exists := b.graph.resolveEdgeRefLocked(op.edgeID)
	if !exists {
		return nil // Skip non-existent edges
	}

	// Remove from the global type index (O(1)), keeping empty buckets so the
	// type stays registered (see removeEdgeFromTypeIndex).
	removeFromLabelIndexKeepEmpty(b.graph.edgesByType, edge.Type, op.edgeID)

	// Remove from the per-tenant edge index + tenant EdgeCount, mirroring the
	// non-batch DeleteEdge — the #288 create-path gap on the delete side (CC6).
	// Pure index maintenance; the OpDeleteEdge WAL write below is unchanged.
	b.graph.removeEdgeFromTenantIndex(edge)

	// Remove from adjacency lists
	outgoing := b.graph.outgoingEdges[edge.FromNodeID]
	for i, id := range outgoing {
		if id == op.edgeID {
			b.graph.outgoingEdges[edge.FromNodeID] = append(outgoing[:i], outgoing[i+1:]...)
			break
		}
	}

	incoming := b.graph.incomingEdges[edge.ToNodeID]
	for i, id := range incoming {
		if id == op.edgeID {
			b.graph.incomingEdges[edge.ToNodeID] = append(incoming[:i], incoming[i+1:]...)
			break
		}
	}

	// Delete edge with atomic decrement
	b.graph.deleteEdgeShardEntry(op.edgeID)
	b.graph.markEdgeDeletedLocked(op.edgeID) // mmap mode: mask the base-resident edge
	atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)

	// Write to WAL for durability
	if b.graph.hasWAL() {
		edgeData, err := json.Marshal(edge)
		if err != nil {
			return fmt.Errorf("failed to marshal edge deletion %d for WAL: %w", op.edgeID, err)
		}
		if walErr := b.graph.appendToWAL(wal.OpDeleteEdge, edgeData); walErr != nil {
			return fmt.Errorf("failed to append edge deletion to WAL: %w", walErr)
		}
	}

	return nil
}
