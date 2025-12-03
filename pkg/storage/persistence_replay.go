package storage

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// replayWAL replays WAL entries to recover state
func (gs *GraphStorage) replayWAL() error {
	if gs.useBatching && gs.batchedWAL != nil {
		return gs.batchedWAL.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	} else if gs.wal != nil {
		return gs.wal.Replay(func(entry *wal.Entry) error {
			return gs.replayEntry(entry)
		})
	}
	return nil
}

// replayEntry replays a single WAL entry
func (gs *GraphStorage) replayEntry(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		return gs.replayCreateNode(entry)
	case wal.OpUpdateNode:
		return gs.replayUpdateNode(entry)
	case wal.OpCreateEdge:
		return gs.replayCreateEdge(entry)
	case wal.OpDeleteEdge:
		return gs.replayDeleteEdge(entry)
	case wal.OpDeleteNode:
		return gs.replayDeleteNode(entry)
	case wal.OpCreatePropertyIndex:
		return gs.replayCreatePropertyIndex(entry)
	case wal.OpDropPropertyIndex:
		return gs.replayDropPropertyIndex(entry)
	}
	return nil
}

func (gs *GraphStorage) replayCreateNode(entry *wal.Entry) error {
	var node Node
	if err := json.Unmarshal(entry.Data, &node); err != nil {
		return err
	}

	// Skip if node already exists (already in snapshot)
	if _, exists := gs.nodes[node.ID]; exists {
		return nil
	}

	// Replay node creation
	gs.nodes[node.ID] = &node
	for _, label := range node.Labels {
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], node.ID)
	}
	if gs.outgoingEdges[node.ID] == nil {
		gs.outgoingEdges[node.ID] = make([]uint64, 0)
	}
	if gs.incomingEdges[node.ID] == nil {
		gs.incomingEdges[node.ID] = make([]uint64, 0)
	}

	// Insert into property indexes if they exist
	if err := gs.insertNodeIntoPropertyIndexes(node.ID, node.Properties); err != nil {
		return fmt.Errorf("failed to insert node %d into property indexes: %w", node.ID, err)
	}

	// Update stats atomically
	atomic.AddUint64(&gs.stats.NodeCount, 1)

	// Update next ID if necessary
	if node.ID >= gs.nextNodeID {
		gs.nextNodeID = node.ID + 1
	}

	return nil
}

func (gs *GraphStorage) replayUpdateNode(entry *wal.Entry) error {
	var updateInfo struct {
		NodeID     uint64
		Properties map[string]Value
	}
	if err := json.Unmarshal(entry.Data, &updateInfo); err != nil {
		return err
	}

	// Skip if node doesn't exist
	node, exists := gs.nodes[updateInfo.NodeID]
	if !exists {
		return nil
	}

	// Update property indexes
	if err := gs.updatePropertyIndexes(updateInfo.NodeID, node, updateInfo.Properties); err != nil {
		return fmt.Errorf("failed to update property indexes for node %d: %w", updateInfo.NodeID, err)
	}

	// Apply property updates
	for key, value := range updateInfo.Properties {
		node.Properties[key] = value
	}

	return nil
}

func (gs *GraphStorage) replayCreateEdge(entry *wal.Entry) error {
	var edge Edge
	if err := json.Unmarshal(entry.Data, &edge); err != nil {
		return err
	}

	// Skip if edge already exists (already in snapshot)
	if _, exists := gs.edges[edge.ID]; exists {
		return nil
	}

	// Replay edge creation
	gs.edges[edge.ID] = &edge
	gs.edgesByType[edge.Type] = append(gs.edgesByType[edge.Type], edge.ID)

	// Rebuild adjacency lists (disk-backed or in-memory)
	if err := gs.storeOutgoingEdge(edge.FromNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store outgoing edge during replay: %w", err)
	}
	if err := gs.storeIncomingEdge(edge.ToNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store incoming edge during replay: %w", err)
	}

	// Update stats atomically
	atomic.AddUint64(&gs.stats.EdgeCount, 1)

	// Update next ID if necessary
	if edge.ID >= gs.nextEdgeID {
		gs.nextEdgeID = edge.ID + 1
	}

	return nil
}

func (gs *GraphStorage) replayDeleteEdge(entry *wal.Entry) error {
	var edge Edge
	if err := json.Unmarshal(entry.Data, &edge); err != nil {
		return err
	}

	// Skip if edge doesn't exist (already deleted or never existed)
	if _, exists := gs.edges[edge.ID]; !exists {
		return nil
	}

	// Replay edge deletion
	delete(gs.edges, edge.ID)

	// Remove from type index
	gs.removeEdgeFromTypeIndex(edge.Type, edge.ID)

	// Remove from adjacency lists (disk-backed or in-memory)
	if err := gs.removeOutgoingEdge(edge.FromNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge during replay: %w", err)
	}
	if err := gs.removeIncomingEdge(edge.ToNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to remove incoming edge during replay: %w", err)
	}

	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

	return nil
}

func (gs *GraphStorage) replayDeleteNode(entry *wal.Entry) error {
	var node Node
	if err := json.Unmarshal(entry.Data, &node); err != nil {
		return err
	}

	// Skip if node doesn't exist (already deleted or never existed)
	if _, exists := gs.nodes[node.ID]; !exists {
		return nil
	}

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		var err error
		outgoingEdgeIDs, err = gs.edgeStore.GetOutgoingEdges(node.ID)
		if err != nil {
			return fmt.Errorf("failed to get outgoing edges for node %d during replay: %w", node.ID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(node.ID)
		if err != nil {
			return fmt.Errorf("failed to get incoming edges for node %d during replay: %w", node.ID, err)
		}
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[node.ID]
		incomingEdgeIDs = gs.incomingEdges[node.ID]
	}

	// Cascade delete all outgoing edges during replay
	for _, edgeID := range outgoingEdgeIDs {
		if err := gs.cascadeDeleteOutgoingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete outgoing edge %d during replay: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges during replay
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete incoming edge %d during replay: %w", edgeID, err)
		}
	}

	// Remove from label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, node.ID)
	}

	// Remove from property indexes
	if err := gs.removeNodeFromPropertyIndexes(node.ID, node.Properties); err != nil {
		return fmt.Errorf("failed to remove node %d from property indexes: %w", node.ID, err)
	}

	// Delete node
	delete(gs.nodes, node.ID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(node.ID); err != nil {
		return fmt.Errorf("failed to clear adjacency for node %d during replay: %w", node.ID, err)
	}

	// Decrement stats with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	return nil
}

func (gs *GraphStorage) replayCreatePropertyIndex(entry *wal.Entry) error {
	var indexInfo struct {
		PropertyKey string
		ValueType   ValueType
	}
	if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
		return err
	}

	// Skip if index already exists
	if _, exists := gs.propertyIndexes[indexInfo.PropertyKey]; exists {
		return nil
	}

	// Create index and populate with existing nodes
	idx := NewPropertyIndex(indexInfo.PropertyKey, indexInfo.ValueType)
	for nodeID, node := range gs.nodes {
		if prop, exists := node.Properties[indexInfo.PropertyKey]; exists {
			if prop.Type == indexInfo.ValueType {
				if err := idx.Insert(nodeID, prop); err != nil {
					return fmt.Errorf("failed to insert node %d into property index %s during replay: %w", nodeID, indexInfo.PropertyKey, err)
				}
			}
		}
	}
	gs.propertyIndexes[indexInfo.PropertyKey] = idx

	return nil
}

func (gs *GraphStorage) replayDropPropertyIndex(entry *wal.Entry) error {
	var indexInfo struct {
		PropertyKey string
	}
	if err := json.Unmarshal(entry.Data, &indexInfo); err != nil {
		return err
	}

	// Remove index
	delete(gs.propertyIndexes, indexInfo.PropertyKey)

	return nil
}
