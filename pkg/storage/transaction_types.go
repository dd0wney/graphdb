package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Transaction represents a database transaction
type Transaction struct {
	gs         Storage // Changed to Storage interface
	id         uint64
	StringID   string // External UUID for API use (S10)
	TenantID   string // Scoped to a tenant (S10)
	StartTime  time.Time
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

// TransactionManager handles active transactions
type TransactionManager struct {
	active map[string]*Transaction
	graph  Storage
	mu     sync.RWMutex
}

func NewTransactionManager(graph Storage) *TransactionManager {
	return &TransactionManager{
		active: make(map[string]*Transaction),
		graph:  graph,
	}
}

func (tm *TransactionManager) Begin(tenantID string) (*Transaction, error) {
	// For this spike, we'll try to cast to *GraphStorage for internal ID allocation,
	// or just generate a new UUID and use it for everything.
	tx := &Transaction{
		gs:           tm.graph,
		StringID:     uuid.New().String(),
		TenantID:     tenantID,
		StartTime:    time.Now(),
		active:       true,
		createdNodes: make(map[uint64]*Node),
		updatedNodes: make(map[uint64]map[string]Value),
		createdEdges: make(map[uint64]*Edge),
		deletedNodes: make(map[uint64]bool),
		deletedEdges: make(map[uint64]bool),
	}
	
	tm.mu.Lock()
	tm.active[tx.StringID] = tx
	tm.mu.Unlock()
	
	return tx, nil
}

func (tm *TransactionManager) Get(id string) (*Transaction, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	tx, ok := tm.active[id]
	return tx, ok
}

func (tm *TransactionManager) Commit(ctx context.Context, id string) error {
	tm.mu.Lock()
	tx, ok := tm.active[id]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("transaction not found: %s", id)
	}
	delete(tm.active, id)
	tm.mu.Unlock()

	return tx.Commit(ctx)
}

func (tm *TransactionManager) Rollback(id string) error {
	tm.mu.Lock()
	tx, ok := tm.active[id]
	if !ok {
		tm.mu.Unlock()
		return fmt.Errorf("transaction not found: %s", id)
	}
	delete(tm.active, id)
	tm.mu.Unlock()

	return tx.Rollback()
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
