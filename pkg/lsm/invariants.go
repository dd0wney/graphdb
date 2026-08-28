package lsm

import (
	"fmt"
	"path/filepath"
)

// CheckInvariants returns one string for each violated invariant. An empty
// slice means healthy. The error return is for a check that could not run at
// all, never for a violation.
//
// It exists for the reason #468 promoted the pkg/storage checker out of the
// test binary: a consumer that suspects a corrupt store must be able to ask,
// and an assertion only a test can make is an assertion the shipped artefact
// does not carry.
//
// What it covers, and what it does not: the level set against the files on the
// disk. It says nothing about the contents of an SSTable, about the bloom
// filters, or about the block cache. A clean result is therefore a statement
// about bookkeeping, not about data.
//
// PRECONDITION: the store is quiescent. Stop the background workers, or call
// this before they start. It takes lsm.mu for reading, which excludes a level
// swap but NOT a flush between its two critical sections.
func CheckInvariants(l *LSMStorage) ([]string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var violations []string
	report := func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	}

	// Ground truth is the directory, read through the store's own driver so a
	// fault driver sees this read too.
	entries, err := l.fs.ReadDir(l.dataDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", l.dataDir, err)
	}
	onDisk := make(map[string]struct{})
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sst" {
			continue
		}
		onDisk[filepath.Join(l.dataDir, e.Name())] = struct{}{}
	}

	inLevels := make(map[string]int)
	for level := range l.levels {
		for _, sst := range l.levels[level] {
			if sst == nil {
				report("levels[%d] holds a nil SSTable", level)
				continue
			}

			// I2: no table twice, in one level or across two.
			if seen, dup := inLevels[sst.path]; dup {
				report("SSTable %s appears twice: level %d and level %d",
					filepath.Base(sst.path), seen, level)
				continue
			}
			inLevels[sst.path] = level

			// I3: the name records the level, and ListSSTables rebuilds the
			// levels from it. A table held elsewhere changes level across a
			// reopen, silently.
			var named, id int
			if _, err := fmt.Sscanf(filepath.Base(sst.path), "L%d-%d.sst", &named, &id); err != nil {
				report("SSTable %s has a name no reopen can parse", filepath.Base(sst.path))
			} else if named != level {
				report("SSTable %s is held at level %d but its name says level %d, so a "+
					"reopen would move it", filepath.Base(sst.path), level, named)
			}

			// I1, one direction.
			if _, ok := onDisk[sst.path]; !ok {
				report("SSTable %s is held at level %d but has no file on the disk",
					filepath.Base(sst.path), level)
			}
		}
	}

	// I1, the other direction. This is what a failed compaction cleanup leaves
	// behind, and a reopen loads it beside its own replacement.
	for path := range onDisk {
		if _, ok := inLevels[path]; !ok {
			report("SSTable %s is on the disk but no level holds it, so a reopen would "+
				"load it beside the tables that replaced it", filepath.Base(path))
		}
	}

	// I4: nothing is left half-flushed at quiescence. flush moves the memtable
	// into immutableTable and clears it when the SSTable is written, so this
	// being set with no flush running means a flush was interrupted. Every
	// later flush then takes the "already flushing" arm, including the one
	// inside Sync.
	//
	// A non-empty memTable is NOT a violation. Data waiting for its first
	// flush is the ordinary state of an LSM store.
	if l.immutableTable != nil {
		report("an immutable memtable of %d bytes is still set at quiescence: a flush was "+
			"interrupted, so every later flush returns early and Sync reports success "+
			"without writing", l.immutableTable.Size())
	}

	// I5: the cache is shared across readers and mirrors the levels. Nothing
	// checked it against them until now.
	for key, cached := range l.cache.Snapshot() {
		entry, ok := l.lookupEntry([]byte(key))
		if !ok || entry.Deleted {
			report("the cache holds key %q, which no memtable or level holds", key)
			continue
		}
		if string(entry.Value) != string(cached) {
			report("the cache holds %q for key %q, and the levels hold %q",
				cached, key, entry.Value)
		}
	}

	return violations, nil
}
