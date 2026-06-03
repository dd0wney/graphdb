package storage

import (
	"errors"
	"sync"
)

// Transaction represents a database transaction. It is tenant-bound: every
// node/edge it creates is stamped with tenantID, and Commit validates that any
// edge endpoint / update target belongs to that tenant (the *ForTenant
// convention). Commit is atomic (single-fsync, all-or-none) and serializes on
// gs.mu (last-writer-wins; no conflict detection). Creates + updates only —
// deletes are deferred (the deletedNodes/deletedEdges fields below are dead
// scaffolding for that follow-up; nothing populates them today).
type Transaction struct {
	gs         *GraphStorage
	id         uint64
	tenantID   string // empty == default tenant (effectiveTenantID resolves it)
	active     bool
	committed  bool
	rolledBack bool
	mu         sync.RWMutex

	// Pending operations
	createdNodes map[uint64]*Node
	updatedNodes map[uint64]map[string]Value
	createdEdges map[uint64]*Edge
	deletedNodes map[uint64]bool // reserved for deferred delete support
	deletedEdges map[uint64]bool // reserved for deferred delete support
}

var (
	ErrTransactionNotActive    = errors.New("transaction is not active")
	ErrTransactionAlreadyEnded = errors.New("transaction has already been committed or rolled back")
	ErrNestedTransaction       = errors.New("nested transactions are not supported")
)

// BeginTransaction starts a new transaction in the default tenant. Equivalent
// to BeginTransactionForTenant("") — kept for backward compatibility.
func (gs *GraphStorage) BeginTransaction() (*Transaction, error) {
	return gs.BeginTransactionForTenant("")
}

// BeginTransactionForTenant starts a new transaction bound to tenantID. All
// nodes/edges created in the transaction are stamped with this tenant, and
// Commit enforces tenant ownership of edge endpoints / update targets.
func (gs *GraphStorage) BeginTransactionForTenant(tenantID string) (*Transaction, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Allocate new transaction (allow concurrent transactions)
	tx := &Transaction{
		gs:           gs,
		id:           gs.allocateTransactionID(),
		tenantID:     tenantID,
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
