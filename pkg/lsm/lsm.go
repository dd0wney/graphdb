package lsm

import (
	"fmt"
	"os"
	"sync"
	"time"
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
	dataDir         string
	memTableSize    int
	compactionStrategy CompactionStrategy
	compactor       *Compactor

	// Background workers
	flushChan      chan struct{}
	compactionChan chan struct{}
	stopChan       chan struct{}
	wg             sync.WaitGroup

	// Statistics
	stats LSMStats
}

// LSMStats tracks LSM storage statistics
type LSMStats struct {
	mu sync.Mutex

	WriteCount       int64
	ReadCount        int64
	FlushCount       int64
	CompactionCount  int64
	BytesWritten     int64
	BytesRead        int64
	MemTableSize     int
	SSTableCount     int
	Level0FileCount  int
}

// LSMOptions configures LSM storage
type LSMOptions struct {
	DataDir            string
	MemTableSize       int // Bytes (default 4MB)
	CompactionStrategy CompactionStrategy
	EnableAutoCompaction bool
}

// DefaultLSMOptions returns default LSM configuration
func DefaultLSMOptions(dataDir string) LSMOptions {
	return LSMOptions{
		DataDir:            dataDir,
		MemTableSize:       4 * 1024 * 1024, // 4MB
		CompactionStrategy: DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	}
}

// NewLSMStorage creates a new LSM storage engine
func NewLSMStorage(opts LSMOptions) (*LSMStorage, error) {
	// Create data directory
	if err := os.MkdirAll(opts.DataDir, 0755); err != nil {
		return nil, err
	}

	// Load existing SSTables
	levels, err := ListSSTables(opts.DataDir)
	if err != nil {
		return nil, err
	}

	lsm := &LSMStorage{
		memTable:           NewMemTable(opts.MemTableSize),
		immutableTable:     nil,
		levels:             levels,
		cache:              NewBlockCache(10000), // Cache 10k blocks
		dataDir:            opts.DataDir,
		memTableSize:       opts.MemTableSize,
		compactionStrategy: opts.CompactionStrategy,
		compactor:          NewCompactor(opts.DataDir, opts.CompactionStrategy),
		flushChan:          make(chan struct{}, 1),
		compactionChan:     make(chan struct{}, 1),
		stopChan:           make(chan struct{}),
	}

	// Start background workers
	if opts.EnableAutoCompaction {
		lsm.wg.Add(2)
		go lsm.flushWorker()
		go lsm.compactionWorker()
	}

	return lsm, nil
}

// Put writes a key-value pair
func (lsm *LSMStorage) Put(key, value []byte) error {
	lsm.mu.Lock()

	// Invalidate cache entry for this key (handles updates)
	cacheKey := string(key)
	lsm.cache.Delete(cacheKey)

	// Write to MemTable
	if err := lsm.memTable.Put(key, value); err != nil {
		lsm.mu.Unlock()
		return err
	}

	// Update stats
	lsm.stats.mu.Lock()
	lsm.stats.WriteCount++
	lsm.stats.BytesWritten += int64(len(key) + len(value))
	lsm.stats.mu.Unlock()

	// Check if MemTable is full
	needsFlush := lsm.memTable.IsFull()
	lsm.mu.Unlock()

	if needsFlush {
		lsm.triggerFlush()
	}

	return nil
}

// Get retrieves a value by key
func (lsm *LSMStorage) Get(key []byte) ([]byte, bool) {
	lsm.mu.RLock()
	defer lsm.mu.RUnlock()

	// Update stats
	lsm.stats.mu.Lock()
	lsm.stats.ReadCount++
	lsm.stats.mu.Unlock()

	// 0. Check cache first
	cacheKey := string(key)
	if value, ok := lsm.cache.Get(cacheKey); ok {
		return value, true
	}

	// 1. Check active MemTable
	if entry, ok := lsm.memTable.Get(key); ok {
		// Add to cache
		lsm.cache.Put(cacheKey, entry.Value)
		return entry.Value, true
	}

	// 2. Check immutable MemTable
	if lsm.immutableTable != nil {
		if entry, ok := lsm.immutableTable.Get(key); ok {
			lsm.cache.Put(cacheKey, entry.Value)
			return entry.Value, true
		}
	}

	// 3. Check SSTables from newest to oldest
	for level := 0; level < len(lsm.levels); level++ {
		for i := len(lsm.levels[level]) - 1; i >= 0; i-- {
			sst := lsm.levels[level][i]
			if entry, ok := sst.Get(key); ok {
				// Add to cache
				lsm.cache.Put(cacheKey, entry.Value)
				return entry.Value, true
			}
		}
	}

	return nil, false
}

// Delete removes a key (writes tombstone)
func (lsm *LSMStorage) Delete(key []byte) error {
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	// Invalidate cache entry for this key
	cacheKey := string(key)
	lsm.cache.Delete(cacheKey)

	return lsm.memTable.Delete(key)
}

// Scan returns all key-value pairs in range [start, end)
func (lsm *LSMStorage) Scan(start, end []byte) (map[string][]byte, error) {
	lsm.mu.RLock()
	defer lsm.mu.RUnlock()

	results := make(map[string][]byte)

	// Scan MemTable
	memEntries := lsm.memTable.Scan(start, end)
	for _, entry := range memEntries {
		results[string(entry.Key)] = entry.Value
	}

	// Scan immutable MemTable
	if lsm.immutableTable != nil {
		immEntries := lsm.immutableTable.Scan(start, end)
		for _, entry := range immEntries {
			if _, exists := results[string(entry.Key)]; !exists {
				results[string(entry.Key)] = entry.Value
			}
		}
	}

	// Scan SSTables
	for level := 0; level < len(lsm.levels); level++ {
		for _, sst := range lsm.levels[level] {
			entries, err := sst.Scan(start, end)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if _, exists := results[string(entry.Key)]; !exists {
					results[string(entry.Key)] = entry.Value
				}
			}
		}
	}

	return results, nil
}

// triggerFlush signals the flush worker
func (lsm *LSMStorage) triggerFlush() {
	select {
	case lsm.flushChan <- struct{}{}:
	default:
	}
}

// triggerCompaction signals the compaction worker
func (lsm *LSMStorage) triggerCompaction() {
	select {
	case lsm.compactionChan <- struct{}{}:
	default:
	}
}

// flushWorker handles MemTable -> SSTable flushes
func (lsm *LSMStorage) flushWorker() {
	defer lsm.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lsm.flushChan:
			lsm.flush()
		case <-ticker.C:
			// Periodic check
			lsm.mu.RLock()
			needsFlush := lsm.memTable.IsFull()
			lsm.mu.RUnlock()
			if needsFlush {
				lsm.flush()
			}
		case <-lsm.stopChan:
			return
		}
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
	sst, err := NewSSTable(sstPath, entries)
	if err != nil {
		return err
	}

	// Add to L0
	lsm.mu.Lock()
	if len(lsm.levels) == 0 {
		lsm.levels = make([][]*SSTable, 1)
	}
	lsm.levels[0] = append(lsm.levels[0], sst)
	lsm.immutableTable = nil

	// Update stats
	lsm.stats.mu.Lock()
	lsm.stats.FlushCount++
	lsm.stats.SSTableCount++
	lsm.stats.Level0FileCount = len(lsm.levels[0])
	lsm.stats.mu.Unlock()

	lsm.mu.Unlock()

	// Trigger compaction if needed
	lsm.triggerCompaction()

	return nil
}

// compactionWorker handles SSTable compaction
func (lsm *LSMStorage) compactionWorker() {
	defer lsm.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lsm.compactionChan:
			lsm.compact()
		case <-ticker.C:
			lsm.compact()
		case <-lsm.stopChan:
			return
		}
	}
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

	// Update stats
	lsm.stats.mu.Lock()
	lsm.stats.CompactionCount++
	lsm.stats.SSTableCount = lsm.countSSTables()
	if len(lsm.levels) > 0 {
		lsm.stats.Level0FileCount = len(lsm.levels[0])
	}
	lsm.stats.mu.Unlock()

	lsm.mu.Unlock()

	// Cleanup old SSTables
	lsm.compactor.CleanupOldSSTables(plan.SSTables)

	return nil
}

// countSSTables returns total number of SSTables
func (lsm *LSMStorage) countSSTables() int {
	count := 0
	for _, level := range lsm.levels {
		count += len(level)
	}
	return count
}

// GetStats returns current statistics
func (lsm *LSMStorage) GetStats() LSMStats {
	lsm.stats.mu.Lock()
	defer lsm.stats.mu.Unlock()

	lsm.mu.RLock()
	lsm.stats.MemTableSize = lsm.memTable.Size()
	lsm.stats.SSTableCount = lsm.countSSTables()
	if len(lsm.levels) > 0 {
		lsm.stats.Level0FileCount = len(lsm.levels[0])
	}
	lsm.mu.RUnlock()

	// Return a copy without the mutex to avoid copying lock value
	return LSMStats{
		WriteCount:      lsm.stats.WriteCount,
		ReadCount:       lsm.stats.ReadCount,
		FlushCount:      lsm.stats.FlushCount,
		CompactionCount: lsm.stats.CompactionCount,
		BytesWritten:    lsm.stats.BytesWritten,
		BytesRead:       lsm.stats.BytesRead,
		MemTableSize:    lsm.stats.MemTableSize,
		SSTableCount:    lsm.stats.SSTableCount,
		Level0FileCount: lsm.stats.Level0FileCount,
	}
}

// Close flushes pending writes and stops background workers
func (lsm *LSMStorage) Close() error {
	// Stop workers
	close(lsm.stopChan)
	lsm.wg.Wait()

	// Final flush
	if lsm.memTable.Size() > 0 {
		lsm.flush()
	}

	// Close all SSTables
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	for _, level := range lsm.levels {
		for _, sst := range level {
			sst.Close()
		}
	}

	return nil
}

// PrintStats prints storage statistics
func (lsm *LSMStorage) PrintStats() {
	stats := lsm.GetStats()

	fmt.Printf("LSM Storage Statistics:\n")
	fmt.Printf("  Writes: %d (%.2f MB)\n", stats.WriteCount, float64(stats.BytesWritten)/(1024*1024))
	fmt.Printf("  Reads: %d (%.2f MB)\n", stats.ReadCount, float64(stats.BytesRead)/(1024*1024))
	fmt.Printf("  Flushes: %d\n", stats.FlushCount)
	fmt.Printf("  Compactions: %d\n", stats.CompactionCount)
	fmt.Printf("  MemTable Size: %.2f KB\n", float64(stats.MemTableSize)/1024)
	fmt.Printf("  Total SSTables: %d\n", stats.SSTableCount)
	fmt.Printf("  L0 Files: %d\n", stats.Level0FileCount)
}
