package lsm

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
)

// Compactor performs SSTable compaction
type Compactor struct {
	strategy CompactionStrategy
	dataDir  string
}

// NewCompactor creates a new compactor
func NewCompactor(dataDir string, strategy CompactionStrategy) *Compactor {
	return &Compactor{
		strategy: strategy,
		dataDir:  dataDir,
	}
}

// Compact merges SSTables according to the plan.
// This is a critical background operation - panic recovery prevents corruption.
// On error, any partially created output SSTables are cleaned up.
func (c *Compactor) Compact(plan *CompactionPlan) (result []*SSTable, err error) {
	// Track output SSTables for cleanup on error
	var outputSSTables []*SSTable

	// Panic recovery for critical compaction path
	// A panic here could leave the LSM tree in an inconsistent state
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in Compact (level=%d, tables=%d): %v",
				plan.Level, len(plan.SSTables), r)
			err = fmt.Errorf("panic during compaction: %v", r)
			result = nil
			// Clean up any partially created output SSTables
			c.cleanupOutputSSTables(outputSSTables)
		}
	}()

	// Cleanup function to remove partially created output SSTables on error
	cleanup := func() {
		c.cleanupOutputSSTables(outputSSTables)
		outputSSTables = nil
	}

	if plan == nil || len(plan.SSTables) == 0 {
		return nil, nil
	}

	// Collect all entries from input SSTables
	allEntries := make([]*Entry, 0)

	for _, sst := range plan.SSTables {
		entries, iterErr := sst.Iterator()
		if iterErr != nil {
			cleanup()
			return nil, fmt.Errorf("iterate SSTable %s: %w", sst.path, iterErr)
		}
		allEntries = append(allEntries, entries...)
	}

	// Sort all entries by key, then timestamp (newest first)
	sort.Slice(allEntries, func(i, j int) bool {
		cmp := EntryCompare(allEntries[i], allEntries[j])
		if cmp == 0 {
			// Same key: keep newest
			return allEntries[i].Timestamp > allEntries[j].Timestamp
		}
		return cmp < 0
	})

	// Deduplicate: keep only newest version of each key
	deduplicated := make([]*Entry, 0)
	var lastKey []byte

	for _, entry := range allEntries {
		keyStr := string(entry.Key)
		if lastKey == nil || keyStr != string(lastKey) {
			// Skip tombstones during compaction (remove deleted entries)
			if !entry.Deleted {
				deduplicated = append(deduplicated, entry)
			}
			lastKey = entry.Key
		}
		// Else: duplicate key, skip (we already have the newest version)
	}

	if len(deduplicated) == 0 {
		return nil, nil
	}

	// Split into multiple SSTables if needed (max 64MB per SSTable)
	const maxSSTableSize = 64 * 1024 * 1024

	currentBatch := make([]*Entry, 0)
	currentSize := 0
	sstableID := 0

	for _, entry := range deduplicated {
		entrySize := len(entry.Key) + len(entry.Value) + 20 // Approximate

		if currentSize+entrySize > maxSSTableSize && len(currentBatch) > 0 {
			// Flush current batch
			path := SSTablePath(c.dataDir, plan.OutputLevel, sstableID)
			sst, createErr := NewSSTable(path, currentBatch)
			if createErr != nil {
				cleanup()
				return nil, fmt.Errorf("create SSTable %s: %w", path, createErr)
			}
			outputSSTables = append(outputSSTables, sst)

			currentBatch = make([]*Entry, 0)
			currentSize = 0
			sstableID++
		}

		currentBatch = append(currentBatch, entry)
		currentSize += entrySize
	}

	// Flush remaining entries
	if len(currentBatch) > 0 {
		path := SSTablePath(c.dataDir, plan.OutputLevel, sstableID)
		sst, createErr := NewSSTable(path, currentBatch)
		if createErr != nil {
			cleanup()
			return nil, fmt.Errorf("create SSTable %s: %w", path, createErr)
		}
		outputSSTables = append(outputSSTables, sst)
	}

	return outputSSTables, nil
}

// cleanupOutputSSTables removes output SSTables created during a failed compaction.
// Logs errors but does not return them since we're already in an error path.
func (c *Compactor) cleanupOutputSSTables(sstables []*SSTable) {
	for _, sst := range sstables {
		if sst == nil {
			continue
		}
		if err := sst.Delete(); err != nil {
			log.Printf("Warning: failed to cleanup output SSTable %s: %v", sst.path, err)
		}
	}
}

// CleanupOldSSTables removes old SSTables after compaction.
// Continues on errors to avoid leaving the LSM tree in an inconsistent state.
// Returns an error aggregating all deletion failures.
func (c *Compactor) CleanupOldSSTables(sstables []*SSTable) error {
	var errs []error
	for _, sst := range sstables {
		if err := sst.Delete(); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", sst.path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to cleanup %d of %d SSTables: %v", len(errs), len(sstables), errs[0])
	}
	return nil
}

// ListSSTables returns all SSTable files in a directory.
// Returns partial results even if some SSTables fail to open, along with an error
// describing the failures. Callers should check both the result and error.
func ListSSTables(dir string) ([][]*SSTable, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sst"))
	if err != nil {
		return nil, fmt.Errorf("glob SSTable files: %w", err)
	}

	// Group by level
	levelMap := make(map[int][]*SSTable)
	var openedSSTables []*SSTable // Track for cleanup on error
	var errs []error

	for _, path := range files {
		var level, id int
		_, err := fmt.Sscanf(filepath.Base(path), "L%d-%d.sst", &level, &id)
		if err != nil {
			log.Printf("Warning: SSTable file has invalid name format: %s", path)
			continue
		}

		sst, err := OpenSSTable(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("open %s: %w", path, err))
			continue
		}

		openedSSTables = append(openedSSTables, sst)
		levelMap[level] = append(levelMap[level], sst)
	}

	// Convert to slice of levels
	maxLevel := 0
	for level := range levelMap {
		if level > maxLevel {
			maxLevel = level
		}
	}

	levels := make([][]*SSTable, maxLevel+1)
	for level := 0; level <= maxLevel; level++ {
		levels[level] = levelMap[level]
	}

	// Return partial results with error if any files failed to open
	if len(errs) > 0 {
		return levels, fmt.Errorf("failed to open %d SSTable(s): %v", len(errs), errs[0])
	}

	return levels, nil
}
