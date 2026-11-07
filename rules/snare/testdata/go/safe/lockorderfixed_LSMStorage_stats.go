// Snare benchmark fixture, set 2, rule graphdb-lock-order-inversion.
// Source: pkg/lsm/lsm_types.go at b9bc9d2, lines 10-58 (declarations).
// Source: pkg/lsm/lsm_types.go at b9bc9d2, lines 82-93 (declarations).
// Source: pkg/lsm/lsm.go at 0d1c650, lines 226-253, function LSMStorage.GetStats.
// Source: pkg/lsm/lsm_workers.go at 0d1c650, lines 54-122, function LSMStorage.flush.
// Source: pkg/lsm/lsm_workers.go at 0d1c650, lines 147-221, function LSMStorage.compact.
// Label: safe.
// Reason: The fix on origin/fix/lsm-getstats-lock-order. GetStats now takes lsm.mu before lsm.stats.mu, the same order as flush and compact.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
	fs                 vfs.FileSystem
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

// GetStats returns current statistics as a snapshot
func (lsm *LSMStorage) GetStats() LSMStatsSnapshot {
	// lsm.mu before lsm.stats.mu, the order flush and compact use. The reverse
	// order deadlocks against a concurrent flush.
	lsm.mu.RLock()
	lsm.stats.mu.Lock()
	memTableSize := lsm.memTable.Size()
	ssTableCount := lsm.countSSTables()
	level0Count := 0
	if len(lsm.levels) > 0 {
		level0Count = len(lsm.levels[0])
	}
	lsm.stats.mu.Unlock()
	lsm.mu.RUnlock()

	// Return snapshot with atomic loads for high-frequency counters
	return LSMStatsSnapshot{
		WriteCount:      lsm.stats.WriteCount.Load(),
		ReadCount:       lsm.stats.ReadCount.Load(),
		FlushCount:      lsm.stats.FlushCount.Load(),
		CompactionCount: lsm.stats.CompactionCount.Load(),
		BytesWritten:    lsm.stats.BytesWritten.Load(),
		BytesRead:       lsm.stats.BytesRead.Load(),
		MemTableSize:    memTableSize,
		SSTableCount:    ssTableCount,
		Level0FileCount: level0Count,
	}
}

// flush writes MemTable to SSTable
func (lsm *LSMStorage) flush() error {
	lsm.mu.Lock()

	// Swap MemTable to immutable
	if lsm.immutableTable != nil {
		lsm.mu.Unlock()
		return nil // Already flushing
	}

	lsm.immutableTable = lsm.memTable
	lsm.memTable = NewMemTable(lsm.memTableSize)
	lsm.mu.Unlock()

	// Get entries from immutable table
	entries := lsm.immutableTable.Iterator()

	if len(entries) == 0 {
		lsm.mu.Lock()
		lsm.immutableTable = nil
		lsm.mu.Unlock()
		return nil
	}

	// Create SSTable
	sstPath := SSTablePath(lsm.dataDir, 0, int(time.Now().UnixNano()))
	sst, err := NewSSTableWithFS(sstPath, entries, lsm.fs)
	if err != nil {
		lsm.mu.Lock()
		// Put the entries back into the active memtable, and clear the
		// immutable one.
		//
		// Clearing it matters because leaving it set makes every later flush
		// take the "already flushing" arm above, so flushing stops for the
		// life of the store and the memtable grows without a bound.
		//
		// Putting the entries back matters just as much, and the first
		// attempt at this fix missed it. Dropping them loses writes that Put
		// had acknowledged and that a reader could see a moment earlier, and
		// the flush sweep caught it through the block cache: the cache held a
		// key that no table held any more. A later flush now retries them.
		lsm.memTable.MergeOlder(lsm.immutableTable)
		lsm.immutableTable = nil
		lsm.mu.Unlock()
		return err
	}

	// Add to L0
	lsm.mu.Lock()
	if len(lsm.levels) == 0 {
		lsm.levels = make([][]*SSTable, 1)
	}
	lsm.levels[0] = append(lsm.levels[0], sst)
	lsm.immutableTable = nil

	// Update stats (FlushCount is atomic, others need mutex)
	lsm.stats.FlushCount.Add(1)
	lsm.stats.mu.Lock()
	lsm.stats.SSTableCount++
	lsm.stats.Level0FileCount = len(lsm.levels[0])
	lsm.stats.mu.Unlock()

	lsm.mu.Unlock()

	// Trigger compaction if needed
	lsm.triggerCompaction()

	return nil
}

// compact performs compaction based on strategy
func (lsm *LSMStorage) compact() error {
	lsm.mu.RLock()
	plan := lsm.compactionStrategy.SelectCompaction(lsm.levels)
	lsm.mu.RUnlock()

	if plan == nil {
		return nil // No compaction needed
	}

	// Perform compaction
	newSSTables, err := lsm.compactor.Compact(plan)
	if err != nil {
		return err
	}

	// Update levels using copy-on-write to avoid race with concurrent reads
	lsm.mu.Lock()

	// Create a new copy of levels to avoid modifying while readers have references
	newLevels := make([][]*SSTable, len(lsm.levels))
	for i := range lsm.levels {
		if i == plan.Level {
			// Skip old SSTables from source level (remove them)
			newLevels[i] = make([]*SSTable, 0)
		} else {
			// Copy other levels unchanged
			newLevels[i] = lsm.levels[i]
		}
	}

	// Add new SSTables to output level
	if plan.OutputLevel >= len(newLevels) {
		// Extend levels array
		for i := len(newLevels); i <= plan.OutputLevel; i++ {
			newLevels = append(newLevels, make([]*SSTable, 0))
		}
	}
	newLevels[plan.OutputLevel] = append(newLevels[plan.OutputLevel], newSSTables...)

	// Atomically replace levels (readers with old reference will continue safely)
	lsm.levels = newLevels

	// Update stats (CompactionCount is atomic, others need mutex)
	lsm.stats.CompactionCount.Add(1)
	lsm.stats.mu.Lock()
	lsm.stats.SSTableCount = lsm.countSSTables()
	if len(lsm.levels) > 0 {
		lsm.stats.Level0FileCount = len(lsm.levels[0])
	}
	lsm.stats.mu.Unlock()

	lsm.mu.Unlock()

	// Cleanup old SSTables.
	//
	// A failure here leaves the superseded tables on the disk while the levels
	// already point at their replacement. NewLSMStorage rebuilds the levels by
	// reading that directory, and Get scans level 0 before level 1 — which is
	// correct while L0 really is the newest data, and wrong for an input table
	// a compaction has already superseded. So a reopened store serves the
	// pre-delete value, and a key that was deleted is readable again.
	//
	// One retry covers the transient case, which is the common one. A removal
	// that still fails is reported with its consequence named, because the
	// caller can act on it now and cannot act on it after the next open.
	if err := lsm.compactor.CleanupOldSSTables(plan.SSTables); err != nil {
		if retryErr := lsm.compactor.CleanupOldSSTables(plan.SSTables); retryErr != nil {
			return fmt.Errorf("compaction left superseded SSTables on the disk, which a reopen "+
				"would load beside their replacement and read as live data: %w", retryErr)
		}
	}

	return nil
}
