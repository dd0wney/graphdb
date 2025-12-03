package lsm

import (
	"fmt"
	"os"
)

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
		cache:              NewBlockCache(100000), // Cache 100k blocks (10x increase for large graphs)
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

	// Update stats atomically (lock-free)
	lsm.stats.WriteCount.Add(1)
	lsm.stats.BytesWritten.Add(int64(len(key) + len(value)))

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

	// Update stats atomically (lock-free)
	lsm.stats.ReadCount.Add(1)

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

// Sync forces a flush of the current memtable to disk
func (lsm *LSMStorage) Sync() error {
	lsm.mu.Lock()
	needsFlush := lsm.memTable.Size() > 0
	lsm.mu.Unlock()

	if needsFlush {
		if err := lsm.flush(); err != nil {
			return fmt.Errorf("failed to flush memtable: %w", err)
		}
	}
	return nil
}

// GetStats returns current statistics as a snapshot
func (lsm *LSMStorage) GetStats() LSMStatsSnapshot {
	// Get mutex-protected stats
	lsm.stats.mu.Lock()
	lsm.mu.RLock()
	memTableSize := lsm.memTable.Size()
	ssTableCount := lsm.countSSTables()
	level0Count := 0
	if len(lsm.levels) > 0 {
		level0Count = len(lsm.levels[0])
	}
	lsm.mu.RUnlock()
	lsm.stats.mu.Unlock()

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

// Close flushes pending writes and stops background workers
func (lsm *LSMStorage) Close() error {
	lsm.mu.Lock()
	if lsm.closed {
		lsm.mu.Unlock()
		return nil // Already closed
	}
	lsm.closed = true
	lsm.mu.Unlock()

	// Stop workers
	close(lsm.stopChan)
	lsm.wg.Wait()

	// Final flush
	if lsm.memTable.Size() > 0 {
		if err := lsm.flush(); err != nil {
			return fmt.Errorf("final flush failed: %w", err)
		}
	}

	// Close all SSTables
	lsm.mu.Lock()
	defer lsm.mu.Unlock()

	var closeErrors []error
	for _, level := range lsm.levels {
		for _, sst := range level {
			if err := sst.Close(); err != nil {
				closeErrors = append(closeErrors, err)
			}
		}
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf("failed to close %d SSTables: %v", len(closeErrors), closeErrors[0])
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
