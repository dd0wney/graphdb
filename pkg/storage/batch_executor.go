package storage

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// Commit executes all batched operations atomically
func (b *Batch) Commit() error {
	b.graph.mu.Lock()
	defer b.graph.mu.Unlock()

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
			return err
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

	b.graph.storeNodeInShard(node)
	atomic.AddUint64(&b.graph.stats.NodeCount, 1)

	// Update label indexes
	for _, label := range node.Labels {
		addToLabelIndex(b.graph.nodesByLabel, label, node.ID)
	}

	// Maintain the per-tenant indexes (tenantNodesByLabel + tenantNodeIDs +
	// tenant stats) exactly as the normal create path does
	// (persistNodeLocked -> addNodeToTenantIndex). The legacy global
	// nodesByLabel above is not what GetNodesByLabelForTenant reads.
	b.graph.addNodeToTenantIndex(node)

	// Update property indexes
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists {
			if err := idx.Insert(node.ID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
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
	node, exists := b.graph.lookupNodeShard(op.nodeID)
	if !exists {
		return fmt.Errorf("node %d not found", op.nodeID)
	}

	// Update property indexes (remove old, add new)
	for key, oldValue := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists {
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
		if idx, exists := b.graph.propertyIndexes[key]; exists {
			if err := idx.Insert(node.ID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
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
	node, exists := b.graph.lookupNodeShard(op.nodeID)
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

	// Remove from property indexes
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists {
			if err := idx.Remove(op.nodeID, value); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}

	// Delete edges. Each cascaded edge must also leave the per-tenant edge
	// index + tenant EdgeCount (removeEdgeFromTenantIndex), the edge analogue of
	// the node fix above. Look the edge up before deleting its shard entry so
	// its Type/TenantID are still available. (The global edgesByType bucket and
	// the opposite endpoint's adjacency list are left as-is — a separate,
	// read-self-healing index-hygiene gap, not the tenant-visibility bug here.)
	outgoing := b.graph.outgoingEdges[op.nodeID]
	for _, edgeID := range outgoing {
		if edge, ok := b.graph.lookupEdgeShard(edgeID); ok {
			b.graph.removeEdgeFromTenantIndex(edge)
		}
		b.graph.deleteEdgeShardEntry(edgeID)
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	incoming := b.graph.incomingEdges[op.nodeID]
	for _, edgeID := range incoming {
		if edge, ok := b.graph.lookupEdgeShard(edgeID); ok {
			b.graph.removeEdgeFromTenantIndex(edge)
		}
		b.graph.deleteEdgeShardEntry(edgeID)
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	delete(b.graph.outgoingEdges, op.nodeID)
	delete(b.graph.incomingEdges, op.nodeID)

	// Delete node with atomic decrement
	b.graph.deleteNodeShardEntry(op.nodeID)
	atomicDecrementWithUnderflowProtection(&b.graph.stats.NodeCount)

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
	edge, exists := b.graph.lookupEdgeShard(op.edgeID)
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
