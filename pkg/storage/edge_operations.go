package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// CreateEdge creates a new edge between two nodes in the default
// tenant. Tenant-blind on node verification — used by replication
// (which lands replicated writes in the default tenant; see audit
// task A8) and by other intentionally tenant-blind callers (temporal
// snapshots, CLI, examples). Multi-tenant API callers must use
// CreateEdgeWithTenant.
//
// Existence (not tenancy) of the from/to nodes is still validated.
func (gs *GraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	// Deferred WAL wait runs AFTER gs.mu.Unlock (defers are LIFO), so the
	// durability wait happens off-lock and concurrent writers can fill the
	// batch (group commit, Track P item 1). nil handle on the error paths
	// below => no-op wait.
	var walPending *wal.Pending
	defer func() { gs.waitWALPending(wal.OpCreateEdge, walPending) }()
	defer gs.mu.Unlock()

	if err := gs.verifyNodeExists(fromID, "source"); err != nil {
		return nil, err
	}
	if err := gs.verifyNodeExists(toID, "target"); err != nil {
		return nil, err
	}

	edge, p, err := gs.createEdgeWithTenantNoVerify(DefaultTenantID, fromID, toID, edgeType, properties, weight)
	walPending = p
	return edge, err
}

// CreateEdgeWithTenant creates a new edge between two nodes for a
// specific tenant. From/to nodes must belong to the same tenant —
// cross-tenant or missing surfaces as ErrNodeNotFound (unified to
// avoid existence-leak side channel).
//
// Audit A6a follow-up (2026-05-08): closes the residual gap from A6a
// where this method accepted node IDs owned by other tenants. The
// /vector-search, /traverse and /shortest-path tenant scoping now
// rests on this guarantee — see the updated comments in
// pkg/api/handlers_edges.go and pkg/algorithms/shortest_path.go.
func (gs *GraphStorage) CreateEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. See CreateEdge.
	var walPending *wal.Pending
	defer func() { gs.waitWALPending(wal.OpCreateEdge, walPending) }()
	defer gs.mu.Unlock()

	if err := gs.verifyNodeExistsForTenant(fromID, "source", tenantID); err != nil {
		return nil, err
	}
	if err := gs.verifyNodeExistsForTenant(toID, "target", tenantID); err != nil {
		return nil, err
	}

	edge, p, err := gs.createEdgeWithTenantNoVerify(tenantID, fromID, toID, edgeType, properties, weight)
	walPending = p
	return edge, err
}

// createEdgeWithTenantNoVerify is the shared edge-creation core. It
// assumes the caller has already taken gs.mu.Lock and (when relevant)
// validated tenant ownership of the source/target nodes.
//
// CreateEdge calls this directly (tenant-blind) for replication and
// other legitimately tenant-blind paths; CreateEdgeWithTenant runs
// the tenant-strict node check first.
func (gs *GraphStorage) createEdgeWithTenantNoVerify(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, *wal.Pending, error) {
	edge, walPending, err := gs.createEdgeLocked(tenantID, fromID, toID, edgeType, properties, weight)
	if err != nil {
		return nil, nil, err
	}
	return edge.Clone(), walPending, nil
}

// DeleteEdgeForTenant deletes an edge by ID, scoped to the given tenant.
// Returns ErrEdgeNotFound on missing or cross-tenant.
//
// See GetNodeForTenant in node_operations.go for the rationale on the
// unified missing-vs-cross-tenant error.
func (gs *GraphStorage) DeleteEdgeForTenant(edgeID uint64, tenantID string) error {
	// Tenant validation under shard read lock; release before delegating
	// to DeleteEdge which acquires the global write lock. The lock-drop
	// window is benign — tenant IDs are immutable after creation, and
	// "edge deleted by another goroutine" surfaces as ErrEdgeNotFound
	// from DeleteEdge.
	gs.rlockShard(edgeID)
	if _, err := gs.getEdgeRefForTenant(edgeID, tenantID); err != nil {
		gs.runlockShard(edgeID)
		return err
	}
	gs.runlockShard(edgeID)
	return gs.DeleteEdge(edgeID)
}

// getEdgeRefForTenant returns the live edge pointer (NO clone) after
// validating tenant ownership. Caller MUST hold the appropriate shard
// read lock for the duration the returned pointer is used.
//
// Internal use only — package-private. Mirrors getNodeRefForTenant in
// node_operations.go.
func (gs *GraphStorage) getEdgeRefForTenant(edgeID uint64, tenantID string) (*Edge, error) {
	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		return nil, ErrEdgeNotFound
	}
	expectedTenant := effectiveTenantID(tenantID).String()
	if edge.TenantID != expectedTenant {
		// Cross-tenant: same error as missing to avoid existence leak.
		return nil, ErrEdgeNotFound
	}
	return edge, nil
}

// DeleteEdge deletes an edge by ID.
//
// Tenant-blind. New callers should prefer DeleteEdgeForTenant.
func (gs *GraphStorage) DeleteEdge(edgeID uint64) error {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. nil handle (not-found path) => no-op wait.
	var walPending *wal.Pending
	defer func() { gs.waitWALPending(wal.OpDeleteEdge, walPending) }()
	defer gs.mu.Unlock()

	// Lookup + delete on edgeShards under the per-shard write lock so
	// concurrent GetEdge readers see a consistent map. gs.mu.Lock above
	// excludes other writers; lockShard excludes the readers. A4-edges.
	gs.lockShard(edgeID)
	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		gs.unlockShard(edgeID)
		return fmt.Errorf("edge %d not found", edgeID)
	}
	fromID := edge.FromNodeID
	toID := edge.ToNodeID
	gs.deleteEdgeShardEntry(edgeID)
	gs.unlockShard(edgeID)

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

	// Enqueue under gs.mu; the deferred wait above blocks off-lock.
	walPending = gs.enqueueWAL(wal.OpDeleteEdge, edge)

	return nil
}

// GetEdgeForTenant retrieves an edge by ID, scoped to the given tenant.
// Returns ErrEdgeNotFound on missing or cross-tenant.
func (gs *GraphStorage) GetEdgeForTenant(edgeID uint64, tenantID string) (*Edge, error) {
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)
	edge, err := gs.getEdgeRefForTenant(edgeID, tenantID)
	if err != nil {
		return nil, err
	}
	return edge.Clone(), nil
}

// GetEdge retrieves an edge by ID.
//
// Tenant-blind. New callers should prefer GetEdgeForTenant.
//
// Reader takes the per-shard read lock; writers (CreateEdge,
// UpdateEdge, DeleteEdge, UpsertEdge) hold gs.mu.Lock plus
// lockShard(edgeID) for the edgeShards mutation, so readers and
// writers exclude correctly on shardLocks[edgeID & shardMask].
// Audit task A4-edges (2026-05-10) closed the prior shared-map race
// by partitioning gs.edges into [256]map[uint64]*Edge.
func (gs *GraphStorage) GetEdge(edgeID uint64) (*Edge, error) {
	defer gs.startQueryTiming()()

	// Use shard-level read lock for better concurrency
	gs.rlockShard(edgeID)
	defer gs.runlockShard(edgeID)

	edge, exists := gs.lookupEdgeShard(edgeID)
	if !exists {
		return nil, ErrEdgeNotFound
	}

	return edge.Clone(), nil
}

// UpdateEdgeForTenant updates an edge's properties and/or weight, scoped
// to the given tenant. Returns ErrEdgeNotFound on missing or cross-tenant.
func (gs *GraphStorage) UpdateEdgeForTenant(edgeID uint64, properties map[string]Value, weight *float64, tenantID string) error {
	// Tenant validation under shard read lock; release before delegating
	// to UpdateEdge (which acquires the global write lock). Same lock-drop
	// rationale as DeleteEdgeForTenant.
	gs.rlockShard(edgeID)
	if _, err := gs.getEdgeRefForTenant(edgeID, tenantID); err != nil {
		gs.runlockShard(edgeID)
		return err
	}
	gs.runlockShard(edgeID)
	return gs.UpdateEdge(edgeID, properties, weight)
}

// UpdateEdge updates an edge's properties and/or weight.
//
// Tenant-blind. New callers should prefer UpdateEdgeForTenant.
func (gs *GraphStorage) UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock AND gs.unlockShard (LIFO) —
	// group commit, Track P item 1. nil handle (not-found path) => no-op wait.
	var walPending *wal.Pending
	defer func() { gs.waitWALPending(wal.OpUpdateEdge, walPending) }()
	defer gs.mu.Unlock()

	// lockShard excludes concurrent GetEdge readers from this edge's
	// shard while we mutate edge.Properties / edge.Weight. The
	// edge struct's pointer is stable across the unlock — Edge is only
	// removed via deleteEdgeShardEntry which also takes lockShard, and
	// gs.mu.Lock prevents that from racing with this UpdateEdge.
	// A4-edges.
	gs.lockShard(edgeID)
	defer gs.unlockShard(edgeID)

	edge, exists := gs.lookupEdgeShard(edgeID)
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

	// Enqueue under gs.mu; the deferred wait above blocks off-lock.
	walPending = gs.enqueueWAL(wal.OpUpdateEdge, edge)

	return nil
}

// createEdgeLocked is the internal edge creation logic that assumes the lock is already held.
// This follows the DRY principle by extracting common logic used by both CreateEdge and UpsertEdge.
func (gs *GraphStorage) createEdgeLocked(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, *wal.Pending, error) {
	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) {
		return nil, nil, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++

	edge := &Edge{
		ID: edgeID,
		// Edge.TenantID is still string — A3 will migrate it. For now,
		// .String() preserves the existing wire format.
		TenantID:   effectiveTenantID(tenantID).String(),
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		CreatedAt:  time.Now().Unix(),
	}

	// Publish the edge into all in-memory structures + indexes (the shared
	// persist helper — same logic Transaction.Commit uses). Endpoint
	// tenant-ownership validation stays the public caller's responsibility
	// (CreateEdge/UpsertEdge via verifyNodeExistsForTenant), unchanged.
	if err := gs.persistEdgeLocked(edge); err != nil {
		return nil, nil, err
	}

	// Enqueue under gs.mu (preserves WAL order); the public caller waits on the
	// returned handle after releasing gs.mu (group commit, Track P item 1).
	walPending := gs.enqueueWAL(wal.OpCreateEdge, edge)

	return edge, walPending, nil
}

// persistEdgeLocked publishes a fully-built edge (ID, TenantID, endpoints,
// Type, Properties, Weight, timestamp already set) into every in-memory
// structure — shard map, global type index, per-tenant index, adjacency
// lists, stats. It does NOT write the WAL or validate endpoint ownership: the
// caller chooses durability (single-op enqueueWAL on the direct create path;
// one atomic batch for Transaction.Commit) and is responsible for endpoint-
// tenant validation. Caller must hold gs.mu.Lock. Single source of truth for
// "persist an edge," shared by createEdgeLocked and Transaction.Commit.
func (gs *GraphStorage) persistEdgeLocked(edge *Edge) error {
	// lockShard excludes concurrent GetEdge readers from this edge's shard
	// while we write edgeShards (A4-edges).
	gs.lockShard(edge.ID)
	gs.storeEdgeInShard(edge)
	gs.unlockShard(edge.ID)

	// Global type index (tenant-blind, backward compatibility).
	gs.edgesByType[edge.Type] = append(gs.edgesByType[edge.Type], edge.ID)

	// Per-tenant indexes (type + enumeration set + stats).
	gs.addEdgeToTenantIndex(edge)

	if err := gs.storeOutgoingEdge(edge.FromNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store outgoing edge: %w", err)
	}
	if err := gs.storeIncomingEdge(edge.ToNodeID, edge.ID); err != nil {
		return fmt.Errorf("failed to store incoming edge: %w", err)
	}

	atomic.AddUint64(&gs.stats.EdgeCount, 1)
	return nil
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
		edge, exists := gs.lookupEdgeShard(edgeID)
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
		edge, exists := gs.lookupEdgeShard(edgeID)
		if !exists {
			continue
		}
		if edge.ToNodeID == toID {
			result = append(result, edge.Clone())
		}
	}

	return result, nil
}

// UpsertEdge creates a new edge or updates an existing one between
// two nodes in the default tenant. Tenant-blind on node verification;
// see CreateEdge for the rationale. Existence is still validated.
func (gs *GraphStorage) UpsertEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. walOp reflects the branch taken (create vs update) for logging.
	var walPending *wal.Pending
	walOp := wal.OpCreateEdge
	defer func() { gs.waitWALPending(walOp, walPending) }()
	defer gs.mu.Unlock()

	if err := gs.verifyNodeExists(fromID, "source"); err != nil {
		return nil, false, err
	}
	if err := gs.verifyNodeExists(toID, "target"); err != nil {
		return nil, false, err
	}

	edge, created, p, err := gs.upsertEdgeWithTenantNoVerify(DefaultTenantID, fromID, toID, edgeType, properties, weight)
	walPending = p
	if !created {
		walOp = wal.OpUpdateEdge
	}
	return edge, created, err
}

// UpsertEdgeWithTenant creates a new edge or updates an existing one
// between two nodes for a specific tenant. From/to nodes must belong
// to the same tenant — cross-tenant or missing surfaces as
// ErrNodeNotFound (audit A6a follow-up; see CreateEdgeWithTenant).
func (gs *GraphStorage) UpsertEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. See UpsertEdge.
	var walPending *wal.Pending
	walOp := wal.OpCreateEdge
	defer func() { gs.waitWALPending(walOp, walPending) }()
	defer gs.mu.Unlock()

	if err := gs.verifyNodeExistsForTenant(fromID, "source", tenantID); err != nil {
		return nil, false, err
	}
	if err := gs.verifyNodeExistsForTenant(toID, "target", tenantID); err != nil {
		return nil, false, err
	}

	edge, created, p, err := gs.upsertEdgeWithTenantNoVerify(tenantID, fromID, toID, edgeType, properties, weight)
	walPending = p
	if !created {
		walOp = wal.OpUpdateEdge
	}
	return edge, created, err
}

// upsertEdgeWithTenantNoVerify is the shared upsert core. Caller
// must hold gs.mu.Lock and (when relevant) have validated tenant
// ownership of the source/target nodes.
func (gs *GraphStorage) upsertEdgeWithTenantNoVerify(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, *wal.Pending, error) {
	existing, err := gs.findEdgeBetweenLocked(fromID, toID, edgeType)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to check existing edge: %w", err)
	}

	if existing != nil {
		// Update existing edge under per-shard lock to exclude
		// concurrent GetEdge readers. A4-edges.
		gs.lockShard(existing.ID)
		edge, _ := gs.lookupEdgeShard(existing.ID)

		// Merge properties (new values override existing)
		for k, v := range properties {
			edge.Properties[k] = v
		}
		edge.Weight = weight
		gs.unlockShard(existing.ID)

		// Enqueue under gs.mu (preserves WAL order); caller waits off-lock.
		walPending := gs.enqueueWAL(wal.OpUpdateEdge, edge)

		return edge.Clone(), false, walPending, nil
	}

	// Create new edge using shared helper
	edge, walPending, err := gs.createEdgeLocked(tenantID, fromID, toID, edgeType, properties, weight)
	if err != nil {
		return nil, false, nil, err
	}

	return edge.Clone(), true, walPending, nil
}

// DeleteEdgeBetween deletes an edge between two nodes by type.
// Returns true if an edge was deleted, false if no matching edge existed.
func (gs *GraphStorage) DeleteEdgeBetween(fromID, toID uint64, edgeType string) (bool, error) {
	gs.mu.Lock()
	// Deferred WAL wait runs after gs.mu.Unlock (LIFO) — group commit, Track P
	// item 1. nil handle (no-matching-edge paths) => no-op wait.
	var walPending *wal.Pending
	defer func() { gs.waitWALPending(wal.OpDeleteEdge, walPending) }()
	defer gs.mu.Unlock()

	// Find the edge first
	edgeIDs := gs.getEdgeIDsForNode(fromID, true)
	if edgeIDs == nil {
		return false, nil
	}

	// One-edge-at-a-time shard locking for the search loop. gs.mu.Lock
	// (held above) excludes other writers; lockShard excludes readers
	// per shard during each lookup. A4-edges.
	var edgeToDelete *Edge
	for _, edgeID := range edgeIDs {
		gs.rlockShard(edgeID)
		edge, exists := gs.lookupEdgeShard(edgeID)
		gs.runlockShard(edgeID)
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

	// Delete from edges shard under write lock.
	gs.lockShard(edgeToDelete.ID)
	gs.deleteEdgeShardEntry(edgeToDelete.ID)
	gs.unlockShard(edgeToDelete.ID)

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
	// Enqueue under gs.mu; the deferred wait above blocks off-lock.
	walPending = gs.enqueueWAL(wal.OpDeleteEdge, edgeToDelete)

	return true, nil
}
