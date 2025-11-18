package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// CreateNode creates a new node
func (gs *GraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		return nil, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++

	now := time.Now().Unix()
	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: properties,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	gs.nodes[nodeID] = node

	// Update label indexes
	for _, label := range labels {
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], nodeID)
	}

	// Initialize edge lists
	gs.outgoingEdges[nodeID] = make([]uint64, 0)
	gs.incomingEdges[nodeID] = make([]uint64, 0)

	atomic.AddUint64(&gs.stats.NodeCount, 1)

	// Update property indexes
	gs.insertNodeIntoPropertyIndexes(nodeID, properties)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpCreateNode, node)

	return node.Clone(), nil
}

// GetNode retrieves a node by ID
func (gs *GraphStorage) GetNode(nodeID uint64) (*Node, error) {
	defer gs.startQueryTiming()()

	// Use global read lock to properly synchronize with CreateNode's write lock
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return nil, ErrNodeNotFound
	}

	return node.Clone(), nil
}

// UpdateNode updates a node's properties
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Update property indexes
	gs.updatePropertyIndexes(nodeID, node, properties)

	// Update properties
	for k, v := range properties {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()

	// Write to WAL for durability
	gs.writeToWAL(wal.OpUpdateNode, struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: properties,
	})

	return nil
}

// DeleteNode deletes a node and all its edges
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		outgoingEdgeIDs, _ = gs.edgeStore.GetOutgoingEdges(nodeID)
		incomingEdgeIDs, _ = gs.edgeStore.GetIncomingEdges(nodeID)
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[nodeID]
		incomingEdgeIDs = gs.incomingEdges[nodeID]
	}

	// Cascade delete all outgoing edges
	for _, edgeID := range outgoingEdgeIDs {
		gs.cascadeDeleteOutgoingEdge(edgeID)
	}

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		gs.cascadeDeleteIncomingEdge(edgeID)
	}

	// Remove from label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, nodeID)
	}

	// Remove from property indexes
	gs.removeNodeFromPropertyIndexes(nodeID, node.Properties)

	// Delete node
	delete(gs.nodes, nodeID)

	// Delete adjacency lists (disk-backed or in-memory)
	gs.clearNodeAdjacency(nodeID)

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDeleteNode, node)

	return nil
}

// Property index helper methods

// updatePropertyIndexes updates property indexes when a node's properties change
func (gs *GraphStorage) updatePropertyIndexes(nodeID uint64, node *Node, properties map[string]Value) {
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[k]; exists {
				idx.Remove(nodeID, oldValue)
			}
			// Add new value to index
			idx.Insert(nodeID, newValue)
		}
	}
}

// insertNodeIntoPropertyIndexes inserts a node into all matching property indexes
func (gs *GraphStorage) insertNodeIntoPropertyIndexes(nodeID uint64, properties map[string]Value) {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			idx.Insert(nodeID, value)
		}
	}
}

// removeNodeFromPropertyIndexes removes a node from all property indexes
func (gs *GraphStorage) removeNodeFromPropertyIndexes(nodeID uint64, properties map[string]Value) {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			idx.Remove(nodeID, value)
		}
	}
}

// Adjacency and cascade delete helper methods

// clearNodeAdjacency clears a node's adjacency lists (disk or memory)
func (gs *GraphStorage) clearNodeAdjacency(nodeID uint64) {
	if gs.useDiskBackedEdges {
		gs.edgeStore.StoreOutgoingEdges(nodeID, []uint64{})
		gs.edgeStore.StoreIncomingEdges(nodeID, []uint64{})
	} else {
		delete(gs.outgoingEdges, nodeID)
		delete(gs.incomingEdges, nodeID)
	}
}

// removeOutgoingEdge removes an edge from a node's outgoing adjacency list (disk or memory)
func (gs *GraphStorage) removeOutgoingEdge(nodeID, edgeID uint64) {
	if gs.useDiskBackedEdges {
		outgoing, _ := gs.edgeStore.GetOutgoingEdges(nodeID)
		gs.edgeStore.StoreOutgoingEdges(nodeID, removeEdgeFromList(outgoing, edgeID))
	} else {
		gs.outgoingEdges[nodeID] = removeEdgeFromList(gs.outgoingEdges[nodeID], edgeID)
	}
}

// removeIncomingEdge removes an edge from a node's incoming adjacency list (disk or memory)
func (gs *GraphStorage) removeIncomingEdge(nodeID, edgeID uint64) {
	if gs.useDiskBackedEdges {
		incoming, _ := gs.edgeStore.GetIncomingEdges(nodeID)
		gs.edgeStore.StoreIncomingEdges(nodeID, removeEdgeFromList(incoming, edgeID))
	} else {
		gs.incomingEdges[nodeID] = removeEdgeFromList(gs.incomingEdges[nodeID], edgeID)
	}
}

// removeEdgeFromTypeIndex removes an edge from the type index
func (gs *GraphStorage) removeEdgeFromTypeIndex(edgeType string, edgeID uint64) {
	if edgeList, exists := gs.edgesByType[edgeType]; exists {
		gs.edgesByType[edgeType] = removeEdgeFromList(edgeList, edgeID)
	}
}

// cascadeDeleteOutgoingEdge deletes an outgoing edge and removes it from the target node's incoming list
func (gs *GraphStorage) cascadeDeleteOutgoingEdge(edgeID uint64) {
	if edge, exists := gs.edges[edgeID]; exists {
		// Remove from target node's incoming edges
		gs.removeIncomingEdge(edge.ToNodeID, edgeID)

		// Remove from type index
		gs.removeEdgeFromTypeIndex(edge.Type, edgeID)

		// Delete edge object
		delete(gs.edges, edgeID)

		// Decrement stats with underflow protection
		atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	}
}

// cascadeDeleteIncomingEdge deletes an incoming edge and removes it from the source node's outgoing list
func (gs *GraphStorage) cascadeDeleteIncomingEdge(edgeID uint64) {
	if edge, exists := gs.edges[edgeID]; exists {
		// Remove from source node's outgoing edges
		gs.removeOutgoingEdge(edge.FromNodeID, edgeID)

		// Remove from type index
		gs.removeEdgeFromTypeIndex(edge.Type, edgeID)

		// Delete edge object
		delete(gs.edges, edgeID)

		// Decrement stats with underflow protection
		atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	}
}

// removeFromLabelIndex removes a node from a label index
func (gs *GraphStorage) removeFromLabelIndex(label string, nodeID uint64) {
	nodeIDs := gs.nodesByLabel[label]
	for i, id := range nodeIDs {
		if id == nodeID {
			// Remove by swapping with last element and truncating
			nodeIDs[i] = nodeIDs[len(nodeIDs)-1]
			gs.nodesByLabel[label] = nodeIDs[:len(nodeIDs)-1]
			break
		}
	}
}
