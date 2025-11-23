package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

var (
	ErrNodeNotFound = fmt.Errorf("node not found")
	ErrEdgeNotFound = fmt.Errorf("edge not found")
)

const (
	// File and directory permissions
	dirPermissions  = 0755 // rwxr-xr-x: Owner can read/write/execute, others can read/execute
	filePermissions = 0644 // rw-r--r--: Owner can read/write, others can read
)

// GraphStorage is the core in-memory graph storage engine
type GraphStorage struct {
	// Core data structures
	nodes map[uint64]*Node
	edges map[uint64]*Edge

	// Indexes for fast lookups
	nodesByLabel    map[string][]uint64       // label -> node IDs
	edgesByType     map[string][]uint64       // edge type -> edge IDs
	outgoingEdges   map[uint64][]uint64       // node ID -> outgoing edge IDs (uncompressed)
	incomingEdges   map[uint64][]uint64       // node ID -> incoming edge IDs (uncompressed)
	propertyIndexes map[string]*PropertyIndex // property key -> index
	vectorIndex     *VectorIndex              // vector search indexes

	// Compressed edge storage (optional)
	compressedOutgoing map[uint64]*CompressedEdgeList // node ID -> compressed outgoing edges
	compressedIncoming map[uint64]*CompressedEdgeList // node ID -> compressed incoming edges
	useEdgeCompression bool

	// Disk-backed edge storage (Milestone 2)
	edgeStore          *EdgeStore // LSM-backed edge storage with LRU cache
	useDiskBackedEdges bool       // If true, use EdgeStore instead of in-memory maps

	// ID generators
	nextNodeID uint64
	nextEdgeID uint64

	// Concurrency control
	mu sync.RWMutex // Global lock for operations spanning multiple shards
	shardLocks [256]*sync.RWMutex // Shard-specific locks for fine-grained concurrency
	shardMask uint64 // Mask for efficient shard calculation (255 for 256 shards)

	// Persistence
	dataDir        string
	wal            *wal.WAL
	batchedWAL     *wal.BatchedWAL
	compressedWAL  *wal.CompressedWAL
	useBatching    bool
	useCompression bool

	// Statistics (using atomic operations for thread-safety)
	stats Statistics
	// Internal field for atomic float64 operations on AvgQueryTime
	avgQueryTimeBits uint64 // Stores AvgQueryTime as bits for atomic access

	// Transaction management
	activeTransaction *Transaction
	txIDCounter       uint64

	// Metrics
	metricsRegistry *metrics.Registry

	// Encryption
	encryptionEngine interface{} // *encryption.Engine (interface to avoid import cycle)
	keyManager       interface{} // *encryption.KeyManager
}

// StorageConfig holds configuration for GraphStorage
type StorageConfig struct {
	DataDir               string
	EnableBatching        bool
	EnableCompression     bool
	EnableEdgeCompression bool
	BatchSize             int
	FlushInterval         time.Duration
	UseDiskBackedEdges    bool // Enable disk-backed adjacency lists (Milestone 2)
	EdgeCacheSize         int  // LRU cache size for hot edge lists (default: 10000)
	BulkImportMode        bool // Disable WAL and use fast path for bulk loading
}

// Statistics tracks database statistics
type Statistics struct {
	NodeCount    uint64
	EdgeCount    uint64
	LastSnapshot time.Time
	TotalQueries uint64
	AvgQueryTime float64
}

// removeEdgeFromList removes a specific edge ID from a list of edge IDs
// This is a helper to eliminate duplicate filtering logic across the codebase
func removeEdgeFromList(edges []uint64, edgeID uint64) []uint64 {
	result := make([]uint64, 0, len(edges))
	for _, id := range edges {
		if id != edgeID {
			result = append(result, id)
		}
	}
	return result
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


// updatePropertyIndexes updates property indexes when node properties change
// For each property being updated, removes the old value and inserts the new value


// clearNodeAdjacency removes all adjacency data for a node (disk or memory)

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
			gs.compressedOutgoing[nodeID] = NewCompressedEdgeList(edgeIDs)
		}
	}
	// Compress incoming edges
	for nodeID, edgeIDs := range gs.incomingEdges {
		if len(edgeIDs) > 0 {
			gs.compressedIncoming[nodeID] = NewCompressedEdgeList(edgeIDs)
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

// NewGraphStorage creates a new graph storage engine with default config
func NewGraphStorage(dataDir string) (*GraphStorage, error) {
	return NewGraphStorageWithConfig(StorageConfig{
		DataDir:               dataDir,
		EnableBatching:        false,
		EnableCompression:     false,
		EnableEdgeCompression: true, // Enabled by default for 5.08x memory savings
		BatchSize:             100,
		FlushInterval:         10 * time.Millisecond,
	})
}

// NewGraphStorageWithConfig creates a new graph storage engine with custom config
func NewGraphStorageWithConfig(config StorageConfig) (*GraphStorage, error) {
	gs := &GraphStorage{
		nodes:              make(map[uint64]*Node),
		edges:              make(map[uint64]*Edge),
		nodesByLabel:       make(map[string][]uint64),
		edgesByType:        make(map[string][]uint64),
		outgoingEdges:      make(map[uint64][]uint64),
		incomingEdges:      make(map[uint64][]uint64),
		propertyIndexes:    make(map[string]*PropertyIndex),
		vectorIndex:        NewVectorIndex(),
		compressedOutgoing: make(map[uint64]*CompressedEdgeList),
		compressedIncoming: make(map[uint64]*CompressedEdgeList),
		useEdgeCompression: config.EnableEdgeCompression,
		shardMask:          255, // 256 shards - 1 for bitwise AND
		dataDir:            config.DataDir,
		useBatching:        config.EnableBatching,
		useCompression:     config.EnableCompression,
		nextNodeID:         1,
		nextEdgeID:         1,
		metricsRegistry:    metrics.DefaultRegistry(),
	}

	// Initialize shard locks for fine-grained concurrency
	for i := range gs.shardLocks {
		gs.shardLocks[i] = &sync.RWMutex{}
	}

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(config.DataDir, dirPermissions); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize WAL (compressed, batched, or regular) - skip in bulk import mode
	if !config.BulkImportMode {
		if config.EnableCompression {
			compressedWAL, err := wal.NewCompressedWAL(filepath.Join(config.DataDir, "wal"))
			if err != nil {
				return nil, fmt.Errorf("failed to initialize compressed WAL: %w", err)
			}
			gs.compressedWAL = compressedWAL
		} else if config.EnableBatching {
			batchedWAL, err := wal.NewBatchedWAL(
				filepath.Join(config.DataDir, "wal"),
				config.BatchSize,
				config.FlushInterval,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to initialize batched WAL: %w", err)
			}
			gs.batchedWAL = batchedWAL
		} else {
			walInstance, err := wal.NewWAL(filepath.Join(config.DataDir, "wal"))
			if err != nil {
				return nil, fmt.Errorf("failed to initialize WAL: %w", err)
			}
			gs.wal = walInstance
		}
	}

	// Initialize disk-backed edge storage if enabled (Milestone 2)
	if config.UseDiskBackedEdges {
		cacheSize := config.EdgeCacheSize
		if cacheSize == 0 {
			cacheSize = 10000 // Default cache size
		}

		edgeStoreDir := filepath.Join(config.DataDir, "edgestore")
		edgeStore, err := NewEdgeStore(edgeStoreDir, cacheSize)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize EdgeStore: %w", err)
		}
		gs.edgeStore = edgeStore
		gs.useDiskBackedEdges = true
	}

	// Try to load from disk
	if err := gs.loadFromDisk(); err != nil {
		// If no snapshot exists, that's OK (fresh database)
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load from disk: %w", err)
		}
	}

	// Replay WAL entries since last snapshot
	if err := gs.replayWAL(); err != nil {
		return nil, fmt.Errorf("failed to replay WAL: %w", err)
	}

	return gs, nil
}

// Helper functions for shard-based locking

// getShardIndex returns the shard index for a given ID
func (gs *GraphStorage) getShardIndex(id uint64) int {
	return int(id & gs.shardMask)
}

// lockShard acquires a write lock on the shard for the given ID
func (gs *GraphStorage) lockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Lock()
}

// unlockShard releases a write lock on the shard for the given ID
func (gs *GraphStorage) unlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].Unlock()
}

// rlockShard acquires a read lock on the shard for the given ID
func (gs *GraphStorage) rlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RLock()
}

// runlockShard releases a read lock on the shard for the given ID
func (gs *GraphStorage) runlockShard(id uint64) {
	gs.shardLocks[gs.getShardIndex(id)].RUnlock()
}

// GetStatistics returns database statistics (thread-safe using atomic operations)

// allocateNodeID allocates a new node ID in a thread-safe manner
// Returns error if ID space is exhausted
func (gs *GraphStorage) allocateNodeID() (uint64, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextNodeID == ^uint64(0) { // MaxUint64
		return 0, fmt.Errorf("node ID space exhausted")
	}

	nodeID := gs.nextNodeID
	gs.nextNodeID++
	return nodeID, nil
}

// allocateEdgeID allocates a new edge ID in a thread-safe manner
// Returns error if ID space is exhausted
func (gs *GraphStorage) allocateEdgeID() (uint64, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Check for ID space exhaustion
	if gs.nextEdgeID == ^uint64(0) { // MaxUint64
		return 0, fmt.Errorf("edge ID space exhausted")
	}

	edgeID := gs.nextEdgeID
	gs.nextEdgeID++
	return edgeID, nil
}


// NOTE: Parallel traversal methods (BFS, DFS, shortest path) are available
// via the parallel package to avoid circular dependencies:
//
//   import "github.com/dd0wney/cluso-graphdb/pkg/parallel"
//
//   traverser := parallel.NewParallelTraverser(graph, numWorkers)
//   defer traverser.Close()
//   results := traverser.TraverseBFS(startNodes, maxDepth)
//
// See pkg/parallel/traverse.go for full API documentation.

// recordOperation records storage operation metrics
func (gs *GraphStorage) recordOperation(operation string, status string, start time.Time) {
	if gs.metricsRegistry != nil {
		duration := time.Since(start)
		gs.metricsRegistry.RecordStorageOperation(operation, status, duration)
	}
}
