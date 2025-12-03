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
		ID:         op.nodeID,
		Labels:     op.labels,
		Properties: op.properties,
	}

	b.graph.nodes[node.ID] = node
	atomic.AddUint64(&b.graph.stats.NodeCount, 1)

	// Update label indexes
	for _, label := range node.Labels {
		b.graph.nodesByLabel[label] = append(b.graph.nodesByLabel[label], node.ID)
	}

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
		Properties: op.properties,
		Weight:     op.weight,
	}

	b.graph.edges[edge.ID] = edge
	atomic.AddUint64(&b.graph.stats.EdgeCount, 1)

	// Update edge type index
	b.graph.edgesByType[edge.Type] = append(b.graph.edgesByType[edge.Type], edge.ID)

	// Update adjacency lists
	b.graph.outgoingEdges[edge.FromNodeID] = append(b.graph.outgoingEdges[edge.FromNodeID], edge.ID)
	b.graph.incomingEdges[edge.ToNodeID] = append(b.graph.incomingEdges[edge.ToNodeID], edge.ID)

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
	node, exists := b.graph.nodes[op.nodeID]
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
	node, exists := b.graph.nodes[op.nodeID]
	if !exists {
		return nil // Skip non-existent nodes
	}

	// Remove from label indexes (defensive: use helper to avoid slice modification during range)
	for _, label := range node.Labels {
		b.graph.nodesByLabel[label] = removeEdgeFromList(b.graph.nodesByLabel[label], op.nodeID)
	}

	// Remove from property indexes
	for key, value := range node.Properties {
		if idx, exists := b.graph.propertyIndexes[key]; exists {
			if err := idx.Remove(op.nodeID, value); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}

	// Delete edges
	outgoing := b.graph.outgoingEdges[op.nodeID]
	for _, edgeID := range outgoing {
		delete(b.graph.edges, edgeID)
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	incoming := b.graph.incomingEdges[op.nodeID]
	for _, edgeID := range incoming {
		delete(b.graph.edges, edgeID)
		atomicDecrementWithUnderflowProtection(&b.graph.stats.EdgeCount)
	}

	delete(b.graph.outgoingEdges, op.nodeID)
	delete(b.graph.incomingEdges, op.nodeID)

	// Delete node with atomic decrement
	delete(b.graph.nodes, op.nodeID)
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
	edge, exists := b.graph.edges[op.edgeID]
	if !exists {
		return nil // Skip non-existent edges
	}

	// Remove from type index
	edgeIDs := b.graph.edgesByType[edge.Type]
	for i, id := range edgeIDs {
		if id == op.edgeID {
			b.graph.edgesByType[edge.Type] = append(edgeIDs[:i], edgeIDs[i+1:]...)
			break
		}
	}

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
	delete(b.graph.edges, op.edgeID)
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
