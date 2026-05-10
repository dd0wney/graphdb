package storage

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// removeEdgeFromList removes a specific edge ID from a list of edge IDs in-place.
// Uses swap-with-last for O(1) removal instead of O(n) allocation.
// Note: This does NOT preserve order; if order matters, use removeEdgeFromListOrdered.
func removeEdgeFromList(edges []uint64, edgeID uint64) []uint64 {
	for i, id := range edges {
		if id == edgeID {
			// Swap with last element and shrink slice
			edges[i] = edges[len(edges)-1]
			return edges[:len(edges)-1]
		}
	}
	return edges
}

// atomicDecrementWithUnderflowProtection safely decrements a uint64 counter
// with protection against underflow (prevents decrementing below zero)
func atomicDecrementWithUnderflowProtection(counter *uint64) {
	for {
		current := atomic.LoadUint64(counter)
		if current == 0 {
			break
		}
		if atomic.CompareAndSwapUint64(counter, current, current-1) {
			break
		}
	}
}

// appendToWAL appends an operation to the appropriate WAL based on configuration.
// It selects between batched WAL, compressed WAL, or standard WAL.
// Returns nil if no WAL is configured.
func (gs *GraphStorage) appendToWAL(opType wal.OpType, data []byte) error {
	if gs.useBatching && gs.batchedWAL != nil {
		_, err := gs.batchedWAL.Append(opType, data)
		return err
	}
	if gs.useCompression && gs.compressedWAL != nil {
		_, err := gs.compressedWAL.Append(opType, data)
		return err
	}
	if gs.wal != nil {
		_, err := gs.wal.Append(opType, data)
		return err
	}
	return nil // No WAL configured
}

// hasWAL returns true if any WAL is configured
func (gs *GraphStorage) hasWAL() bool {
	return gs.batchedWAL != nil || gs.compressedWAL != nil || gs.wal != nil
}

// checkClosed returns an error if the storage is closed. Uses an atomic
// load so callers on the per-shard hot path don't need to take gs.mu just
// to read a single bool (audit task A4).
func (gs *GraphStorage) checkClosed() error {
	if gs.closed.Load() {
		return fmt.Errorf("storage is closed")
	}
	return nil
}

// verifyNodeExists checks if a node exists and returns an error if
// not. Tenant-blind — used by replication and other intentionally
// tenant-blind callers (CreateEdge, UpsertEdge). New tenant-aware
// callers should prefer verifyNodeExistsForTenant.
func (gs *GraphStorage) verifyNodeExists(nodeID uint64, nodeType string) error {
	if _, exists := gs.lookupNodeShard(nodeID); !exists {
		return fmt.Errorf("%s node %d not found", nodeType, nodeID)
	}
	return nil
}

// verifyNodeExistsForTenant checks if a node exists AND belongs to
// the given tenant. Returns ErrNodeNotFound on either condition,
// matching the unified missing-vs-cross-tenant contract used by
// GetNodeForTenant — distinguishing them would leak existence to a
// cross-tenant probe.
//
// Audit A6a follow-up (2026-05-08): closes the residual gap from A6a
// where CreateEdgeWithTenant accepted from/to node IDs owned by
// another tenant; the resulting edge was stamped with the caller's
// tenant, enabling tenant-A to write a tenant-A-stamped edge against
// tenant-B's nodes.
func (gs *GraphStorage) verifyNodeExistsForTenant(nodeID uint64, nodeType string, tenantID string) error {
	node, exists := gs.lookupNodeShard(nodeID)
	if !exists {
		return fmt.Errorf("%s node %d not found: %w", nodeType, nodeID, ErrNodeNotFound)
	}
	expected := effectiveTenantID(tenantID).String()
	if node.TenantID != expected {
		// Cross-tenant: same error as missing to avoid existence leak.
		return fmt.Errorf("%s node %d not found: %w", nodeType, nodeID, ErrNodeNotFound)
	}
	return nil
}

// compressAllEdgeLists compresses all outgoing and incoming edge lists
// Assumes the caller holds gs.mu.Lock()
func (gs *GraphStorage) compressAllEdgeLists() {
	// Compress outgoing edges
	for nodeID, edgeIDs := range gs.outgoingEdges {
		if len(edgeIDs) > 0 {
			compressed, err := NewCompressedEdgeList(edgeIDs)
			if err != nil {
				// Log and skip - this should never happen with valid data
				// but we don't want to crash the database
				continue
			}
			gs.compressedOutgoing[nodeID] = compressed
		}
	}
	// Compress incoming edges
	for nodeID, edgeIDs := range gs.incomingEdges {
		if len(edgeIDs) > 0 {
			compressed, err := NewCompressedEdgeList(edgeIDs)
			if err != nil {
				// Log and skip - this should never happen with valid data
				continue
			}
			gs.compressedIncoming[nodeID] = compressed
		}
	}
}

// getEdgeIDsForNode retrieves edge IDs for a node from disk/compressed/uncompressed storage
// Returns nil if no edges found
// Assumes the caller holds appropriate locks
func (gs *GraphStorage) getEdgeIDsForNode(nodeID uint64, outgoing bool) []uint64 {
	// Check disk-backed storage first if enabled
	if gs.useDiskBackedEdges {
		var diskEdges []uint64
		var err error
		if outgoing {
			diskEdges, err = gs.edgeStore.GetOutgoingEdges(nodeID)
		} else {
			diskEdges, err = gs.edgeStore.GetIncomingEdges(nodeID)
		}
		if err == nil {
			return diskEdges
		}
	} else {
		// Check compressed storage first if compression is enabled
		if gs.useEdgeCompression {
			var compressed *CompressedEdgeList
			var exists bool
			if outgoing {
				compressed, exists = gs.compressedOutgoing[nodeID]
			} else {
				compressed, exists = gs.compressedIncoming[nodeID]
			}
			if exists {
				return compressed.Decompress()
			}
		}

		// Fall back to uncompressed storage
		var uncompressed []uint64
		var exists bool
		if outgoing {
			uncompressed, exists = gs.outgoingEdges[nodeID]
		} else {
			uncompressed, exists = gs.incomingEdges[nodeID]
		}
		if exists {
			return uncompressed
		}
	}

	return nil
}

// Helper functions for shard-based locking

// getShardIndex returns the shard index for a given ID
func (gs *GraphStorage) getShardIndex(id uint64) int {
	return int(id & gs.shardMask)
}

// rlockShard acquires a read lock on the shard for the given ID
func (gs *GraphStorage) rlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RLock()
}

// runlockShard releases a read lock on the shard for the given ID
func (gs *GraphStorage) runlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RUnlock()
}

// lockShard acquires the write lock on the shard for the given ID.
// Used by writers (CreateNode, UpdateNode, DeleteNode etc.) during the
// brief window when they mutate nodeShards[shardOf(id)] or fields of
// the Node struct stored there.
func (gs *GraphStorage) lockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Lock()
}

// unlockShard releases the write lock on the shard for the given ID.
func (gs *GraphStorage) unlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Unlock()
}

// Partitioned-node-map helpers (audit task A4, 2026-05-10).
//
// nodeShards is a [256]map[uint64]*Node array; helpers below hide the
// shard-index arithmetic from call sites. Locking rules: callers must
// hold either gs.mu (read or write, depending on op) or — once A4-T4
// flips the lock-grain — the appropriate shardLocks entry.

// lookupNodeShard returns the node for the given ID. Caller must hold
// the appropriate read lock.
func (gs *GraphStorage) lookupNodeShard(id uint64) (*Node, bool) {
	n, ok := gs.nodeShards[gs.getShardIndex(id)][id]
	return n, ok
}

// storeNodeInShard writes node into its owning shard. Caller must hold
// the appropriate write lock.
func (gs *GraphStorage) storeNodeInShard(node *Node) {
	gs.nodeShards[gs.getShardIndex(node.ID)][node.ID] = node
}

// deleteNodeShardEntry removes the entry for id from its owning shard.
// Caller must hold the appropriate write lock.
func (gs *GraphStorage) deleteNodeShardEntry(id uint64) {
	delete(gs.nodeShards[gs.getShardIndex(id)], id)
}

// nodeCount returns the total number of nodes across all shards. Caller
// must hold gs.mu.RLock or all shard read locks for a consistent count.
func (gs *GraphStorage) nodeCount() int {
	total := 0
	for i := range gs.nodeShards {
		total += len(gs.nodeShards[i])
	}
	return total
}

// forEachNodeUnlocked invokes fn for every node across all shards.
// Iteration stops early if fn returns false. Caller must hold gs.mu.RLock
// (or all shard read locks) for the duration; concurrent map writes
// during iteration will trip the Go runtime's map-race check.
func (gs *GraphStorage) forEachNodeUnlocked(fn func(*Node) bool) {
	for i := range gs.nodeShards {
		for _, node := range gs.nodeShards[i] {
			if !fn(node) {
				return
			}
		}
	}
}

// forEachNodeIDUnlocked invokes fn for every node ID across all shards.
// Iteration stops early if fn returns false. Same locking contract as
// forEachNodeUnlocked.
func (gs *GraphStorage) forEachNodeIDUnlocked(fn func(uint64) bool) {
	for i := range gs.nodeShards {
		for id := range gs.nodeShards[i] {
			if !fn(id) {
				return
			}
		}
	}
}

// flattenNodesForSnapshot collects every node from every shard into a
// single map for serialization. Used by the snapshot writer to preserve
// the on-disk format (a flat map[uint64]*Node) across the partition
// migration. Caller must hold gs.mu.RLock for the duration.
func (gs *GraphStorage) flattenNodesForSnapshot() map[uint64]*Node {
	out := make(map[uint64]*Node, gs.nodeCount())
	for i := range gs.nodeShards {
		for id, node := range gs.nodeShards[i] {
			out[id] = node
		}
	}
	return out
}

// rebucketSnapshotNodes redistributes a flat snapshot map across the
// partitioned shards. Used by the snapshot loader. Caller must hold
// gs.mu.Lock for the duration. Replaces (does not merge with) any
// existing shard contents.
func (gs *GraphStorage) rebucketSnapshotNodes(flat map[uint64]*Node) {
	for i := range gs.nodeShards {
		gs.nodeShards[i] = make(map[uint64]*Node)
	}
	for id, node := range flat {
		gs.nodeShards[gs.getShardIndex(id)][id] = node
	}
}

// Partitioned-edge-map helpers (audit task A4-edges, 2026-05-10).
//
// Mirror of the node-side helpers above. edgeShards is keyed by
// edgeID & shardMask; the same shardLocks array protects both, so a
// node operation on shard 5 and an edge operation on shard 5 share
// the lock (acceptable cross-contention; splitting per-entity-type
// locks is a future optimization if perf shows it matters).

// lookupEdgeShard returns the edge for the given ID. Caller must hold
// the appropriate read lock.
func (gs *GraphStorage) lookupEdgeShard(id uint64) (*Edge, bool) {
	e, ok := gs.edgeShards[gs.getShardIndex(id)][id]
	return e, ok
}

// storeEdgeInShard writes edge into its owning shard. Caller must hold
// the appropriate write lock.
func (gs *GraphStorage) storeEdgeInShard(edge *Edge) {
	gs.edgeShards[gs.getShardIndex(edge.ID)][edge.ID] = edge
}

// deleteEdgeShardEntry removes the entry for id from its owning shard.
// Caller must hold the appropriate write lock.
func (gs *GraphStorage) deleteEdgeShardEntry(id uint64) {
	delete(gs.edgeShards[gs.getShardIndex(id)], id)
}

// edgeCount returns the total number of edges across all shards. Caller
// must hold gs.mu.RLock or all shard read locks for a consistent count.
func (gs *GraphStorage) edgeCount() int {
	total := 0
	for i := range gs.edgeShards {
		total += len(gs.edgeShards[i])
	}
	return total
}

// forEachEdgeUnlocked invokes fn for every edge across all shards.
// Iteration stops early if fn returns false. Caller must hold
// gs.mu.RLock (or all shard read locks) for the duration; concurrent
// map writes during iteration will trip the Go runtime's map-race check.
func (gs *GraphStorage) forEachEdgeUnlocked(fn func(*Edge) bool) {
	for i := range gs.edgeShards {
		for _, edge := range gs.edgeShards[i] {
			if !fn(edge) {
				return
			}
		}
	}
}

// flattenEdgesForSnapshot collects every edge from every shard into a
// single map for serialization. Used by the snapshot writer to preserve
// the on-disk format (a flat map[uint64]*Edge) across the partition
// migration. Caller must hold gs.mu.RLock for the duration.
func (gs *GraphStorage) flattenEdgesForSnapshot() map[uint64]*Edge {
	out := make(map[uint64]*Edge, gs.edgeCount())
	for i := range gs.edgeShards {
		for id, edge := range gs.edgeShards[i] {
			out[id] = edge
		}
	}
	return out
}

// rebucketSnapshotEdges redistributes a flat snapshot map across the
// partitioned shards. Used by the snapshot loader. Caller must hold
// gs.mu.Lock for the duration. Replaces (does not merge with) any
// existing shard contents.
func (gs *GraphStorage) rebucketSnapshotEdges(flat map[uint64]*Edge) {
	for i := range gs.edgeShards {
		gs.edgeShards[i] = make(map[uint64]*Edge)
	}
	for id, edge := range flat {
		gs.edgeShards[gs.getShardIndex(id)][id] = edge
	}
}

// allocateNodeID allocates a new node ID in a thread-safe manner using atomic operations.
// This is a lock-free operation that provides much better throughput than mutex-based allocation.
// Returns error if ID space is exhausted.
func (gs *GraphStorage) allocateNodeID() (uint64, error) {
	// Atomically increment and get the new ID
	// Note: AddUint64 returns the NEW value, so we subtract 1 to get the allocated ID
	nodeID := atomic.AddUint64(&gs.nextNodeID, 1) - 1

	// Detect ID space exhaustion. AddUint64 will wrap at MaxUint64, so
	// a returned nodeID near the cap means we've consumed the space.
	if nodeID >= ^uint64(0)-1 {
		return 0, fmt.Errorf("node ID space exhausted")
	}

	return nodeID, nil
}

// allocateEdgeID allocates a new edge ID in a thread-safe manner using atomic operations.
// This is a lock-free operation that provides much better throughput than mutex-based allocation.
// Returns error if ID space is exhausted.
func (gs *GraphStorage) allocateEdgeID() (uint64, error) {
	// Atomically increment and get the new ID
	edgeID := atomic.AddUint64(&gs.nextEdgeID, 1) - 1

	// Check for ID space exhaustion
	if edgeID >= ^uint64(0)-1 { // Near MaxUint64
		return 0, fmt.Errorf("edge ID space exhausted")
	}

	return edgeID, nil
}

// recordOperation records storage operation metrics
func (gs *GraphStorage) recordOperation(operation string, status string, start time.Time) {
	if gs.metricsRegistry != nil {
		duration := time.Since(start)
		gs.metricsRegistry.RecordStorageOperation(operation, status, duration)
	}
}
