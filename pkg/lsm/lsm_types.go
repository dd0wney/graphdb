package lsm

import (
	"sync"
	"sync/atomic"
)

// LSMStorage is the main LSM-tree storage engine
type LSMStorage struct {
	mu sync.RWMutex

	// Write path
	memTable       *MemTable
	immutableTable *MemTable // Being flushed to disk

	// Read path
	levels [][]*SSTable
	cache  *BlockCache // LRU cache for hot data

	// Configuration
	dataDir            string
	memTableSize       int
	compactionStrategy CompactionStrategy
	compactor          *Compactor

	// Background workers
	flushChan      chan struct{}
	compactionChan chan struct{}
	stopChan       chan struct{}
	wg             sync.WaitGroup

	// State
	closed bool

	// Statistics
	stats LSMStats
}

// LSMStats tracks LSM storage statistics using lock-free atomic counters
// for high-frequency operations (reads/writes) to avoid contention.
type LSMStats struct {
	// High-frequency counters use atomics (lock-free)
	WriteCount      atomic.Int64
	ReadCount       atomic.Int64
	FlushCount      atomic.Int64
	CompactionCount atomic.Int64
	BytesWritten    atomic.Int64
	BytesRead       atomic.Int64

	// Low-frequency counters protected by mutex
	mu              sync.Mutex
	MemTableSize    int
	SSTableCount    int
	Level0FileCount int
}

// LSMOptions configures LSM storage
type LSMOptions struct {
	DataDir              string
	MemTableSize         int // Bytes (default 4MB)
	CompactionStrategy   CompactionStrategy
	EnableAutoCompaction bool
}

// DefaultLSMOptions returns default LSM configuration
func DefaultLSMOptions(dataDir string) LSMOptions {
	return LSMOptions{
		DataDir:              dataDir,
		MemTableSize:         4 * 1024 * 1024, // 4MB
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	}
}

// LSMStatsSnapshot is a point-in-time snapshot of LSM statistics
type LSMStatsSnapshot struct {
	WriteCount      int64
	ReadCount       int64
	FlushCount      int64
	CompactionCount int64
	BytesWritten    int64
	BytesRead       int64
	MemTableSize    int
	SSTableCount    int
	Level0FileCount int
}
