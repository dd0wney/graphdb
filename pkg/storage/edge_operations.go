package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// CreateEdge creates a new edge between two nodes
func (gs *GraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Verify nodes exist
	if err := gs.verifyNodeExists(fromID, "source"); err != nil {
		return nil, err
	}
	if err := gs.verifyNodeExists(toID, "target"); err != nil {
		return nil, err
	}

	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) { // MaxUint64
		return nil, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++

	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	gs.edges[edgeID] = edge

	// Update indexes
	gs.edgesByType[edgeType] = append(gs.edgesByType[edgeType], edgeID)

	// Store edge adjacency (disk-backed or in-memory)
	gs.storeOutgoingEdge(fromID, edgeID)
	gs.storeIncomingEdge(toID, edgeID)

	atomic.AddUint64(&gs.stats.EdgeCount, 1)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpCreateEdge, edge)

	return edge.Clone(), nil
}

// DeleteEdge deletes an edge by ID
func (gs *GraphStorage) DeleteEdge(edgeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Get edge to find fromID and toID
	edge, exists := gs.edges[edgeID]
	if !exists {
		return fmt.Errorf("edge %d not found", edgeID)
	}

	fromID := edge.FromNodeID
	toID := edge.ToNodeID

	// Delete from edges map
	delete(gs.edges, edgeID)

	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)

	// Remove from adjacency (disk-backed or in-memory)
	gs.removeOutgoingEdge(fromID, edgeID)
	gs.removeIncomingEdge(toID, edgeID)

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDeleteEdge, edge)

	return nil
}

// GetEdge retrieves an edge by ID
func (gs *GraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	defer gs.startQueryTiming()()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)

	edge, exists := gs.edges[edgeID]
	if !exists {
		return nil, ErrEdgeNotFound
	}

	return edge.Clone(), nil
}

// UpdateEdge updates an edge's properties and/or weight
func (gs *GraphStorage) UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	edge, exists := gs.edges[edgeID]
	if !exists {
		return ErrEdgeNotFound
	}

	// Update properties (merge with existing)
	if properties != nil {
		for k, v := range properties {
			edge.Properties[k] = v
		}
	}

	// Update weight if provided
	if weight != nil {
		edge.Weight = *weight
	}

	// Write to WAL for durability
	gs.writeToWAL(wal.OpUpdateEdge, edge)

	return nil
}

// Edge adjacency helper methods

// storeOutgoingEdge adds an edge to a node's outgoing adjacency list (disk or memory)
func (gs *GraphStorage) storeOutgoingEdge(nodeID, edgeID uint64) {
	if gs.useDiskBackedEdges {
		existing, _ := gs.edgeStore.GetOutgoingEdges(nodeID)
		gs.edgeStore.StoreOutgoingEdges(nodeID, append(existing, edgeID))
	} else {
		gs.outgoingEdges[nodeID] = append(gs.outgoingEdges[nodeID], edgeID)
	}
}

// storeIncomingEdge adds an edge to a node's incoming adjacency list (disk or memory)
func (gs *GraphStorage) storeIncomingEdge(nodeID, edgeID uint64) {
	if gs.useDiskBackedEdges {
		existing, _ := gs.edgeStore.GetIncomingEdges(nodeID)
		gs.edgeStore.StoreIncomingEdges(nodeID, append(existing, edgeID))
	} else {
		gs.incomingEdges[nodeID] = append(gs.incomingEdges[nodeID], edgeID)
	}
}
