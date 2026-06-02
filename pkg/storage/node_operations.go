package storage

import (
	"context"
	"fmt"
	"log"
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
//
// Lock discipline (R2.1, S11 spike §7.4): the gs.mu.Lock is released
// before notifyNodeCreated runs so observer code never executes under
// any storage lock. Direct unlock + nil-check on err mirrors what
// `defer gs.mu.Unlock()` would do but lets the notify call land after
// the lock release.
func (gs *GraphStorage) CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error) {
	gs.mu.Lock()
	node, walPending, err := gs.createNodeLocked(tenantID, labels, properties)
	gs.mu.Unlock()
	// Wait for WAL durability OUTSIDE gs.mu so concurrent writers can fill the
	// same batch (group commit, Track P item 1). nil handle = synchronous path
	// (already durable). Fail-soft: a flush error is logged, not propagated,
	// matching the pre-split writeToWAL contract.
	gs.waitWALPending(wal.OpCreateNode, walPending)
	if err == nil && node != nil {
		gs.notifyNodeCreated(context.Background(), node)
	}
	return node, err
}

// CreateNodeWithUniquePropertyForTenant creates a node only if no other
// node in the same tenant already has the same value for
// (uniqueLabel, uniquePropertyKey). The check + create runs under a
// single gs.mu.Lock acquisition, so two concurrent calls cannot both
// observe "no conflict" and both create. On conflict, returns a
// *UniqueConstraintError (errors.Is matches ErrUniqueConstraintViolation).
//
// uniqueLabel must be present in labels and the new properties must
// contain uniquePropertyKey. The constraint is enforced for the named
// label only, mirroring the per-label scope in pkg/constraints —
// nodes with other labels can hold the same property value.
//
// Introduced for B-lite (docs/COORD_DEPLOY_SPIKE_2026-05-10.md §5.2 /
// §10 PR 1) so the GraphQL :Claim resolver can enforce one active claim
// per for_task. Generic enough to reuse if other coord types need
// uniqueness; a fully general HTTP/GraphQL constraint API (B-full) is
// still deferred per the spike.
func (gs *GraphStorage) CreateNodeWithUniquePropertyForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
	uniqueLabel string,
	uniquePropertyKey string,
) (*Node, error) {
	if uniqueLabel == "" || uniquePropertyKey == "" {
		return nil, fmt.Errorf("uniqueLabel and uniquePropertyKey are required")
	}

	// Caller-side sanity: the new node must carry the labelled property
	// so the uniqueness rule is meaningful.
	if !containsString(labels, uniqueLabel) {
		return nil, fmt.Errorf("uniqueLabel %q must be present in labels", uniqueLabel)
	}
	newVal, ok := properties[uniquePropertyKey]
	if !ok {
		return nil, fmt.Errorf("property %q is required for uniqueness check", uniquePropertyKey)
	}

	gs.mu.Lock()

	if err := gs.checkClosed(); err != nil {
		gs.mu.Unlock()
		return nil, err
	}

	tid := effectiveTenantID(tenantID)
	if labelMap := gs.tenantNodesByLabel[tid]; labelMap != nil {
		for _, existingID := range labelMap[uniqueLabel] {
			existing, exists := gs.lookupNodeShard(existingID)
			if !exists {
				continue
			}
			if existingVal, has := existing.Properties[uniquePropertyKey]; has && valuesEqual(existingVal, newVal) {
				gs.mu.Unlock()
				return nil, &UniqueConstraintError{
					Label:             uniqueLabel,
					PropertyKey:       uniquePropertyKey,
					ConflictingNodeID: existingID,
					TenantID:          tid.String(),
				}
			}
		}
	}

	node, walPending, err := gs.createNodeLocked(tenantID, labels, properties)
	gs.mu.Unlock()
	// Wait for WAL durability after lock release (group commit, Track P item 1).
	gs.waitWALPending(wal.OpCreateNode, walPending)
	// R2.1: dispatch after lock release. See CreateNodeWithTenant.
	if err == nil && node != nil {
		gs.notifyNodeCreated(context.Background(), node)
	}
	return node, err
}

// createNodeLocked is the body of CreateNodeWithTenant minus the lock.
// Caller must hold gs.mu.Lock().
//
// Returns a *wal.Pending durability handle alongside the created node. For the
// batched WAL the node's WAL entry is enqueued (in-memory mutation order is
// preserved because the enqueue happens under gs.mu) but NOT yet durable; the
// caller must release gs.mu and then call the handle's Wait() before treating
// the create as durable (Track P item 1). For the synchronous WAL path the
// handle is nil and the write is already durable on return. The handle is nil
// on any error path.
func (gs *GraphStorage) createNodeLocked(tenantID string, labels []string, properties map[string]Value) (*Node, *wal.Pending, error) {
	start := time.Now()

	// Check if storage is closed
	if err := gs.checkClosed(); err != nil {
		gs.recordOperation("create_node", "error", start)
		return nil, nil, err
	}

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		gs.recordOperation("create_node", "error", start)
		return nil, nil, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++

	now := time.Now().Unix()
	node := &Node{
		ID: nodeID,
		// Node.TenantID is still string — A3 will migrate it. For now,
		// .String() preserves the existing wire format.
		TenantID:   effectiveTenantID(tenantID).String(),
		Labels:     labels,
		Properties: properties,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Per-shard write lock (A4) excludes shard.RLock readers during the
	// nodeShards mutation. Brief hold — released as soon as the new node
	// is in the shard map. The remaining global-index updates below run
	// under gs.mu.Lock only.
	gs.lockShard(nodeID)
	gs.storeNodeInShard(node)
	gs.unlockShard(nodeID)

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
		return nil, nil, err
	}

	// Update vector indexes for any vector properties
	if err := gs.UpdateNodeVectorIndexes(node); err != nil {
		gs.recordOperation("create_node", "error", start)
		return nil, nil, err
	}

	// Enqueue to WAL for durability. For the batched WAL this does NOT block on
	// the fsync — the caller waits on the returned handle AFTER releasing gs.mu
	// so concurrent writers can fill the same batch (Track P item 1). Enqueue
	// happens here, under gs.mu, so WAL order matches in-memory mutation order.
	walPending := gs.enqueueWAL(wal.OpCreateNode, node)

	gs.recordOperation("create_node", "success", start)
	return node.Clone(), walPending, nil
}

func valuesEqual(a, b Value) bool {
	if a.Type != b.Type {
		return false
	}
	if len(a.Data) != len(b.Data) {
		return false
	}
	for i := range a.Data {
		if a.Data[i] != b.Data[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// GetNode retrieves a node by ID.
//
// Tenant-blind. New callers should prefer GetNodeForTenant. Legacy
// callers retain this entry point until the audit-driven migration
// completes — see docs/AUDIT_fixes_plan_2026-05-06.md task A3.
func (gs *GraphStorage) GetNode(nodeID uint64) (*Node, error) {
	start := time.Now()
	defer gs.startQueryTiming()()

	// Closed-check uses atomic load (A4); no global lock required.
	if err := gs.checkClosed(); err != nil {
		gs.recordOperation("get_node", "error", start)
		return nil, err
	}

	// Per-shard read lock (A4). Concurrent readers on different shards
	// proceed in parallel; readers on the same shard contend only with
	// writers actively mutating that shard's nodeShards entry.
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)

	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		gs.recordOperation("get_node", "error", start)
		return nil, ErrNodeNotFound
	}

	gs.recordOperation("get_node", "success", start)
	return node.Clone(), nil
}

// GetNodeForTenant retrieves a node by ID, scoped to the given tenant.
//
// Returns ErrNodeNotFound if the node does not exist OR belongs to a
// different tenant. The same error in both cases is intentional — a
// distinct ErrCrossTenant would let an attacker enumerate "this ID
// exists in *some* tenant" via response-shape inference.
//
// Empty tenantID defaults to tenantid.Default ("default"). This matches
// CreateNode's default-tenant fallback so single-tenant deployments and
// tests that don't supply a tenant continue to work transparently.
//
// Closes Security CRIT #1 from docs/AUDIT_security_2026-05-06.md.
func (gs *GraphStorage) GetNodeForTenant(nodeID uint64, tenantID string) (*Node, error) {
	// Per-shard read lock (A4) — see GetNode for the rationale.
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)
	node, err := gs.getNodeRefForTenant(nodeID, tenantID)
	if err != nil {
		return nil, err
	}
	return node.Clone(), nil
}

// WithNodeRefForTenant invokes fn with the live node pointer for the
// given (id, tenantID), holding the per-shard read lock for the
// duration of fn. Returns ErrNodeNotFound if the node does not exist
// or belongs to a different tenant (same unified-error rationale as
// GetNodeForTenant).
//
// Caller contract: fn MUST NOT escape the *Node pointer past its own
// return — the shard lock is released as soon as fn finishes, and a
// concurrent writer may mutate the node's fields immediately after.
// To retain a snapshot beyond fn, call node.Clone() before returning.
//
// Designed for hot-path filter loops (vector search post-filter, etc.)
// where most candidates are rejected before any escape is needed; the
// per-iteration clone of GetNodeForTenant is wasted work for rejected
// candidates. Audit task A4 clone-elision (2026-05-10) — closes
// Performance HIGH-1 from docs/AUDIT_performance_2026-05-06.md.
func (gs *GraphStorage) WithNodeRefForTenant(nodeID uint64, tenantID string, fn func(*Node) error) error {
	gs.rlockShard(nodeID)
	defer gs.runlockShard(nodeID)
	node, err := gs.getNodeRefForTenant(nodeID, tenantID)
	if err != nil {
		return err
	}
	return fn(node)
}

// getNodeRefForTenant returns the live node pointer (NO clone) after
// validating tenant ownership. Caller MUST hold the per-shard read
// lock for nodeID (gs.rlockShard/runlockShard) for the duration that
// the returned pointer is used. Audit task A4 (2026-05-10) migrated
// the locking contract from gs.mu.RLock to per-shard.
//
// Internal use only — package-private. Hot-path callers (post-filter
// loops in vector search etc.) avoid the per-iteration clone cost via
// this helper. Public callers must use GetNodeForTenant which clones
// for safety.
//
// Returns ErrNodeNotFound on missing or cross-tenant. See
// GetNodeForTenant for the rationale on the unified error response.
func (gs *GraphStorage) getNodeRefForTenant(nodeID uint64, tenantID string) (*Node, error) {
	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		return nil, ErrNodeNotFound
	}
	expectedTenant := effectiveTenantID(tenantID).String()
	if node.TenantID != expectedTenant {
		// Cross-tenant access: same error as missing to avoid an
		// existence-leak side channel.
		return nil, ErrNodeNotFound
	}
	return node, nil
}

// UpdateNodeForTenant updates a node's properties, scoped to the given
// tenant. Returns ErrNodeNotFound on missing or cross-tenant (same
// rationale as GetNodeForTenant).
func (gs *GraphStorage) UpdateNodeForTenant(nodeID uint64, properties map[string]Value, tenantID string) error {
	// Validate tenant ownership *before* delegating to UpdateNode. We
	// hold the per-shard read lock just long enough for the check (A4),
	// then drop it so UpdateNode can acquire the write lock without
	// deadlocking.
	gs.rlockShard(nodeID)
	if _, err := gs.getNodeRefForTenant(nodeID, tenantID); err != nil {
		gs.runlockShard(nodeID)
		return err
	}
	gs.runlockShard(nodeID)
	return gs.UpdateNode(nodeID, properties)
}

// UpdateNode updates a node's properties.
//
// Tenant-blind. New callers should prefer UpdateNodeForTenant.
//
// Lock discipline (R2.1, S11 spike §7.4): notifyNodeUpdated dispatches
// after gs.mu.Lock is released. The oldNode / newNode clones are taken
// inside the lock window (when the live shard pointer is safe) and are
// only allocated when observers are registered — observerless callers pay
// zero clone cost.
func (gs *GraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	gs.mu.Lock()

	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		gs.mu.Unlock()
		return ErrNodeNotFound
	}

	// R2.1: snapshot pre-update state for observer dispatch. Only allocate
	// when observers are registered.
	var oldNode *Node
	if len(gs.observers) > 0 {
		oldNode = node.Clone()
	}

	// Update property indexes (global structures — under gs.mu.Lock).
	if err := gs.updatePropertyIndexes(nodeID, node, properties); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Per-shard write lock (A4) excludes shard.RLock readers during
	// the in-place Node-struct mutation that follows.
	gs.lockShard(nodeID)
	for k, v := range properties {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()
	gs.unlockShard(nodeID)

	// Update vector indexes for any vector properties
	if err := gs.UpdateNodeVectorIndexes(node); err != nil {
		gs.mu.Unlock()
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

	// R2.1: snapshot post-update state before releasing the lock so the
	// observer sees a consistent view.
	var newNode *Node
	if oldNode != nil {
		newNode = node.Clone()
	}
	gs.mu.Unlock()

	if newNode != nil {
		gs.notifyNodeUpdated(context.Background(), newNode, oldNode)
	}
	return nil
}

// RemoveNodeProperties removes specified properties from a node.
// Unlike UpdateNode (which merges), this deletes keys from the
// properties map. Tenant-blind — new callers in tenant-scoped code
// paths should prefer RemoveNodePropertiesForTenant.
func (gs *GraphStorage) RemoveNodeProperties(nodeID uint64, keys []string) error {
	gs.mu.Lock()

	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		gs.mu.Unlock()
		return ErrNodeNotFound
	}

	// R2.1: snapshot pre-removal state for observer dispatch. Only
	// allocate when observers are registered.
	var oldNode *Node
	if len(gs.observers) > 0 {
		oldNode = node.Clone()
	}

	// Per-shard write lock (A4) covers the property-map mutations and
	// the live-map snapshot that follows. The propertyIndexes lookups
	// touch a global map under gs.mu, but the index Remove calls walk
	// node.Properties (read) before mutation — keep those inside the
	// shard.Lock window so a concurrent reader on this shard never sees
	// a torn state.
	gs.lockShard(nodeID)
	for _, key := range keys {
		// Remove from property indexes
		if idx, exists := gs.propertyIndexes[key]; exists {
			if oldValue, hasKey := node.Properties[key]; hasKey {
				if err := idx.Remove(nodeID, oldValue); err != nil {
					log.Printf("node_operations: property index Remove failed for key %q node %d: %v", key, nodeID, err)
				}
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
	gs.unlockShard(nodeID)
	gs.writeToWAL(wal.OpUpdateNode, struct {
		NodeID     uint64
		Properties map[string]Value
	}{
		NodeID:     nodeID,
		Properties: walProps,
	})

	// R2.1: snapshot post-removal state before releasing the lock.
	var newNode *Node
	if oldNode != nil {
		newNode = node.Clone()
	}
	gs.mu.Unlock()

	if newNode != nil {
		gs.notifyNodeUpdated(context.Background(), newNode, oldNode)
	}
	return nil
}

// RemoveNodePropertiesForTenant removes specified properties from a
// node, scoped to the given tenant. Returns ErrNodeNotFound on
// missing or cross-tenant. Audit A6c-query (2026-05-08).
//
// Mirrors UpdateNodeForTenant's lock-then-delegate pattern: tenant
// validation under read lock, brief lock-drop window before
// RemoveNodeProperties acquires the write lock. Race window is
// benign — tenant IDs are immutable after node creation and node IDs
// don't recycle, so the only race is "node deleted by another
// goroutine before ours" which RemoveNodeProperties handles via
// ErrNodeNotFound.
func (gs *GraphStorage) RemoveNodePropertiesForTenant(nodeID uint64, keys []string, tenantID string) error {
	gs.rlockShard(nodeID)
	if _, err := gs.getNodeRefForTenant(nodeID, tenantID); err != nil {
		gs.runlockShard(nodeID)
		return err
	}
	gs.runlockShard(nodeID)
	return gs.RemoveNodeProperties(nodeID, keys)
}

// DeleteNodeForTenant deletes a node and all its edges, scoped to the
// given tenant. Returns ErrNodeNotFound on missing or cross-tenant
// (same rationale as GetNodeForTenant).
func (gs *GraphStorage) DeleteNodeForTenant(nodeID uint64, tenantID string) error {
	// Tenant check under per-shard read lock (A4), then delegate to
	// DeleteNode which acquires the write lock. The brief lock-drop
	// window between the two is benign: tenant IDs are immutable after
	// node creation (no API to change them) and node IDs don't get
	// recycled, so the only race is "node deleted by another goroutine
	// before our delete" — which DeleteNode handles correctly by
	// returning ErrNodeNotFound.
	gs.rlockShard(nodeID)
	if _, err := gs.getNodeRefForTenant(nodeID, tenantID); err != nil {
		gs.runlockShard(nodeID)
		return err
	}
	gs.runlockShard(nodeID)
	return gs.DeleteNode(nodeID)
}

// DeleteNode deletes a node and all its edges.
//
// Tenant-blind. New callers should prefer DeleteNodeForTenant.
//
// Lock discipline (R2.1, S11 spike §7.4): defer-unlock was replaced with
// explicit unlock at every return path so notifyNodeDeleted can dispatch
// strictly after gs.mu.Lock is released. The deleted node's TenantID is
// captured under lock (from the lookup at line 514) and passed to the
// notify call after unlock — the node's data is not accessible by then.
func (gs *GraphStorage) DeleteNode(nodeID uint64) error {
	gs.mu.Lock()

	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		gs.mu.Unlock()
		return ErrNodeNotFound
	}

	// Capture for OnNodeDeleted dispatch after unlock. node.TenantID is
	// stable for the lifetime of the node (immutable after creation).
	tenantID := node.TenantID

	// Get edges to delete (disk-backed or in-memory)
	var outgoingEdgeIDs, incomingEdgeIDs []uint64
	if gs.useDiskBackedEdges {
		var err error
		outgoingEdgeIDs, err = gs.edgeStore.GetOutgoingEdges(nodeID)
		if err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to get outgoing edges for node %d: %w", nodeID, err)
		}
		incomingEdgeIDs, err = gs.edgeStore.GetIncomingEdges(nodeID)
		if err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to get incoming edges for node %d: %w", nodeID, err)
		}
	} else {
		outgoingEdgeIDs = gs.outgoingEdges[nodeID]
		incomingEdgeIDs = gs.incomingEdges[nodeID]
	}

	// Cascade delete all outgoing edges
	for _, edgeID := range outgoingEdgeIDs {
		if err := gs.cascadeDeleteOutgoingEdge(edgeID); err != nil {
			gs.mu.Unlock()
			return fmt.Errorf("failed to cascade delete outgoing edge %d: %w", edgeID, err)
		}
	}

	// Cascade delete all incoming edges
	for _, edgeID := range incomingEdgeIDs {
		if err := gs.cascadeDeleteIncomingEdge(edgeID); err != nil {
			gs.mu.Unlock()
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
		gs.mu.Unlock()
		return err
	}

	// Remove from vector indexes (R1.2: routes by node.TenantID; empty
	// TenantID on legacy tenant-blind nodes falls back to tenantid.Default
	// inside RemoveNodeFromVectorIndexes).
	if err := gs.RemoveNodeFromVectorIndexes(nodeID, node.TenantID); err != nil {
		gs.mu.Unlock()
		return err
	}

	// Delete node — per-shard write lock (A4) excludes shard.RLock
	// readers during the nodeShards delete. The cascade work above
	// (label/property/vector index removal, edge cascades) all touches
	// global structures under the gs.mu.Lock that's held throughout
	// this function.
	gs.lockShard(nodeID)
	gs.deleteNodeShardEntry(nodeID)
	gs.unlockShard(nodeID)

	// Delete adjacency lists (disk-backed or in-memory)
	if err := gs.clearNodeAdjacency(nodeID); err != nil {
		gs.mu.Unlock()
		return fmt.Errorf("failed to clear adjacency for node %d: %w", nodeID, err)
	}

	// Atomic decrement with underflow protection
	atomicDecrementWithUnderflowProtection(&gs.stats.NodeCount)

	// Write to WAL for durability
	gs.writeToWAL(wal.OpDeleteNode, node)

	gs.mu.Unlock()

	// R2.1: dispatch after lock release. See lock-discipline comment in
	// pkg/storage/observation.go.
	gs.notifyNodeDeleted(context.Background(), nodeID, tenantID)
	return nil
}

// GetAllNodeIDs returns all node IDs in the storage.
// This is the preferred way to iterate over all nodes, as it handles
// deleted nodes correctly (unlike iterating from 1 to NodeCount).
func (gs *GraphStorage) GetAllNodeIDs() []uint64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodeIDs := make([]uint64, 0, gs.nodeCount())
	gs.forEachNodeIDUnlocked(func(id uint64) bool {
		nodeIDs = append(nodeIDs, id)
		return true
	})
	return nodeIDs
}

// GetAllNodesAcrossTenants returns every node from every tenant.
//
// Use ONLY for legitimate cross-tenant operations: replication, full
// backup, admin reports. The name is deliberately verbose so that
// reaching for it is a deliberate decision; calling it from any HTTP
// handler is almost certainly the wrong choice — use
// GetAllNodesForTenant(getTenantFromContext(r)) instead.
//
// Replaced the previous GetAllNodes (removed 2026-05-08, audit A3b)
// which had identical semantics under a name that made it easy to call
// accidentally from request-scoped code paths. That accidental misuse
// in pkg/api/handlers_nodes.go was Security CRIT #2 in the
// 2026-05-06 audit.
func (gs *GraphStorage) GetAllNodesAcrossTenants() []*Node {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	nodes := make([]*Node, 0, gs.nodeCount())
	gs.forEachNodeUnlocked(func(node *Node) bool {
		nodes = append(nodes, node.Clone())
		return true
	})
	return nodes
}

// ForEachNode iterates over all nodes, calling the provided function for each.
// The function receives a cloned node to prevent modification.
// Iteration stops early if the function returns false.
func (gs *GraphStorage) ForEachNode(fn func(*Node) bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	gs.forEachNodeUnlocked(func(node *Node) bool {
		return fn(node.Clone())
	})
}
