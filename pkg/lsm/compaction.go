package lsm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// CompactionStrategy defines how SSTables are compacted
type CompactionStrategy interface {
	SelectCompaction(levels [][]*SSTable) *CompactionPlan
}

// CompactionPlan describes which SSTables to compact
type CompactionPlan struct {
	Level       int
	SSTables    []*SSTable
	OutputLevel int
}

// LeveledCompactionStrategy implements leveled compaction (like LevelDB/RocksDB)
// - Level 0: Multiple overlapping SSTables (from MemTable flushes)
// - Level 1+: Non-overlapping SSTables, size increases by 10x per level
type LeveledCompactionStrategy struct {
	Level0FileLimit int     // Max files in L0 before compaction
	LevelSizeRatio  float64 // Size ratio between levels (default 10.0)
	MaxLevels       int     // Maximum number of levels
}

// DefaultLeveledCompaction returns default leveled compaction config
func DefaultLeveledCompaction() *LeveledCompactionStrategy {
	return &LeveledCompactionStrategy{
		Level0FileLimit: 4,
		LevelSizeRatio:  10.0,
		MaxLevels:       7,
	}
}

// SelectCompaction determines which SSTables to compact
func (lcs *LeveledCompactionStrategy) SelectCompaction(levels [][]*SSTable) *CompactionPlan {
	// Check if L0 needs compaction
	if len(levels) > 0 && len(levels[0]) >= lcs.Level0FileLimit {
		return &CompactionPlan{
			Level:       0,
			SSTables:    levels[0],
			OutputLevel: 1,
		}
	}

	// Check higher levels based on size ratio
	for level := 1; level < len(levels)-1; level++ {
		levelSize := calculateLevelSize(levels[level])
		nextLevelSize := calculateLevelSize(levels[level+1])

		// If this level is too large relative to next level, compact it
		if float64(levelSize) > lcs.LevelSizeRatio*float64(nextLevelSize) {
			return &CompactionPlan{
				Level:       level,
				SSTables:    levels[level],
				OutputLevel: level + 1,
			}
		}
	}

	return nil // No compaction needed
}

// calculateLevelSize returns total size of all SSTables in a level
func calculateLevelSize(sstables []*SSTable) int64 {
	var size int64
	for _, sst := range sstables {
		info, err := os.Stat(sst.path)
		if err == nil {
			size += info.Size()
		}
	}
	return size
}

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

// Compact merges SSTables according to the plan
func (c *Compactor) Compact(plan *CompactionPlan) ([]*SSTable, error) {
	if plan == nil || len(plan.SSTables) == 0 {
		return nil, nil
	}

	// Collect all entries from input SSTables
	allEntries := make([]*Entry, 0)

	for _, sst := range plan.SSTables {
		entries, err := sst.Iterator()
		if err != nil {
			return nil, err
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
	outputSSTables := make([]*SSTable, 0)

	currentBatch := make([]*Entry, 0)
	currentSize := 0
	sstableID := 0

	for _, entry := range deduplicated {
		entrySize := len(entry.Key) + len(entry.Value) + 20 // Approximate

		if currentSize+entrySize > maxSSTableSize && len(currentBatch) > 0 {
			// Flush current batch
			path := SSTablePath(c.dataDir, plan.OutputLevel, sstableID)
			sst, err := NewSSTable(path, currentBatch)
			if err != nil {
				return nil, err
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
		sst, err := NewSSTable(path, currentBatch)
		if err != nil {
			return nil, err
		}
		outputSSTables = append(outputSSTables, sst)
	}

	return outputSSTables, nil
}

// CleanupOldSSTables removes old SSTables after compaction
func (c *Compactor) CleanupOldSSTables(sstables []*SSTable) error {
	for _, sst := range sstables {
		if err := sst.Delete(); err != nil {
			return err
		}
	}
	return nil
}

// CompactionStats tracks compaction metrics
type CompactionStats struct {
	TotalCompactions int64
	BytesRead        int64
	BytesWritten     int64
	KeysRemoved      int64 // Deleted or duplicate keys
}

// MergeIterator merges multiple sorted iterators
type MergeIterator struct {
	iterators []*SSTableIterator
	current   *Entry
}

// SSTableIterator iterates over an SSTable
type SSTableIterator struct {
	sst     *SSTable
	entries []*Entry
	index   int
}

// NewSSTableIterator creates an iterator for an SSTable
func NewSSTableIterator(sst *SSTable) (*SSTableIterator, error) {
	entries, err := sst.Iterator()
	if err != nil {
		return nil, err
	}

	return &SSTableIterator{
		sst:     sst,
		entries: entries,
		index:   0,
	}, nil
}

// Next advances the iterator
func (it *SSTableIterator) Next() (*Entry, bool) {
	if it.index >= len(it.entries) {
		return nil, false
	}

	entry := it.entries[it.index]
	it.index++
	return entry, true
}

// Peek returns current entry without advancing
func (it *SSTableIterator) Peek() (*Entry, bool) {
	if it.index >= len(it.entries) {
		return nil, false
	}
	return it.entries[it.index], true
}

// NewMergeIterator creates an iterator that merges multiple SSTables
func NewMergeIterator(sstables []*SSTable) (*MergeIterator, error) {
	iterators := make([]*SSTableIterator, 0, len(sstables))

	for _, sst := range sstables {
		it, err := NewSSTableIterator(sst)
		if err != nil {
			return nil, err
		}
		iterators = append(iterators, it)
	}

	return &MergeIterator{
		iterators: iterators,
	}, nil
}

// Next returns the next entry in sorted order across all iterators
func (mi *MergeIterator) Next() (*Entry, bool) {
	var minEntry *Entry
	var minIdx int = -1

	// Find minimum key across all iterators
	for i, it := range mi.iterators {
		entry, ok := it.Peek()
		if !ok {
			continue
		}

		if minEntry == nil || EntryCompare(entry, minEntry) < 0 {
			minEntry = entry
			minIdx = i
		}
	}

	if minIdx == -1 {
		return nil, false
	}

	// Advance the iterator with minimum key
	mi.iterators[minIdx].Next()
	return minEntry, true
}

// ListSSTables returns all SSTable files in a directory
func ListSSTables(dir string) ([][]*SSTable, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sst"))
	if err != nil {
		return nil, err
	}

	// Group by level
	levelMap := make(map[int][]*SSTable)

	for _, path := range files {
		var level, id int
		_, err := fmt.Sscanf(filepath.Base(path), "L%d-%d.sst", &level, &id)
		if err != nil {
			continue
		}

		sst, err := OpenSSTable(path)
		if err != nil {
			continue
		}

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

	return levels, nil
}
