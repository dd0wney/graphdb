package storage

import (
	"errors"
	"fmt"
	"time"

	"github.com/dd0wney/graphdb/pkg/tenantid"
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
		gs.tenantNodesByLabel[tenantID] = make(labelIndex)
	}

	// Add node to each label's index
	for _, label := range node.Labels {
		addToLabelIndex(gs.tenantNodesByLabel[tenantID], label, node.ID)
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
			removeFromLabelIndexSet(labelMap, label, node.ID)
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
		gs.tenantEdgesByType[tenantID] = make(labelIndex)
	}

	// Add edge to type index
	addToLabelIndex(gs.tenantEdgesByType[tenantID], edge.Type, edge.ID)

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
		removeFromLabelIndexSet(typeMap, edge.Type, edge.ID)

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
	nodeIDs := gs.membershipNodeIDsByLabelLocked(tid, label)
	nodes := make([]*Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		if node, exists := gs.resolveNodeRefLocked(id); exists {
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
	return len(gs.membershipNodeIDsByLabelLocked(tid, label))
}

// GetEdgesByTypeForTenant returns all edges with the given type for a specific tenant.
func (gs *GraphStorage) GetEdgesByTypeForTenant(tenantID, edgeType string) []*Edge {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)
	edgeIDs := gs.membershipEdgeIDsByTypeLocked(tid, edgeType)
	edges := make([]*Edge, 0, len(edgeIDs))
	for _, id := range edgeIDs {
		if edge, exists := gs.resolveEdgeRefLocked(id); exists {
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
	ids := gs.membershipNodeIDsForTenantLocked(tid)
	gs.mu.RUnlock()

	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		gs.rlockShard(id)
		node, exists := gs.resolveNodeRefLocked(id)
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
	ids := gs.membershipEdgeIDsForTenantLocked(tid)
	gs.mu.RUnlock()

	edges := make([]*Edge, 0, len(ids))
	for _, id := range ids {
		gs.rlockShard(id)
		edge, exists := gs.resolveEdgeRefLocked(id)
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

	tids := gs.membershipTenantsLocked()
	// Convert to []string for the public API (deferred to A3 to migrate to []TenantID).
	result := make([]string, 0, len(tids))
	for _, tid := range tids {
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

// DeleteTenant cascade-deletes all of a tenant's graph data — every node and
// edge it owns, plus the per-tenant indexes/counts those deletes maintain, plus
// the tenant's vector-index definitions. Returns how many nodes and edges were
// removed. The default tenant cannot be deleted.
//
// Mechanism: loop DeleteNodeForTenant over the tenant's nodes (reusing the
// tested cascade — each node delete removes its incident edges and maintains
// global+per-tenant indexes and the WAL), then a defensive pass over any edges
// not already cascaded (edges are intra-tenant, so the node pass usually clears
// them). Per-node ErrNodeNotFound / per-edge ErrEdgeNotFound are tolerated so
// the cascade is idempotent and re-runnable. Vector index definitions are
// dropped via the WAL-logged DropVectorIndexForTenant (#320) so the drop
// survives a crash, mirroring the WAL-durable node/edge deletes.
//
// Search indexes (on-disk LSA, in-memory FTS) are owned by the API server, not
// GraphStorage — the caller drops those (see handleDeleteTenant).
//
// Cost is O(nodes+edges) individually-locked deletes; acceptable for a
// correctness fix. A bulk in-storage purge is a future optimization.
func (gs *GraphStorage) DeleteTenant(tenantID string) (nodesDeleted, edgesDeleted int, err error) {
	if effectiveTenantID(tenantID) == tenantid.Default {
		return 0, 0, fmt.Errorf("cannot delete the default tenant")
	}

	for _, n := range gs.GetAllNodesForTenant(tenantID) {
		if derr := gs.DeleteNodeForTenant(n.ID, tenantID); derr != nil {
			if errors.Is(derr, ErrNodeNotFound) {
				continue // already removed via another node's edge cascade
			}
			return nodesDeleted, edgesDeleted, fmt.Errorf("delete node %d: %w", n.ID, derr)
		}
		nodesDeleted++
	}

	// Defensive sweep: any edges the node pass didn't cascade.
	for _, e := range gs.GetAllEdgesForTenant(tenantID) {
		if derr := gs.DeleteEdgeForTenant(e.ID, tenantID); derr != nil {
			if errors.Is(derr, ErrEdgeNotFound) {
				continue
			}
			return nodesDeleted, edgesDeleted, fmt.Errorf("delete edge %d: %w", e.ID, derr)
		}
		edgesDeleted++
	}

	// Drop the tenant's vector-index definitions (WAL-durable). Best-effort:
	// a concurrently-dropped index just surfaces as an error we can ignore.
	for _, prop := range gs.ListVectorIndexesForTenant(tenantID) {
		_ = gs.DropVectorIndexForTenant(tenantID, prop)
	}

	return nodesDeleted, edgesDeleted, nil
}

// GetLabelsForTenant returns all unique labels used by a tenant.
func (gs *GraphStorage) GetLabelsForTenant(tenantID string) []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)
	return gs.membershipLabelsForTenantLocked(tid)
}

// GetEdgeTypesForTenant returns all unique edge types used by a tenant.
func (gs *GraphStorage) GetEdgeTypesForTenant(tenantID string) []string {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	tid := effectiveTenantID(tenantID)
	return gs.membershipEdgeTypesForTenantLocked(tid)
}
