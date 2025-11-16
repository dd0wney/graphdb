package storage

import (
	"fmt"
	"sync"
)

// GraphStorageGlobalLock is a simplified version with a single global lock
// Used for benchmarking comparison against sharded locking
type GraphStorageGlobalLock struct {
	mu    sync.RWMutex // Single lock for ALL operations
	nodes map[uint64]*Node
	edges map[uint64]*Edge

	nextNodeID uint64
	nextEdgeID uint64

	// Label index
	labelIndex map[string][]uint64 // label -> node IDs

	// Edge type index
	edgeTypeIndex map[string][]uint64 // edge_type -> edge IDs
}

// NewGraphStorageGlobalLock creates a new storage with global locking
func NewGraphStorageGlobalLock() *GraphStorageGlobalLock {
	return &GraphStorageGlobalLock{
		nodes:         make(map[uint64]*Node),
		edges:         make(map[uint64]*Edge),
		nextNodeID:    1,
		nextEdgeID:    1,
		labelIndex:    make(map[string][]uint64),
		edgeTypeIndex: make(map[string][]uint64),
	}
}

// CreateNode creates a new node with global locking
func (gs *GraphStorageGlobalLock) CreateNode(label string, properties map[string]interface{}) (*Node, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	nodeID := gs.nextNodeID
	gs.nextNodeID++

	node := &Node{
		ID:         nodeID,
		Label:      label,
		Properties: properties,
	}

	gs.nodes[nodeID] = node
	gs.labelIndex[label] = append(gs.labelIndex[label], nodeID)

	return node, nil
}

// GetNode retrieves a node by ID with global locking
func (gs *GraphStorageGlobalLock) GetNode(id uint64) (*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	node, exists := gs.nodes[id]
	if !exists {
		return nil, fmt.Errorf("node %d not found", id)
	}

	return node, nil
}

// CreateEdge creates a new edge with global locking
func (gs *GraphStorageGlobalLock) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]interface{}) (*Edge, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Verify nodes exist
	if _, exists := gs.nodes[fromID]; !exists {
		return nil, fmt.Errorf("from node %d not found", fromID)
	}
	if _, exists := gs.nodes[toID]; !exists {
		return nil, fmt.Errorf("to node %d not found", toID)
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++

	edge := &Edge{
		ID:         edgeID,
		FromID:     fromID,
		ToID:       toID,
		Type:       edgeType,
		Properties: properties,
	}

	gs.edges[edgeID] = edge
	gs.edgeTypeIndex[edgeType] = append(gs.edgeTypeIndex[edgeType], edgeID)

	return edge, nil
}

// FindNodesByLabel finds nodes by label with global locking
func (gs *GraphStorageGlobalLock) FindNodesByLabel(label string) ([]*Node, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodeIDs, exists := gs.labelIndex[label]
	if !exists {
		return []*Node{}, nil
	}

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if node, exists := gs.nodes[nodeID]; exists {
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// GetStatistics returns storage statistics with global locking
func (gs *GraphStorageGlobalLock) GetStatistics() Statistics {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	return Statistics{
		NodeCount: uint64(len(gs.nodes)),
		EdgeCount: uint64(len(gs.edges)),
	}
}
