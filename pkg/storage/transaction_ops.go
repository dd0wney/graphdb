package storage

import (
	"errors"
	"time"
)

func (tx *Transaction) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	// Internal ID for intra-transaction referencing
	nodeID := uint64(time.Now().UnixNano())

	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: make(map[string]Value),
		TenantID:   tx.TenantID,
	}

	for k, v := range properties {
		node.Properties[k] = v
	}

	tx.createdNodes[nodeID] = node
	return node, nil
}

func (tx *Transaction) UpdateNode(nodeID uint64, properties map[string]Value) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}

	if node, ok := tx.createdNodes[nodeID]; ok {
		for k, v := range properties {
			node.Properties[k] = v
		}
		return nil
	}

	if tx.updatedNodes[nodeID] == nil {
		tx.updatedNodes[nodeID] = make(map[string]Value)
	}
	for k, v := range properties {
		tx.updatedNodes[nodeID][k] = v
	}

	return nil
}

func (tx *Transaction) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	edgeID := uint64(time.Now().UnixNano())

	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: make(map[string]Value),
		Weight:     weight,
		TenantID:   tx.TenantID,
	}

	for k, v := range properties {
		edge.Properties[k] = v
	}

	tx.createdEdges[edgeID] = edge
	return edge, nil
}

func (tx *Transaction) GetNodeByID(nodeID uint64) (*Node, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	if tx.deletedNodes[nodeID] {
		return nil, errors.New("node not found")
	}

	if node, ok := tx.createdNodes[nodeID]; ok {
		return node, nil
	}

	return tx.gs.GetNodeForTenant(nodeID, tx.TenantID)
}

func (tx *Transaction) GetEdgeByID(edgeID uint64) (*Edge, error) {
	tx.mu.RLock()
	defer tx.mu.RUnlock()

	if !tx.active {
		return nil, ErrTransactionNotActive
	}

	if tx.deletedEdges[edgeID] {
		return nil, errors.New("edge not found")
	}

	if edge, ok := tx.createdEdges[edgeID]; ok {
		return edge, nil
	}

	return tx.gs.GetEdgeForTenant(edgeID, tx.TenantID)
}
