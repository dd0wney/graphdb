package storage

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

const (
	// File and directory permissions
	dirPermissions  = 0755 // rwxr-xr-x: Owner can read/write/execute, others can read/execute
	filePermissions = 0644 // rw-r--r--: Owner can read/write, others can read
)

// GraphStorage is the core in-memory graph storage engine
type GraphStorage struct {
	// Core data structures
	//
	// nodeShards partitions all nodes across 256 disjoint maps, indexed
	// by `id & shardMask`. Each shard is guarded by shardLocks[i]; this
	// lets concurrent readers on different shards proceed without
	// contending on a single global lock and avoids the Go runtime
	// "concurrent map read and map write" panic that a single shared
	// map would risk under shard-grained locking. Audit task A4
	// (2026-05-10).
	nodeShards [256]map[uint64]*Node
	edges      map[uint64]*Edge

	// Indexes for fast lookups
	nodesByLabel    map[string][]uint64       // label -> node IDs
	edgesByType     map[string][]uint64       // edge type -> edge IDs
	outgoingEdges   map[uint64][]uint64       // node ID -> outgoing edge IDs (uncompressed)
	incomingEdges   map[uint64][]uint64       // node ID -> incoming edge IDs (uncompressed)
	propertyIndexes map[string]*PropertyIndex // property key -> index
	vectorIndex     *VectorIndex              // vector search indexes

	// Tenant-scoped indexes for multi-tenancy.
	// Keyed by tenantid.TenantID since audit task A1 (2026-05-06); public
	// methods that take "tenantID string" still convert at the boundary
	// until A3 migrates the public surface.
	tenantNodesByLabel map[tenantid.TenantID]map[string][]uint64 // tenant -> label -> node IDs
	tenantEdgesByType  map[tenantid.TenantID]map[string][]uint64 // tenant -> edge type -> edge IDs
	tenantStats        map[tenantid.TenantID]*TenantStats        // tenant -> usage statistics

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
	mu         sync.RWMutex       // Global lock for global indexes (label/property/vector/tenant)
	shardLocks [256]*sync.RWMutex // Per-shard locks; shardLocks[i] guards nodeShards[i]
	shardMask  uint64             // Mask for efficient shard calculation (255 for 256 shards)
	// closed is read on the GetNode hot path which now takes only the
	// per-shard read lock (audit task A4, 2026-05-10); using atomic.Bool
	// avoids the previous gs.mu.RLock/RUnlock dance just to read a bool.
	closed atomic.Bool

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
	txIDCounter uint64

	// Metrics
	metricsRegistry *metrics.Registry

	// Encryption (using typed interfaces for compile-time safety)
	encryptionEngine encryption.EncryptDecrypter // Handles data encryption/decryption
	keyManager       encryption.KeyProvider      // Manages encryption keys
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

// TenantStats tracks per-tenant usage statistics for multi-tenancy
type TenantStats struct {
	NodeCount    uint64 // Number of nodes belonging to this tenant
	EdgeCount    uint64 // Number of edges belonging to this tenant
	StorageBytes uint64 // Estimated storage used by this tenant
	LastUpdated  int64  // Unix timestamp of last update
}
