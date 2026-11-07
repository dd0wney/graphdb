// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/lsm/lsm_workers.go at b9bc9d2, lines 147-221, function LSMStorage.compact.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): the call is an atomic Add; index operations are guarded.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
