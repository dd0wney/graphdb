package storage

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dd0wney/graphdb/pkg/encryption"
	"github.com/dd0wney/graphdb/pkg/metrics"
	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/wal"
)

const (
	// File and directory permissions. Owner-only (security audit H-2):
	// the snapshot file is customer-data-equivalent and the data
	// directory holds the WAL + snapshots, so neither should be readable
	// by other local users. (The audit named wal/search/lsm and assumed
	// pkg/storage was already 0600 — it was 0644/0755; tightened here too.)
	dirPermissions  = 0o700 // rwx------: owner only
	filePermissions = 0o600 // rw-------: owner only
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
	// edgeShards mirrors nodeShards's partition shape for edges.
	// Same rationale: a single shared map[uint64]*Edge can't be safely
	// read under shard.RLock while another goroutine writes to it under
	// any lock — Go's map runtime panics on concurrent map read+write
	// even across distinct keys (bucket splits / rehash mutate map
	// metadata). Audit task A4-edges (2026-05-10).
	edgeShards [256]map[uint64]*Edge

	// Indexes for fast lookups
	nodesByLabel    labelIndex                // label -> set of node IDs (O(1) remove; see node_indexing.go)
	edgesByType     labelIndex                // edge type -> set of edge IDs
	outgoingEdges   map[uint64][]uint64       // node ID -> outgoing edge IDs (uncompressed)
	incomingEdges   map[uint64][]uint64       // node ID -> incoming edge IDs (uncompressed)
	propertyIndexes map[string]*PropertyIndex // property key -> index
	vectorIndex     *VectorIndex              // vector search indexes

	// Tenant-scoped indexes for multi-tenancy.
	// Keyed by tenantid.TenantID since audit task A1 (2026-05-06); public
	// methods that take "tenantID string" still convert at the boundary
	// until A3 migrates the public surface.
	tenantNodesByLabel map[tenantid.TenantID]labelIndex   // tenant -> label -> set of node IDs
	tenantEdgesByType  map[tenantid.TenantID]labelIndex   // tenant -> edge type -> set of edge IDs
	tenantStats        map[tenantid.TenantID]*TenantStats // tenant -> usage statistics

	// tenantNodeIDs is the per-tenant node-ID enumeration index: the set
	// of every node ID owned by a tenant, label or not. It exists so
	// GetAllNodesForTenant enumerates only the caller's nodes (O(tenant))
	// instead of scanning all 256 shards across every tenant and filtering
	// (O(total-DB) — the H4 cross-tenant read amplification). A set, not a
	// slice, so create/delete maintenance stays O(1) on the write hot path.
	// Unlike tenantNodesByLabel it includes unlabeled nodes — which is the
	// gap that forced the full-scan fallback. Maintained by add/remove-
	// NodeToTenantIndex, so it is rebuilt on snapshot-load and WAL-replay
	// through the same hook (no on-disk format change). Track P item (2)
	// (AUDIT_performance_saas_load_2026-06-02 § H4).
	tenantNodeIDs map[tenantid.TenantID]map[uint64]struct{} // tenant -> set of node IDs

	// tenantEdgeIDs is the edge analogue of tenantNodeIDs: the set of every
	// edge ID owned by a tenant. It backs GetAllEdgesForTenant (and so the
	// GraphQL edge connection resolvers) so a tenant's edge enumeration is
	// O(tenant) instead of a full cross-tenant shard scan (H4). Same set-vs-
	// slice rationale and same rebuild-via-addEdgeToTenantIndex property as
	// tenantNodeIDs — and that rebuild only became correct once the edge
	// tenant index was wired into snapshot-load + WAL-replay (#259).
	tenantEdgeIDs map[tenantid.TenantID]map[uint64]struct{} // tenant -> set of edge IDs

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
	// compactMu serializes CompactWAL callers (one snapshot+truncate
	// checkpoint at a time). Independent of gs.mu — the checkpoint takes
	// gs.mu.RLock internally via snapshotWithBoundary.
	compactMu sync.Mutex
	// txWALBarrier closes the Transaction.Commit window where buffered
	// changes are applied in-memory under gs.mu.Lock but the WAL batch is
	// appended AFTER the unlock: Commit holds the read side from before
	// its unlock until the append lands; snapshotWithBoundary takes the
	// write side (under gs.mu.RLock) so a boundary LSN is never captured
	// while a commit's entries are applied-but-unappended. Deadlock-free:
	// read-holders never wait on gs.mu, and the two lock-orders
	// (mu.Lock→barrier.RLock vs mu.RLock→barrier.Lock) cannot overlap
	// because gs.mu already excludes them from each other.
	txWALBarrier sync.RWMutex

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
	// walReplaySawPlaintext is set during constructor-time WAL replay
	// when encryption is enabled but a legacy plaintext entry was
	// replayed — the constructor then runs CompactWAL once so pre-toggle
	// plaintext leaves the disk (H-3). Constructor-only; no locking.
	walReplaySawPlaintext bool

	// observers is the registered NodeObserver slice. Mutated by
	// AddObserver under gs.mu.Lock; snapshot-copied for dispatch under
	// gs.mu.RLock by snapshotObservers (pkg/storage/observation.go).
	// R2.1 / S11 spike §7.4.
	observers []NodeObserver
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

	// EncryptionEngine/KeyManager wire at-rest encryption at CONSTRUCTION
	// time, so the constructor's loadFromDisk can decrypt an encrypted
	// snapshot. SetEncryption after construction is too late for that
	// path — before M-14 an encrypted snapshot could never load at
	// restart (the server exited with "encryption is not enabled").
	EncryptionEngine encryption.EncryptDecrypter
	KeyManager       encryption.KeyProvider
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
