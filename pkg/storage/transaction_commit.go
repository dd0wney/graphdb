package storage

import (
	"errors"
)

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
// Groups nodes by shard to reduce lock contention
func (tx *Transaction) applyCreatedNodes() {
	if len(tx.createdNodes) == 0 {
		return
	}

	// Group nodes by shard to batch lock acquisitions
	nodesByShard := make(map[int][]*Node)
	for _, node := range tx.createdNodes {
		shardIdx := tx.gs.getShardIndex(node.ID)
		nodesByShard[shardIdx] = append(nodesByShard[shardIdx], node)
	}

	// Process each shard with a single lock acquisition
	for shardIdx, nodes := range nodesByShard {
		tx.gs.shardLocks[shardIdx].Lock()
		for _, node := range nodes {
			tx.gs.nodes[node.ID] = node
			// Update indexes
			for _, label := range node.Labels {
				tx.gs.nodesByLabel[label] = append(tx.gs.nodesByLabel[label], node.ID)
			}
		}
		tx.gs.shardLocks[shardIdx].Unlock()
	}
}

// applyCreatedEdges adds buffered edge creations to storage
// Groups edges by shard to reduce lock contention
func (tx *Transaction) applyCreatedEdges() {
	if len(tx.createdEdges) == 0 {
		return
	}

	// Group edges by shard to batch lock acquisitions
	edgesByShard := make(map[int][]*Edge)
	for _, edge := range tx.createdEdges {
		shardIdx := tx.gs.getShardIndex(edge.ID)
		edgesByShard[shardIdx] = append(edgesByShard[shardIdx], edge)
	}

	// Process each shard with a single lock acquisition
	for shardIdx, edges := range edgesByShard {
		tx.gs.shardLocks[shardIdx].Lock()
		for _, edge := range edges {
			tx.gs.edges[edge.ID] = edge
			// Update edge indexes
			tx.gs.edgesByType[edge.Type] = append(tx.gs.edgesByType[edge.Type], edge.ID)
			tx.gs.outgoingEdges[edge.FromNodeID] = append(tx.gs.outgoingEdges[edge.FromNodeID], edge.ID)
			tx.gs.incomingEdges[edge.ToNodeID] = append(tx.gs.incomingEdges[edge.ToNodeID], edge.ID)
		}
		tx.gs.shardLocks[shardIdx].Unlock()
	}
}

// applyNodeUpdates applies buffered property updates to existing nodes
// Groups updates by shard to reduce lock contention
func (tx *Transaction) applyNodeUpdates() {
	if len(tx.updatedNodes) == 0 {
		return
	}

	// Group updates by shard to batch lock acquisitions
	type nodeUpdate struct {
		nodeID     uint64
		properties map[string]Value
	}
	updatesByShard := make(map[int][]nodeUpdate)
	for nodeID, properties := range tx.updatedNodes {
		shardIdx := tx.gs.getShardIndex(nodeID)
		updatesByShard[shardIdx] = append(updatesByShard[shardIdx], nodeUpdate{nodeID, properties})
	}

	// Process each shard with a single lock acquisition
	for shardIdx, updates := range updatesByShard {
		tx.gs.shardLocks[shardIdx].Lock()
		for _, update := range updates {
			if node, exists := tx.gs.nodes[update.nodeID]; exists {
				for k, v := range update.properties {
					node.Properties[k] = v
				}
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
