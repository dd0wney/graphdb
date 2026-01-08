package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// CreateEdge creates a new edge between two nodes in the default tenant.
// For multi-tenant operations, use CreateEdgeWithTenant instead.
func (gs *GraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	return gs.CreateEdgeWithTenant(DefaultTenantID, fromID, toID, edgeType, properties, weight)
}

// CreateEdgeWithTenant creates a new edge between two nodes for a specific tenant.
func (gs *GraphStorage) CreateEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Verify nodes exist
	if err := gs.verifyNodeExists(fromID, "source"); err != nil {
		return nil, err
	}
	if err := gs.verifyNodeExists(toID, "target"); err != nil {
		return nil, err
	}

	edge, err := gs.createEdgeLocked(tenantID, fromID, toID, edgeType, properties, weight)
	if err != nil {
		return nil, err
	}

	return edge.Clone(), nil
}

// DeleteEdge deletes an edge by ID
func (gs *GraphStorage) DeleteEdge(edgeID uint64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Get edge to find fromID and toID
	edge, exists := gs.edges[edgeID]
	if !exists {
		return fmt.Errorf("edge %d not found", edgeID)
	}

	fromID := edge.FromNodeID
	toID := edge.ToNodeID

	// Delete from edges map
	delete(gs.edges, edgeID)

	// Remove from global type index
	gs.removeEdgeFromTypeIndex(edge.Type, edgeID)

	// Remove from tenant-scoped indexes
	gs.removeEdgeFromTenantIndex(edge)

	// Remove from adjacency (disk-backed or in-memory)
	if err := gs.removeOutgoingEdge(fromID, edgeID); err != nil {
		return fmt.Errorf("failed to remove outgoing edge: %w", err)
	}
	if err := gs.removeIncomingEdge(toID, edgeID); err != nil {
		return fmt.Errorf("failed to remove incoming edge: %w", err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDeleteEdge, edge)

	return nil
}

// GetEdge retrieves an edge by ID
func (gs *GraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	defer gs.startQueryTiming()()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)

	edge, exists := gs.edges[edgeID]
	if !exists {
		return nil, ErrEdgeNotFound
	}

	return edge.Clone(), nil
}

// UpdateEdge updates an edge's properties and/or weight
func (gs *GraphStorage) UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	edge, exists := gs.edges[edgeID]
	if !exists {
		return ErrEdgeNotFound
	}

	// Update properties (merge with existing)
	for k, v := range properties {
		edge.Properties[k] = v
	}

	// Update weight if provided
	if weight != nil {
		edge.Weight = *weight
	}

	// Write to WAL for durability
	gs.writeToWAL(wal.OpUpdateEdge, edge)

	return nil
}

// createEdgeLocked is the internal edge creation logic that assumes the lock is already held.
// This follows the DRY principle by extracting common logic used by both CreateEdge and UpsertEdge.
func (gs *GraphStorage) createEdgeLocked(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) {
		return nil, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++

	edge := &Edge{
		ID:         edgeID,
		TenantID:   effectiveTenantID(tenantID),
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	gs.edges[edgeID] = edge

	// Update global type index (for backward compatibility)
	gs.edgesByType[edgeType] = append(gs.edgesByType[edgeType], edgeID)

	// Update tenant-scoped indexes
	gs.addEdgeToTenantIndex(edge)

	if err := gs.storeOutgoingEdge(fromID, edgeID); err != nil {
		return nil, fmt.Errorf("failed to store outgoing edge: %w", err)
	}
	if err := gs.storeIncomingEdge(toID, edgeID); err != nil {
		return nil, fmt.Errorf("failed to store incoming edge: %w", err)
	}

	atomic.AddUint64(&gs.stats.EdgeCount, 1)
	gs.writeToWAL(wal.OpCreateEdge, edge)

	return edge, nil
}

// Edge adjacency helper methods

// storeOutgoingEdge adds an edge to a node's outgoing adjacency list (disk or memory)
func (gs *GraphStorage) storeOutgoingEdge(nodeID, edgeID uint64) error {
	if gs.useDiskBackedEdges {
		existing, err := gs.edgeStore.GetOutgoingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		if err := gs.edgeStore.StoreOutgoingEdges(nodeID, append(existing, edgeID)); err != nil {
			return fmt.Errorf("failed to store outgoing edges for node %d: %w", nodeID, err)
		}
	} else {
		gs.outgoingEdges[nodeID] = append(gs.outgoingEdges[nodeID], edgeID)
	}
	return nil
}

// storeIncomingEdge adds an edge to a node's incoming adjacency list (disk or memory)
func (gs *GraphStorage) storeIncomingEdge(nodeID, edgeID uint64) error {
	if gs.useDiskBackedEdges {
		existing, err := gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			return fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
		}
		if err := gs.edgeStore.StoreIncomingEdges(nodeID, append(existing, edgeID)); err != nil {
			return fmt.Errorf("failed to store incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		gs.incomingEdges[nodeID] = append(gs.incomingEdges[nodeID], edgeID)
	}
	return nil
}

// FindEdgeBetween finds an existing edge between two nodes with a specific type.
// Returns nil if no such edge exists. This is useful for implementing upsert semantics.
func (gs *GraphStorage) FindEdgeBetween(fromID, toID uint64, edgeType string) (*Edge, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	return gs.findEdgeBetweenLocked(fromID, toID, edgeType)
}

// findEdgeBetweenLocked is the internal version that assumes lock is already held
func (gs *GraphStorage) findEdgeBetweenLocked(fromID, toID uint64, edgeType string) (*Edge, error) {
	// Get outgoing edges from source node
	edgeIDs := gs.getEdgeIDsForNode(fromID, true)
	if edgeIDs == nil {
		return nil, nil
	}

	// Search for matching edge
	for _, edgeID := range edgeIDs {
		edge, exists := gs.edges[edgeID]
		if !exists {
			continue
		}
		if edge.ToNodeID == toID && edge.Type == edgeType {
			return edge.Clone(), nil
		}
	}

	return nil, nil
}

// FindAllEdgesBetween finds all edges between two nodes (any type).
// Useful for checking relationship existence regardless of edge type.
func (gs *GraphStorage) FindAllEdgesBetween(fromID, toID uint64) ([]*Edge, error) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	edgeIDs := gs.getEdgeIDsForNode(fromID, true)
	if edgeIDs == nil {
		return []*Edge{}, nil
	}

	var result []*Edge
	for _, edgeID := range edgeIDs {
		edge, exists := gs.edges[edgeID]
		if !exists {
			continue
		}
		if edge.ToNodeID == toID {
			result = append(result, edge.Clone())
		}
	}

	return result, nil
}

// UpsertEdge creates a new edge or updates an existing one between two nodes in the default tenant.
// For multi-tenant operations, use UpsertEdgeWithTenant instead.
func (gs *GraphStorage) UpsertEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) {
	return gs.UpsertEdgeWithTenant(DefaultTenantID, fromID, toID, edgeType, properties, weight)
}

// UpsertEdgeWithTenant creates a new edge or updates an existing one between two nodes for a specific tenant.
// If an edge of the same type already exists between fromID and toID, it updates
// the properties and weight. Otherwise, it creates a new edge.
// Returns the edge (created or updated) and a boolean indicating if it was created (true) or updated (false).
func (gs *GraphStorage) UpsertEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Verify nodes exist
	if err := gs.verifyNodeExists(fromID, "source"); err != nil {
		return nil, false, err
	}
	if err := gs.verifyNodeExists(toID, "target"); err != nil {
		return nil, false, err
	}

	// Check if edge already exists
	existing, err := gs.findEdgeBetweenLocked(fromID, toID, edgeType)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check existing edge: %w", err)
	}

	if existing != nil {
		// Update existing edge
		edge := gs.edges[existing.ID]

		// Merge properties (new values override existing)
		for k, v := range properties {
			edge.Properties[k] = v
		}
		edge.Weight = weight

		// Write to WAL for durability
		gs.writeToWAL(wal.OpUpdateEdge, edge)

		return edge.Clone(), false, nil
	}

	// Create new edge using shared helper
	edge, err := gs.createEdgeLocked(tenantID, fromID, toID, edgeType, properties, weight)
	if err != nil {
		return nil, false, err
	}

	return edge.Clone(), true, nil
}

// DeleteEdgeBetween deletes an edge between two nodes by type.
// Returns true if an edge was deleted, false if no matching edge existed.
func (gs *GraphStorage) DeleteEdgeBetween(fromID, toID uint64, edgeType string) (bool, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Find the edge first
	edgeIDs := gs.getEdgeIDsForNode(fromID, true)
	if edgeIDs == nil {
		return false, nil
	}

	var edgeToDelete *Edge
	for _, edgeID := range edgeIDs {
		edge, exists := gs.edges[edgeID]
		if !exists {
			continue
		}
		if edge.ToNodeID == toID && edge.Type == edgeType {
			edgeToDelete = edge
			break
		}
	}

	if edgeToDelete == nil {
		return false, nil
	}

	// Delete from edges map
	delete(gs.edges, edgeToDelete.ID)

	// Remove from global type index
	gs.removeEdgeFromTypeIndex(edgeType, edgeToDelete.ID)

	// Remove from tenant-scoped indexes
	gs.removeEdgeFromTenantIndex(edgeToDelete)

	// Remove from adjacency
	if err := gs.removeOutgoingEdge(fromID, edgeToDelete.ID); err != nil {
		return false, fmt.Errorf("failed to remove outgoing edge: %w", err)
	}
	if err := gs.removeIncomingEdge(toID, edgeToDelete.ID); err != nil {
		return false, fmt.Errorf("failed to remove incoming edge: %w", err)
	}

	atomicDecrementWithUnderflowProtection(&gs.stats.EdgeCount)
	gs.writeToWAL(wal.OpDeleteEdge, edgeToDelete)

	return true, nil
}
