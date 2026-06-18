package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dd0wney/graphdb/pkg/metrics"
	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/wal"
)

// ErrNodeNotFound and ErrEdgeNotFound are defined in errors.go

// DefaultStorageConfig returns the config NewGraphStorage uses. Callers
// that need to override a single field (e.g. wire EncryptionEngine at
// construction time) start from this instead of duplicating defaults.
func DefaultStorageConfig(dataDir string) StorageConfig {
	return StorageConfig{
		DataDir: dataDir,
		// Per-write fsync is the default: it is the strongest-durability path
		// AND the fastest on local/NVMe storage, where an fsync (~11µs) is far
		// cheaper than BatchedWAL's flush-interval wait. The FlushInterval
		// benchmark (bench_wal_flush_interval_test.go) measured per-write fsync
		// at ~10.8µs/op vs batched-1ms at ~135µs/op — 13× faster — because at low
		// writer counts each batched write is bounded by the flush timer.
		// BatchedWAL (EnableBatching: true) amortizes fsync across a batch and
		// wins only when fsync is EXPENSIVE — slow or networked disks under high
		// write concurrency. Enable it per-deployment there; durability is
		// preserved either way (a write returns only after its (batch-)fsync).
		EnableBatching:        false,
		EnableCompression:     false,
		EnableEdgeCompression: true, // Enabled by default for 5.08x memory savings
		BatchSize:             100,
		FlushInterval:         10 * time.Millisecond,
	}
}

// NewGraphStorage creates a new graph storage engine with default config
func NewGraphStorage(dataDir string) (*GraphStorage, error) {
	return NewGraphStorageWithConfig(DefaultStorageConfig(dataDir))
}

// NewGraphStorageWithConfig creates a new graph storage engine with custom config
func NewGraphStorageWithConfig(config StorageConfig) (*GraphStorage, error) {
	gs := &GraphStorage{
		nodesByLabel:    make(labelIndex),
		edgesByType:     make(labelIndex),
		outgoingEdges:   make(map[uint64][]uint64),
		incomingEdges:   make(map[uint64][]uint64),
		propertyIndexes: make(map[string]*PropertyIndex),
		vectorIndex:     NewVectorIndex(),
		// Tenant-scoped indexes for multi-tenancy.
		// Keyed by tenantid.TenantID since audit task A1 (2026-05-06).
		tenantNodesByLabel: make(map[tenantid.TenantID]labelIndex),
		tenantEdgesByType:  make(map[tenantid.TenantID]labelIndex),
		tenantStats:        make(map[tenantid.TenantID]*TenantStats),
		tenantNodeIDs:      make(map[tenantid.TenantID]map[uint64]struct{}),
		tenantEdgeIDs:      make(map[tenantid.TenantID]map[uint64]struct{}),
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
		// Set before loadFromDisk below so an encrypted snapshot can
		// decrypt during construction (M-14).
		encryptionEngine: config.EncryptionEngine,
		keyManager:       config.KeyManager,
	}

	// Initialize shard locks for fine-grained concurrency
	for i := range gs.shardLocks {
		gs.shardLocks[i] = &sync.RWMutex{}
	}

	// Initialize the partitioned node and edge shards. Each shard is a
	// distinct map; *Shards[i & shardMask] holds the entity for ID i.
	for i := range gs.nodeShards {
		gs.nodeShards[i] = make(map[uint64]*Node)
	}
	for i := range gs.edgeShards {
		gs.edgeShards[i] = make(map[uint64]*Edge)
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

	// Try to load from disk. When the mmap reopen mode is eligible and a
	// snapshot.mmap exists, take the lazy mmap path; otherwise the JSON path.
	// (A store opened in mmap mode with only a legacy snapshot.json loads JSON
	// here and writes snapshot.mmap on its next Snapshot.)
	gs.useMmapSnapshot = mmapEligible(config)
	loadErr := error(nil)
	if gs.useMmapSnapshot && fileExists(mmapSnapshotPath(config.DataDir)) {
		loadErr = gs.loadFromDiskMmap()
	} else {
		loadErr = gs.loadFromDisk()
	}
	if loadErr != nil {
		// If no snapshot exists, that's OK (fresh database)
		if !os.IsNotExist(loadErr) {
			return nil, fmt.Errorf("failed to load from disk: %w", loadErr)
		}
	}

	// Replay WAL entries since last snapshot
	if err := gs.replayWAL(); err != nil {
		return nil, fmt.Errorf("failed to replay WAL: %w", err)
	}

	// Rebuild the HNSW vector index from the FINAL node set (snapshot + WAL
	// replay). The index definitions were recreated in loadFromDisk; the graph
	// itself is not serialized, so without this every vector search silently
	// returns nothing after a restart. Must run last so post-snapshot writes
	// recovered above are indexed too.
	gs.rebuildVectorIndexesFromNodes()

	// H-3 toggle hygiene: encryption is on but the replay saw pre-toggle
	// plaintext entries. Checkpoint once (the snapshot is encrypted; WAL
	// entries ≤ boundary are dropped) so the plaintext leaves the disk
	// instead of lingering next to new ciphertext.
	if gs.encryptionEngine != nil && gs.walReplaySawPlaintext {
		if err := gs.CompactWAL(); err != nil {
			return nil, fmt.Errorf("failed to purge plaintext WAL entries after enabling encryption: %w", err)
		}
	}

	return gs, nil
}

// NOTE: Parallel traversal methods (BFS, DFS, shortest path) are available
// via the parallel package to avoid circular dependencies:
//
//   import "github.com/dd0wney/graphdb/pkg/parallel"
//
//   traverser := parallel.NewParallelTraverser(graph, numWorkers)
//   defer traverser.Close()
//   results := traverser.TraverseBFS(startNodes, maxDepth)
//
// See pkg/parallel/traverse.go for full API documentation.
