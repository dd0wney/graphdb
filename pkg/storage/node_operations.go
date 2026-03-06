package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// CreateNode creates a new node in the default tenant.
// For multi-tenant operations, use CreateNodeWithTenant instead.
func (gs *GraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	return gs.CreateNodeWithTenant(DefaultTenantID, labels, properties)
}

// CreateNodeWithTenant creates a new node for a specific tenant.
func (gs *GraphStorage) CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error) {
	start := time.Now()
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check if storage is closed
	if err := gs.checkClosed(); err != nil {
		gs.recordOperation("create_node", "error", start)
		return nil, err
	}

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		gs.recordOperation("create_node", "error", start)
		return nil, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++

	now := time.Now().Unix()
	node := &Node{
		ID:         nodeID,
		TenantID:   effectiveTenantID(tenantID),
		Labels:     labels,
		Properties: properties,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	gs.nodes[nodeID] = node

	// Update global label indexes (for backward compatibility)
	for _, label := range labels {
		gs.nodesByLabel[label] = append(gs.nodesByLabel[label], nodeID)
	}

	// Update tenant-scoped indexes
	gs.addNodeToTenantIndex(node)

	// Initialize edge lists
	gs.outgoingEdges[nodeID] = make([]uint64, 0)
	gs.incomingEdges[nodeID] = make([]uint64, 0)

	atomic.AddUint64(&gs.stats.NodeCount, 1)

	// Update property indexes
	if err := gs.insertNodeIntoPropertyIndexes(nodeID, properties); err != nil {
		gs.recordOperation("create_node", "error", start)
		return nil, err
	}

	// Update vector indexes for any vector properties
	if err := gs.UpdateNodeVectorIndexes(node); err != nil {
		gs.recordOperation("create_node", "error", start)
		return nil, err
	}

	// Write to WAL for durability
	gs.writeToWAL(wal.OpCreateNode, node)

	gs.recordOperation("create_node", "success", start)
	return node.Clone(), nil
}

// GetNode retrieves a node by ID
func (gs *GraphStorage) GetNode(nodeID uint64) (*Node, error) {
	start := time.Now()
	defer gs.startQueryTiming()()

	// Use global read lock to properly synchronize with CreateNode's write lock
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Check if storage is closed
	if err := gs.checkClosed(); err != nil {
		gs.recordOperation("get_node", "error", start)
		return nil, err
	}

	node, exists := gs.nodes[nodeID]
	if !exists {
		gs.recordOperation("get_node", "error", start)
		return nil, ErrNodeNotFound
	}

	gs.recordOperation("get_node", "success", start)
	return node.Clone(), nil
}

// UpdateNode updates a node's properties
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Update property indexes
	if err := gs.updatePropertyIndexes(nodeID, node, properties); err != nil {
		return err
	}

	// Update properties
	for k, v := range properties {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()

	// Update vector indexes for any vector properties
	if err := gs.UpdateNodeVectorIndexes(node); err != nil {
		return err
	}

	// Write to WAL for durability
	gs.writeToWAL(wal.OpUpdateNode, struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: properties,
	})

	return nil
}

// RemoveNodeProperties removes specified properties from a node.
// Unlike UpdateNode (which merges), this deletes keys from the properties map.
func (gs *GraphStorage) RemoveNodeProperties(nodeID uint64, keys []string) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	for _, key := range keys {
		// Remove from property indexes
		if idx, exists := gs.propertyIndexes[key]; exists {
			if oldValue, hasKey := node.Properties[key]; hasKey {
				idx.Remove(nodeID, oldValue)
			}
		}
		delete(node.Properties, key)
	}
	node.UpdatedAt = time.Now().Unix()

	// Snapshot properties for WAL — avoid passing the live map reference
	// which could race with concurrent writers after the lock is released.
	walProps := make(map[string]Value, len(node.Properties))
	for k, v := range node.Properties {
		walProps[k] = v
	}
	gs.writeToWAL(wal.OpUpdateNode, struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: walProps,
	})

	return nil
}

// DeleteNode deletes a node and all its edges
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	node, exists := gs.nodes[nodeID]
	if !exists {
		return ErrNodeNotFound
	}

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		var err error
		outgoingEdgeIDs, err = gs.edgeStore.GetOutgoingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[nodeID]
		incomingEdgeIDs = gs.incomingEdges[nodeID]
	}

	// Cascade delete all outgoing edges
	for _, edgeID := range outgoingEdgeIDs {
		if err := gs.cascadeDeleteOutgoingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete outgoing edge %d: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			return fmt.Errorf("failed to cascade delete incoming edge %d: %w", edgeID, err)
		}
	}

	// Remove from global label indexes
	for _, label := range node.Labels {
		gs.removeFromLabelIndex(label, nodeID)
	}

	// Remove from tenant-scoped indexes
	gs.removeNodeFromTenantIndex(node)

	// Remove from property indexes
	if err := gs.removeNodeFromPropertyIndexes(nodeID, node.Properties); err != nil {
		return err
	}

	// Remove from vector indexes
	if err := gs.RemoveNodeFromVectorIndexes(nodeID); err != nil {
		return err
	}

	// Delete node
	delete(gs.nodes, nodeID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(nodeID); err != nil {
		return fmt.Errorf("failed to clear adjacency for node %d: %w", nodeID, err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDeleteNode, node)

	return nil
}

// GetAllNodeIDs returns all node IDs in the storage.
// This is the preferred way to iterate over all nodes, as it handles
// deleted nodes correctly (unlike iterating from 1 to NodeCount).
func (gs *GraphStorage) GetAllNodeIDs() []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodeIDs := make([]uint64, 0, len(gs.nodes))
	for id := range gs.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	return nodeIDs
}

// GetAllNodes returns all nodes in the storage.
// Returns cloned nodes to prevent modification of internal data.
func (gs *GraphStorage) GetAllNodes() []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodes := make([]*Node, 0, len(gs.nodes))
	for _, node := range gs.nodes {
		nodes = append(nodes, node.Clone())
	}
	return nodes
}

// ForEachNode iterates over all nodes, calling the provided function for each.
// The function receives a cloned node to prevent modification.
// Iteration stops early if the function returns false.
func (gs *GraphStorage) ForEachNode(fn func(*Node) bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	for _, node := range gs.nodes {
		if !fn(node.Clone()) {
			return
		}
	}
}
