package storage

import "fmt"

// Adjacency and cascade delete helper methods

// clearNodeAdjacency clears a node's adjacency lists (disk or memory)
func (gs *GraphStorage) clearNodeAdjacency(nodeID uint64) error {
	if gs.useDiskBackedEdges {
		if err := gs.edgeStore.StoreOutgoingEdges(nodeID, []uint64{}); err != nil {
			return fmt.Errorf("failed to clear outgoing edges for node %d: %w", nodeID, err)
		}
		if err := gs.edgeStore.StoreIncomingEdges(nodeID, []uint64{}); err != nil {
			return fmt.Errorf("failed to clear incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		delete(gs.outgoingEdges, nodeID)
		delete(gs.incomingEdges, nodeID)
	}
	return nil
}

// removeOutgoingEdge removes an edge from a node's outgoing adjacency list (disk or memory)
func (gs *GraphStorage) removeOutgoingEdge(nodeID, edgeID uint64) error {
	if gs.useDiskBackedEdges {
		outgoing, err := gs.edgeStore.GetOutgoingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		if err := gs.edgeStore.StoreOutgoingEdges(nodeID, removeEdgeFromList(outgoing, edgeID)); err != nil {
			return fmt.Errorf("failed to store outgoing edges for node %d: %w", nodeID, err)
		}
	} else {
		gs.outgoingEdges[nodeID] = removeEdgeFromList(gs.outgoingEdges[nodeID], edgeID)
	}
	return nil
}

// removeIncomingEdge removes an edge from a node's incoming adjacency list (disk or memory)
func (gs *GraphStorage) removeIncomingEdge(nodeID, edgeID uint64) error {
	if gs.useDiskBackedEdges {
		incoming, err := gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
		}
		if err := gs.edgeStore.StoreIncomingEdges(nodeID, removeEdgeFromList(incoming, edgeID)); err != nil {
			return fmt.Errorf("failed to store incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		gs.incomingEdges[nodeID] = removeEdgeFromList(gs.incomingEdges[nodeID], edgeID)
	}
	return nil
}

// removeEdgeFromTypeIndex removes an edge from the type index
func (gs *GraphStorage) removeEdgeFromTypeIndex(edgeType string, edgeID uint64) {
	if edgeList, exists := gs.edgesByType[edgeType]; exists {
		gs.edgesByType[edgeType] = removeEdgeFromList(edgeList, edgeID)
	}
}

// cascadeDeleteOutgoingEdge deletes an outgoing edge and removes it from the target node's incoming list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteOutgoingEdge(edgeID uint64) error {
	gs.lockShard(edgeID)
	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		gs.unlockShard(edgeID)
		return nil
	}
	gs.deleteEdgeShardEntry(edgeID)
	gs.unlockShard(edgeID)

	// Remove from target node's incoming edges
	if err := gs.removeIncomingEdge(edge.ToNodeID, edgeID); err != nil {
		return fmt.Errorf("failed to remove incoming edge %d: %w", edgeID, err)
	}
	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)
	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	return nil
}

// cascadeDeleteIncomingEdge deletes an incoming edge and removes it from the source node's outgoing list.
// Caller (DeleteNode) holds gs.mu.Lock; we add lockShard for the edgeShards mutation per A4-edges.
func (gs *GraphStorage) cascadeDeleteIncomingEdge(edgeID uint64) error {
	gs.lockShard(edgeID)
	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		gs.unlockShard(edgeID)
		return nil
	}
	gs.deleteEdgeShardEntry(edgeID)
	gs.unlockShard(edgeID)

	// Remove from source node's outgoing edges
	if err := gs.removeOutgoingEdge(edge.FromNodeID, edgeID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge %d: %w", edgeID, err)
	}
	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)
	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	return nil
}
