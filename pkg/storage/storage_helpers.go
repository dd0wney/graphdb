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

// checkClosed returns an error if the storage is closed
func (gs *GraphStorage) checkClosed() error {
	if gs.closed {
		return fmt.Errorf("storage is closed")
	}
	return nil
}

// verifyNodeExists checks if a node exists and returns an error if not
func (gs *GraphStorage) verifyNodeExists(nodeID uint64, nodeType string) error {
	if _, exists := gs.nodes[nodeID]; !exists {
		return fmt.Errorf("%s node %d not found", nodeType, nodeID)
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

// allocateNodeID allocates a new node ID in a thread-safe manner using atomic operations.
// This is a lock-free operation that provides much better throughput than mutex-based allocation.
// Returns error if ID space is exhausted.
func (gs *GraphStorage) allocateNodeID() (uint64, error) {
	// Atomically increment and get the new ID
	// Note: AddUint64 returns the NEW value, so we subtract 1 to get the allocated ID
	nodeID := atomic.AddUint64(&gs.nextNodeID, 1) - 1

	// Check for ID space exhaustion (overflow detection)
	// If we've wrapped around to 0, the previous value was MaxUint64
	if nodeID == 0 && atomic.LoadUint64(&gs.nextNodeID) > 1 {
		// This is actually checking if we started from a valid state
		// The real overflow check is if nodeID is close to max
	}
	if nodeID >= ^uint64(0)-1 { // Near MaxUint64
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
