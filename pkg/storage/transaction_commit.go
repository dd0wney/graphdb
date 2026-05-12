package storage

import (
	"context"
	"errors"
)

// Commit commits the transaction
func (tx *Transaction) Commit(ctx context.Context) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}

	if tx.committed || tx.rolledBack {
		return ErrTransactionAlreadyEnded
	}

	// Apply all buffered changes via Storage interface
	// In a real system, we'd use a batch writer.
	for _, node := range tx.createdNodes {
		_, err := tx.gs.CreateNodeWithTenant(tx.TenantID, node.Labels, node.Properties)
		if err != nil {
			return err
		}
	}

	for nodeID, props := range tx.updatedNodes {
		err := tx.gs.UpdateNodeForTenant(nodeID, props, tx.TenantID)
		if err != nil {
			return err
		}
	}

	for _, edge := range tx.createdEdges {
		_, err := tx.gs.CreateEdgeWithTenant(tx.TenantID, edge.FromNodeID, edge.ToNodeID, edge.Type, edge.Properties, edge.Weight)
		if err != nil {
			return err
		}
	}

	for nodeID := range tx.deletedNodes {
		err := tx.gs.DeleteNodeForTenant(nodeID, tx.TenantID)
		if err != nil {
			return err
		}
	}

	tx.committed = true
	tx.active = false

	return nil
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
