package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// ErrNodeNotFound and ErrEdgeNotFound are defined in errors.go

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
