package storage

import (
	"errors"
)

// CreateNode creates a node within the transaction
func (tx *Transaction) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	// Allocate ID but don't add to storage yet
	nodeID, err := tx.gs.allocateNodeID()
	if err != nil {
		return nil, err
	}

	// Create node object
	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: make(map[string]Value),
	}

	// Copy properties
	for k, v := range properties {
		node.Properties[k] = v
	}

	// Buffer the node (don't add to storage until commit)
	tx.createdNodes[nodeID] = node

	return node, nil
}

// UpdateNode updates a node within the transaction
func (tx *Transaction) UpdateNode(nodeID uint64, properties map[string]Value) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}

	// Check if node was created in this transaction
	if node, ok := tx.createdNodes[nodeID]; ok {
		// Update the buffered node
		for k, v := range properties {
			node.Properties[k] = v
		}
		return nil
	}

	// For existing nodes, buffer the update (don't apply until commit)
	if tx.updatedNodes[nodeID] == nil {
		tx.updatedNodes[nodeID] = make(map[string]Value)
	}
	for k, v := range properties {
		tx.updatedNodes[nodeID][k] = v
	}

	return nil
}

// CreateEdge creates an edge within the transaction
func (tx *Transaction) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	// Allocate ID but don't add to storage yet
	edgeID, err := tx.gs.allocateEdgeID()
	if err != nil {
		return nil, err
	}

	// Create edge object
	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: make(map[string]Value),
		Weight:     weight,
	}

	// Copy properties
	for k, v := range properties {
		edge.Properties[k] = v
	}

	// Buffer the edge (don't add to storage until commit)
	tx.createdEdges[edgeID] = edge

	return edge, nil
}

// GetNodeByID gets a node by ID within the transaction context
func (tx *Transaction) GetNodeByID(nodeID uint64) (*Node, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	// Check if node was deleted in this transaction
	if tx.deletedNodes[nodeID] {
		return nil, errors.New("node not found")
	}

	// Check if node was created in this transaction
	if node, ok := tx.createdNodes[nodeID]; ok {
		return node, nil
	}

	// Otherwise get from storage
	return tx.gs.GetNodeByID(nodeID)
}

// GetEdgeByID gets an edge by ID within the transaction context
func (tx *Transaction) GetEdgeByID(edgeID uint64) (*Edge, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	// Check if edge was deleted in this transaction
	if tx.deletedEdges[edgeID] {
		return nil, errors.New("edge not found")
	}

	// Check if edge was created in this transaction
	if edge, ok := tx.createdEdges[edgeID]; ok {
		return edge, nil
	}

	// Otherwise get from storage
	return tx.gs.GetEdgeByID(edgeID)
}
