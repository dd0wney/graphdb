// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/lsm/lsm_workers.go at b9bc9d2, lines 54-122, function LSMStorage.flush.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (RULE-FALSE-POSITIVE at :56 and SAFE at :82): the Lock at :56 pairs with the Unlock at :116 across the Unlock at :66; MergeOlder handles nil and writes only to a map NewMemTable made.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
