package storage

import (
	"errors"
	"sync"
)

// Transaction represents a database transaction
type Transaction struct {
	gs         *GraphStorage
	id         uint64
	active     bool
	committed  bool
	rolledBack bool
	mu         sync.RWMutex

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

// allocateTransactionID allocates a new transaction ID
func (gs *GraphStorage) allocateTransactionID() uint64 {
	gs.txIDCounter++
	return gs.txIDCounter
}
