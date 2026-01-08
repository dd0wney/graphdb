package storage

import (
	"time"
)

// DefaultTenantID is used when no tenant is specified (backward compatibility)
const DefaultTenantID = "default"

// effectiveTenantID returns the tenant ID to use, defaulting to DefaultTenantID if empty
func effectiveTenantID(tenantID string) string {
	if tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
}

// addNodeToTenantIndex adds a node to the tenant-scoped label indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) addNodeToTenantIndex(node *Node) {
	tenantID := effectiveTenantID(node.TenantID)

	// Initialize tenant label map if needed
	if gs.tenantNodesByLabel[tenantID] == nil {
		gs.tenantNodesByLabel[tenantID] = make(map[string][]uint64)
	}

	// Add node to each label's index
	for _, label := range node.Labels {
		gs.tenantNodesByLabel[tenantID][label] = append(
			gs.tenantNodesByLabel[tenantID][label],
			node.ID,
		)
	}

	// Update tenant stats
	gs.incrementTenantNodeCount(tenantID)
}

// removeNodeFromTenantIndex removes a node from the tenant-scoped label indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) removeNodeFromTenantIndex(node *Node) {
	tenantID := effectiveTenantID(node.TenantID)

	labelMap := gs.tenantNodesByLabel[tenantID]
	if labelMap == nil {
		return
	}

	// Remove node from each label's index
	for _, label := range node.Labels {
		ids := labelMap[label]
		for i, id := range ids {
			if id == node.ID {
				labelMap[label] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		// Clean up empty slices
		if len(labelMap[label]) == 0 {
			delete(labelMap, label)
		}
	}

	// Clean up empty tenant map
	if len(labelMap) == 0 {
		delete(gs.tenantNodesByLabel, tenantID)
	}

	// Update tenant stats
	gs.decrementTenantNodeCount(tenantID)
}

// addEdgeToTenantIndex adds an edge to the tenant-scoped type indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) addEdgeToTenantIndex(edge *Edge) {
	tenantID := effectiveTenantID(edge.TenantID)

	// Initialize tenant type map if needed
	if gs.tenantEdgesByType[tenantID] == nil {
		gs.tenantEdgesByType[tenantID] = make(map[string][]uint64)
	}

	// Add edge to type index
	gs.tenantEdgesByType[tenantID][edge.Type] = append(
		gs.tenantEdgesByType[tenantID][edge.Type],
		edge.ID,
	)

	// Update tenant stats
	gs.incrementTenantEdgeCount(tenantID)
}

// removeEdgeFromTenantIndex removes an edge from the tenant-scoped type indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) removeEdgeFromTenantIndex(edge *Edge) {
	tenantID := effectiveTenantID(edge.TenantID)

	typeMap := gs.tenantEdgesByType[tenantID]
	if typeMap == nil {
		return
	}

	// Remove edge from type index
	ids := typeMap[edge.Type]
	for i, id := range ids {
		if id == edge.ID {
			typeMap[edge.Type] = append(ids[:i], ids[i+1:]...)
			break
		}
	}

	// Clean up empty slices
	if len(typeMap[edge.Type]) == 0 {
		delete(typeMap, edge.Type)
	}

	// Clean up empty tenant map
	if len(typeMap) == 0 {
		delete(gs.tenantEdgesByType, tenantID)
	}

	// Update tenant stats
	gs.decrementTenantEdgeCount(tenantID)
}

// GetNodesByLabelForTenant returns all nodes with the given label for a specific tenant.
func (gs *GraphStorage) GetNodesByLabelForTenant(tenantID, label string) []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	labelMap := gs.tenantNodesByLabel[tenantID]
	if labelMap == nil {
		return nil
	}

	nodeIDs := labelMap[label]
	if len(nodeIDs) == 0 {
		return nil
	}

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node, exists := gs.nodes[id]; exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes
}

// GetEdgesByTypeForTenant returns all edges with the given type for a specific tenant.
func (gs *GraphStorage) GetEdgesByTypeForTenant(tenantID, edgeType string) []*Edge {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	typeMap := gs.tenantEdgesByType[tenantID]
	if typeMap == nil {
		return nil
	}

	edgeIDs := typeMap[edgeType]
	if len(edgeIDs) == 0 {
		return nil
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, id := range edgeIDs {
		if edge, exists := gs.edges[id]; exists {
			edges = append(edges, edge.Clone())
		}
	}

	return edges
}

// GetAllNodesForTenant returns all nodes belonging to a specific tenant.
func (gs *GraphStorage) GetAllNodesForTenant(tenantID string) []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	var nodes []*Node
	for _, node := range gs.nodes {
		nodeTenant := effectiveTenantID(node.TenantID)
		if nodeTenant == tenantID {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes
}

// GetAllEdgesForTenant returns all edges belonging to a specific tenant.
func (gs *GraphStorage) GetAllEdgesForTenant(tenantID string) []*Edge {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	var edges []*Edge
	for _, edge := range gs.edges {
		edgeTenant := effectiveTenantID(edge.TenantID)
		if edgeTenant == tenantID {
			edges = append(edges, edge.Clone())
		}
	}

	return edges
}

// GetTenantStats returns usage statistics for a tenant.
func (gs *GraphStorage) GetTenantStats(tenantID string) *TenantStats {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	stats := gs.tenantStats[tenantID]
	if stats == nil {
		return &TenantStats{} // Return empty stats if not tracked
	}

	// Return a copy to prevent mutation
	return &TenantStats{
		NodeCount:    stats.NodeCount,
		EdgeCount:    stats.EdgeCount,
		StorageBytes: stats.StorageBytes,
		LastUpdated:  stats.LastUpdated,
	}
}

// ListTenants returns a list of all tenant IDs that have data.
func (gs *GraphStorage) ListTenants() []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenants := make(map[string]bool)

	// Collect tenants from stats
	for tenantID := range gs.tenantStats {
		tenants[tenantID] = true
	}

	// Collect tenants from node indexes
	for tenantID := range gs.tenantNodesByLabel {
		tenants[tenantID] = true
	}

	// Collect tenants from edge indexes
	for tenantID := range gs.tenantEdgesByType {
		tenants[tenantID] = true
	}

	result := make([]string, 0, len(tenants))
	for tenantID := range tenants {
		result = append(result, tenantID)
	}

	return result
}

// incrementTenantNodeCount increments the node count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) incrementTenantNodeCount(tenantID string) {
	if gs.tenantStats[tenantID] == nil {
		gs.tenantStats[tenantID] = &TenantStats{}
	}
	gs.tenantStats[tenantID].NodeCount++
	gs.tenantStats[tenantID].LastUpdated = time.Now().UnixMilli()
}

// decrementTenantNodeCount decrements the node count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) decrementTenantNodeCount(tenantID string) {
	stats := gs.tenantStats[tenantID]
	if stats == nil {
		return
	}
	if stats.NodeCount > 0 {
		stats.NodeCount--
	}
	stats.LastUpdated = time.Now().UnixMilli()
}

// incrementTenantEdgeCount increments the edge count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) incrementTenantEdgeCount(tenantID string) {
	if gs.tenantStats[tenantID] == nil {
		gs.tenantStats[tenantID] = &TenantStats{}
	}
	gs.tenantStats[tenantID].EdgeCount++
	gs.tenantStats[tenantID].LastUpdated = time.Now().UnixMilli()
}

// decrementTenantEdgeCount decrements the edge count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) decrementTenantEdgeCount(tenantID string) {
	stats := gs.tenantStats[tenantID]
	if stats == nil {
		return
	}
	if stats.EdgeCount > 0 {
		stats.EdgeCount--
	}
	stats.LastUpdated = time.Now().UnixMilli()
}

// CountNodesForTenant returns the number of nodes for a tenant.
func (gs *GraphStorage) CountNodesForTenant(tenantID string) uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	stats := gs.tenantStats[tenantID]
	if stats == nil {
		return 0
	}
	return stats.NodeCount
}

// CountEdgesForTenant returns the number of edges for a tenant.
func (gs *GraphStorage) CountEdgesForTenant(tenantID string) uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	stats := gs.tenantStats[tenantID]
	if stats == nil {
		return 0
	}
	return stats.EdgeCount
}

// GetLabelsForTenant returns all unique labels used by a tenant.
func (gs *GraphStorage) GetLabelsForTenant(tenantID string) []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	labelMap := gs.tenantNodesByLabel[tenantID]
	if labelMap == nil {
		return nil
	}

	labels := make([]string, 0, len(labelMap))
	for label := range labelMap {
		labels = append(labels, label)
	}

	return labels
}

// GetEdgeTypesForTenant returns all unique edge types used by a tenant.
func (gs *GraphStorage) GetEdgeTypesForTenant(tenantID string) []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tenantID = effectiveTenantID(tenantID)

	typeMap := gs.tenantEdgesByType[tenantID]
	if typeMap == nil {
		return nil
	}

	types := make([]string, 0, len(typeMap))
	for edgeType := range typeMap {
		types = append(types, edgeType)
	}

	return types
}
