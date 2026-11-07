// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/lsm/lsm.go at b9bc9d2, lines 226-252, function LSMStorage.GetStats.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (RULE-FALSE-POSITIVE): the matched call is lsm.mu.RLock of a second mutex.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
