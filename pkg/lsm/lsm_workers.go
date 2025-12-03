package lsm

import (
	"fmt"
	"log"
	"time"
)

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
			if err := lsm.flush(); err != nil {
				log.Printf("ERROR: flush failed: %v", err)
			}
		case <-ticker.C:
			// Periodic check
			lsm.mu.RLock()
			needsFlush := lsm.memTable.IsFull()
			lsm.mu.RUnlock()
			if needsFlush {
				if err := lsm.flush(); err != nil {
					log.Printf("ERROR: periodic flush failed: %v", err)
				}
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

// compactionWorker handles SSTable compaction
func (lsm *LSMStorage) compactionWorker() {
	defer lsm.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-lsm.compactionChan:
			if err := lsm.compact(); err != nil {
				log.Printf("ERROR: compaction failed: %v", err)
			}
		case <-ticker.C:
			if err := lsm.compact(); err != nil {
				log.Printf("ERROR: periodic compaction failed: %v", err)
			}
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

	// Update stats (CompactionCount is atomic, others need mutex)
	lsm.stats.CompactionCount.Add(1)
	lsm.stats.mu.Lock()
	lsm.stats.SSTableCount = lsm.countSSTables()
	if len(lsm.levels) > 0 {
		lsm.stats.Level0FileCount = len(lsm.levels[0])
	}
	lsm.stats.mu.Unlock()

	lsm.mu.Unlock()

	// Cleanup old SSTables
	if err := lsm.compactor.CleanupOldSSTables(plan.SSTables); err != nil {
		return fmt.Errorf("failed to cleanup old SSTables: %w", err)
	}

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
