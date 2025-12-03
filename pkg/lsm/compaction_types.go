package lsm

import (
	"os"
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

// CompactionStats tracks compaction metrics
type CompactionStats struct {
	TotalCompactions int64
	BytesRead        int64
	BytesWritten     int64
	KeysRemoved      int64 // Deleted or duplicate keys
}
