package lsm

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"log"
	"path/filepath"
	"sort"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Compactor performs SSTable compaction
type Compactor struct {
	strategy CompactionStrategy
	dataDir  string
	// fs is the driver the tables this compactor writes will carry.
	fs vfs.FileSystem
}

// NewCompactor creates a new compactor
func NewCompactor(dataDir string, strategy CompactionStrategy) *Compactor {
	return NewCompactorWithFS(dataDir, strategy, vfs.Default())
}

// NewCompactorWithFS creates a compactor on a caller-supplied driver.
func NewCompactorWithFS(dataDir string, strategy CompactionStrategy, fs vfs.FileSystem) *Compactor {
	if fs == nil {
		fs = vfs.Default()
	}
	return &Compactor{
		strategy: strategy,
		dataDir:  dataDir,
		fs:       fs,
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
			sst, createErr := NewSSTableWithFS(path, currentBatch, c.fs)
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
		sst, createErr := NewSSTableWithFS(path, currentBatch, c.fs)
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
//
// A file that is already gone counts as removed. The postcondition this is
// asked for is absence, not a successful syscall, and treating ENOENT as a
// failure made the operation impossible to retry: the first pass removes what
// it can and stops short on one file, so a second pass reports every file the
// first one succeeded on. The compaction sweep found that immediately after
// the retry was added.
func (c *Compactor) CleanupOldSSTables(sstables []*SSTable) error {
	var errs []error
	for _, sst := range sstables {
		if err := sst.Delete(); err != nil && !errors.Is(err, iofs.ErrNotExist) {
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
	return ListSSTablesWithFS(dir, vfs.Default())
}

// ListSSTablesWithFS enumerates SSTables on a caller-supplied filesystem
// driver.
//
// It reads the directory rather than calling filepath.Glob, because Glob goes
// straight to the real filesystem and a driver cannot fail it. A listing that
// cannot fail is a listing whose error path is untested.
func ListSSTablesWithFS(dir string, fs vfs.FileSystem) ([][]*SSTable, error) {
	if fs == nil {
		fs = vfs.Default()
	}
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list SSTable files: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// The pattern is a constant, so Match cannot return an error here —
		// but an ignored error is an ignored error, and the linter is right to
		// say so. Treating a match failure as "not an SSTable" is also the
		// behaviour we want if the pattern is ever made dynamic.
		ok, matchErr := filepath.Match("*.sst", e.Name())
		if matchErr != nil || !ok {
			continue
		}
		files = append(files, filepath.Join(dir, e.Name()))
	}
	sort.Strings(files)

	// Group by level. This function returns partial results on error —
	// successfully-opened SSTables are passed back via `levels`, so the
	// caller owns them and no cleanup-on-error tracking is needed here.
	levelMap := make(map[int][]*SSTable)
	var errs []error

	for _, path := range files {
		var level, id int
		_, err := fmt.Sscanf(filepath.Base(path), "L%d-%d.sst", &level, &id)
		if err != nil {
			log.Printf("Warning: SSTable file has invalid name format: %s", path)
			continue
		}

		sst, err := OpenSSTableWithFS(path, fs)
		if err != nil {
			errs = append(errs, fmt.Errorf("open %s: %w", path, err))
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

	// Return partial results with error if any files failed to open
	if len(errs) > 0 {
		// %w, not %v: a caller must be able to tell an I/O failure from any
		// other reason a table would not open. Flattening the cause here made
		// errors.Is fail against the underlying error, which the LSM I/O sweep
		// caught — the injected fault appeared in the message text while the
		// error chain was broken.
		return levels, fmt.Errorf("failed to open %d SSTable(s): %w", len(errs), errs[0])
	}

	return levels, nil
}
