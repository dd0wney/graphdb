package storage

import (
	"sort"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
)

// DefaultTenantID is used when no tenant is specified (backward compatibility).
// String form for use with public APIs that still take "tenantID string"
// parameters; mirrors tenantid.Default for the typed code paths.
const DefaultTenantID = "default"

// effectiveTenantID returns the tenant ID to use, defaulting to
// tenantid.Default if the input is empty. Internal helper; converts at
// the boundary so internal map accesses use the typed form.
func effectiveTenantID(tenantID string) tenantid.TenantID {
	if tenantID == "" {
		return tenantid.Default
	}
	return tenantid.TenantID(tenantID)
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

	// Add node to the per-tenant enumeration set. This is the only index
	// that captures unlabeled nodes, so it is what GetAllNodesForTenant
	// reads. O(1) set insert; idempotent.
	if gs.tenantNodeIDs[tenantID] == nil {
		gs.tenantNodeIDs[tenantID] = make(map[uint64]struct{})
	}
	gs.tenantNodeIDs[tenantID][node.ID] = struct{}{}

	// Update tenant stats
	gs.incrementTenantNodeCount(tenantID)
}

// removeNodeFromTenantIndex removes a node from the tenant-scoped label indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) removeNodeFromTenantIndex(node *Node) {
	tenantID := effectiveTenantID(node.TenantID)

	// Label-index removal is guarded locally rather than via an early
	// return: an unlabeled node never has a label-map entry to remove, but
	// it IS in the enumeration set and the stats — both of which must still
	// be maintained. (The previous early-return on a nil label map also
	// skipped the stats decrement, drifting NodeCount for tenants whose
	// label map had already been GC'd — e.g. a tenant of only unlabeled
	// nodes after its first delete.)
	if labelMap := gs.tenantNodesByLabel[tenantID]; labelMap != nil {
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
	}

	// Remove from the per-tenant enumeration set; drop the tenant's set
	// entirely once it empties so an offboarded tenant leaves no residue.
	if idSet := gs.tenantNodeIDs[tenantID]; idSet != nil {
		delete(idSet, node.ID)
		if len(idSet) == 0 {
			delete(gs.tenantNodeIDs, tenantID)
		}
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

	// Add edge to the per-tenant enumeration set (the index
	// GetAllEdgesForTenant reads). O(1) set insert; idempotent.
	if gs.tenantEdgeIDs[tenantID] == nil {
		gs.tenantEdgeIDs[tenantID] = make(map[uint64]struct{})
	}
	gs.tenantEdgeIDs[tenantID][edge.ID] = struct{}{}

	// Update tenant stats
	gs.incrementTenantEdgeCount(tenantID)
}

// removeEdgeFromTenantIndex removes an edge from the tenant-scoped type indexes.
// Must be called with appropriate lock held.
func (gs *GraphStorage) removeEdgeFromTenantIndex(edge *Edge) {
	tenantID := effectiveTenantID(edge.TenantID)

	// Local guard rather than an early return, for the same reason as
	// removeNodeFromTenantIndex: the enumeration set and stats must be
	// maintained even when the type map is absent (e.g. an empty-type edge,
	// or a tenant whose type map was already GC'd), which the previous
	// early-return-on-nil skipped — drifting EdgeCount.
	if typeMap := gs.tenantEdgesByType[tenantID]; typeMap != nil {
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
	}

	// Remove from the per-tenant enumeration set; drop the tenant's set
	// once empty so an offboarded tenant leaves no residue.
	if idSet := gs.tenantEdgeIDs[tenantID]; idSet != nil {
		delete(idSet, edge.ID)
		if len(idSet) == 0 {
			delete(gs.tenantEdgeIDs, tenantID)
		}
	}

	// Update tenant stats
	gs.decrementTenantEdgeCount(tenantID)
}

// GetNodesByLabelForTenant returns all nodes with the given label for a specific tenant.
func (gs *GraphStorage) GetNodesByLabelForTenant(tenantID, label string) []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)

	labelMap := gs.tenantNodesByLabel[tid]
	if labelMap == nil {
		return nil
	}

	nodeIDs := labelMap[label]
	if len(nodeIDs) == 0 {
		return nil
	}

	nodes := make([]*Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node, exists := gs.lookupNodeShard(id); exists {
			nodes = append(nodes, node.Clone())
		}
	}

	return nodes
}

// CountNodesByLabelForTenant returns how many nodes a tenant has with the
// given label, reading len(index) directly instead of cloning the whole
// bucket the way len(GetNodesByLabelForTenant(...)) does. The label index
// is O(1) to size; the previous count path materialized and deep-cloned
// every node in the bucket — for a 50k-node label that is 50k Clone() calls
// under gs.mu.RLock just to discard them and take a length (audit M1).
func (gs *GraphStorage) CountNodesByLabelForTenant(tenantID, label string) int {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)
	labelMap := gs.tenantNodesByLabel[tid]
	if labelMap == nil {
		return 0
	}
	return len(labelMap[label])
}

// GetEdgesByTypeForTenant returns all edges with the given type for a specific tenant.
func (gs *GraphStorage) GetEdgesByTypeForTenant(tenantID, edgeType string) []*Edge {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)

	typeMap := gs.tenantEdgesByType[tid]
	if typeMap == nil {
		return nil
	}

	edgeIDs := typeMap[edgeType]
	if len(edgeIDs) == 0 {
		return nil
	}

	edges := make([]*Edge, 0, len(edgeIDs))
	for _, id := range edgeIDs {
		if edge, exists := gs.lookupEdgeShard(id); exists {
			edges = append(edges, edge.Clone())
		}
	}

	return edges
}

// GetAllNodesForTenant returns all nodes belonging to a specific tenant.
//
// It enumerates the tenant's own node IDs from tenantNodeIDs (O(tenant
// size)) rather than scanning every shard across all tenants and filtering
// (the former O(total-DB) cross-tenant amplification — Track P / H4). IDs
// are collected under gs.mu.RLock then cloned under per-shard RLocks after
// release, so the global lock is not held across the clone loop and writers
// are not stalled (the A4 read pattern, see GetNodeForTenant).
//
// Consequence of the lock split: the result is no longer an atomic snapshot
// of the tenant at a single instant — a node deleted between the ID
// collection and its clone is simply skipped (same tradeoff A4 accepted for
// GetNode). IDs are returned in ascending order for deterministic pagination.
func (gs *GraphStorage) GetAllNodesForTenant(tenantID string) []*Node {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	idSet := gs.tenantNodeIDs[tid]
	ids := make([]uint64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	gs.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		gs.rlockShard(id)
		node, exists := gs.lookupNodeShard(id)
		if exists {
			node = node.Clone()
		}
		gs.runlockShard(id)
		if exists {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// GetAllEdgesForTenant returns all edges belonging to a specific tenant.
//
// Edge analogue of GetAllNodesForTenant: it enumerates the tenant's own edge
// IDs from tenantEdgeIDs (O(tenant size)) instead of scanning every shard
// across all tenants and filtering (the former O(total-DB) cross-tenant
// amplification that also stalled writers — Track P / H4). IDs are collected
// under gs.mu.RLock, sorted ascending for deterministic pagination, then
// cloned under per-shard RLocks after release. Same non-atomic-snapshot
// tradeoff as GetAllNodesForTenant: an edge deleted between collection and
// clone is skipped.
func (gs *GraphStorage) GetAllEdgesForTenant(tenantID string) []*Edge {
	tid := effectiveTenantID(tenantID)

	gs.mu.RLock()
	idSet := gs.tenantEdgeIDs[tid]
	ids := make([]uint64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	gs.mu.RUnlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	edges := make([]*Edge, 0, len(ids))
	for _, id := range ids {
		gs.rlockShard(id)
		edge, exists := gs.lookupEdgeShard(id)
		if exists {
			edge = edge.Clone()
		}
		gs.runlockShard(id)
		if exists {
			edges = append(edges, edge)
		}
	}

	return edges
}

// GetTenantStats returns usage statistics for a tenant.
func (gs *GraphStorage) GetTenantStats(tenantID string) *TenantStats {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)

	stats := gs.tenantStats[tid]
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

	tenants := make(map[tenantid.TenantID]bool)

	// Collect tenants from stats
	for tid := range gs.tenantStats {
		tenants[tid] = true
	}

	// Collect tenants from node indexes
	for tid := range gs.tenantNodesByLabel {
		tenants[tid] = true
	}

	// Collect tenants from edge indexes
	for tid := range gs.tenantEdgesByType {
		tenants[tid] = true
	}

	// Convert to []string for the public API (deferred to A3 to migrate to []TenantID).
	result := make([]string, 0, len(tenants))
	for tid := range tenants {
		result = append(result, tid.String())
	}

	return result
}

// incrementTenantNodeCount increments the node count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) incrementTenantNodeCount(tenantID tenantid.TenantID) {
	if gs.tenantStats[tenantID] == nil {
		gs.tenantStats[tenantID] = &TenantStats{}
	}
	gs.tenantStats[tenantID].NodeCount++
	gs.tenantStats[tenantID].LastUpdated = time.Now().UnixMilli()
}

// decrementTenantNodeCount decrements the node count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) decrementTenantNodeCount(tenantID tenantid.TenantID) {
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
func (gs *GraphStorage) incrementTenantEdgeCount(tenantID tenantid.TenantID) {
	if gs.tenantStats[tenantID] == nil {
		gs.tenantStats[tenantID] = &TenantStats{}
	}
	gs.tenantStats[tenantID].EdgeCount++
	gs.tenantStats[tenantID].LastUpdated = time.Now().UnixMilli()
}

// decrementTenantEdgeCount decrements the edge count for a tenant.
// Must be called with appropriate lock held.
func (gs *GraphStorage) decrementTenantEdgeCount(tenantID tenantid.TenantID) {
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

	tid := effectiveTenantID(tenantID)

	stats := gs.tenantStats[tid]
	if stats == nil {
		return 0
	}
	return stats.NodeCount
}

// CountEdgesForTenant returns the number of edges for a tenant.
func (gs *GraphStorage) CountEdgesForTenant(tenantID string) uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)

	stats := gs.tenantStats[tid]
	if stats == nil {
		return 0
	}
	return stats.EdgeCount
}

// GetLabelsForTenant returns all unique labels used by a tenant.
func (gs *GraphStorage) GetLabelsForTenant(tenantID string) []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)

	labelMap := gs.tenantNodesByLabel[tid]
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

	tid := effectiveTenantID(tenantID)

	typeMap := gs.tenantEdgesByType[tid]
	if typeMap == nil {
		return nil
	}

	types := make([]string, 0, len(typeMap))
	for edgeType := range typeMap {
		types = append(types, edgeType)
	}

	return types
}
