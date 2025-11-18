package storage

import (
	"errors"
	"sync"
)

// Transaction represents a database transaction
type Transaction struct {
	gs        *GraphStorage
	id        uint64
	active    bool
	committed bool
	rolledBack bool
	mu        sync.RWMutex

	// Pending operations
	createdNodes map[uint64]*Node
	updatedNodes map[uint64]map[string]Value
	createdEdges map[uint64]*Edge
	deletedNodes map[uint64]bool
	deletedEdges map[uint64]bool
}

var (
	ErrTransactionNotActive    = errors.New("transaction is not active")
	ErrTransactionAlreadyEnded = errors.New("transaction has already been committed or rolled back")
	ErrNestedTransaction       = errors.New("nested transactions are not supported")
)

// BeginTransaction starts a new transaction
func (gs *GraphStorage) BeginTransaction() (*Transaction, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Allocate new transaction (allow concurrent transactions)
	tx := &Transaction{
		gs:           gs,
		id:           gs.allocateTransactionID(),
		active:       true,
		createdNodes: make(map[uint64]*Node),
		updatedNodes: make(map[uint64]map[string]Value),
		createdEdges: make(map[uint64]*Edge),
		deletedNodes: make(map[uint64]bool),
		deletedEdges: make(map[uint64]bool),
	}

	return tx, nil
}

// GetNodeByID gets a node by ID (alias for GetNode for compatibility)
func (gs *GraphStorage) GetNodeByID(nodeID uint64) (*Node, error) {
	return gs.GetNode(nodeID)
}

// GetEdgeByID gets an edge by ID (alias for GetEdge for compatibility)
func (gs *GraphStorage) GetEdgeByID(edgeID uint64) (*Edge, error) {
	return gs.GetEdge(edgeID)
}

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

// Commit commits the transaction
func (tx *Transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}

	if tx.committed || tx.rolledBack {
		return ErrTransactionAlreadyEnded
	}

	// Apply buffered changes to storage atomically
	tx.gs.mu.Lock()
	defer tx.gs.mu.Unlock()

	// Apply all buffered changes
	tx.applyCreatedNodes()
	tx.applyCreatedEdges()
	tx.applyNodeUpdates()

	tx.committed = true
	tx.active = false

	return nil
}

// applyCreatedNodes adds buffered node creations to storage
func (tx *Transaction) applyCreatedNodes() {
	for _, node := range tx.createdNodes {
		// Manually add to storage (bypass CreateNode to avoid re-allocation)
		shardIdx := tx.gs.getShardIndex(node.ID)
		tx.gs.shardLocks[shardIdx].Lock()
		tx.gs.nodes[node.ID] = node

		// Update indexes
		for _, label := range node.Labels {
			tx.gs.nodesByLabel[label] = append(tx.gs.nodesByLabel[label], node.ID)
		}

		tx.gs.shardLocks[shardIdx].Unlock()
	}
}

// applyCreatedEdges adds buffered edge creations to storage
func (tx *Transaction) applyCreatedEdges() {
	for _, edge := range tx.createdEdges {
		shardIdx := tx.gs.getShardIndex(edge.ID)
		tx.gs.shardLocks[shardIdx].Lock()
		tx.gs.edges[edge.ID] = edge

		// Update edge indexes
		tx.gs.edgesByType[edge.Type] = append(tx.gs.edgesByType[edge.Type], edge.ID)
		tx.gs.outgoingEdges[edge.FromNodeID] = append(tx.gs.outgoingEdges[edge.FromNodeID], edge.ID)
		tx.gs.incomingEdges[edge.ToNodeID] = append(tx.gs.incomingEdges[edge.ToNodeID], edge.ID)

		tx.gs.shardLocks[shardIdx].Unlock()
	}
}

// applyNodeUpdates applies buffered property updates to existing nodes
func (tx *Transaction) applyNodeUpdates() {
	for nodeID, properties := range tx.updatedNodes {
		shardIdx := tx.gs.getShardIndex(nodeID)
		tx.gs.shardLocks[shardIdx].Lock()

		if node, exists := tx.gs.nodes[nodeID]; exists {
			for k, v := range properties {
				node.Properties[k] = v
			}
		}

		tx.gs.shardLocks[shardIdx].Unlock()
	}
}

// Rollback rolls back the transaction
func (tx *Transaction) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil // Rollback is idempotent
	}

	if tx.committed {
		return errors.New("cannot rollback a committed transaction")
	}

	// Since changes are buffered, rollback just means discarding the buffers
	// No need to undo anything as nothing was written to storage

	tx.rolledBack = true
	tx.active = false

	// Clear buffers
	tx.createdNodes = make(map[uint64]*Node)
	tx.createdEdges = make(map[uint64]*Edge)
	tx.updatedNodes = make(map[uint64]map[string]Value)

	return nil
}

// allocateTransactionID allocates a new transaction ID
func (gs *GraphStorage) allocateTransactionID() uint64 {
	gs.txIDCounter++
	return gs.txIDCounter
}
