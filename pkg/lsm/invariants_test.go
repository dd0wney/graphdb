package lsm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newInvariantStore(t *testing.T) *LSMStorage {
	t.Helper()
	opts := DefaultLSMOptions(t.TempDir())
	opts.EnableAutoCompaction = false
	l, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return l
}

func containsSubstring(violations []string, want string) bool {
	for _, v := range violations {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}

func TestCheckInvariantsCleanStore(t *testing.T) {
	l := newInvariantStore(t)
	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("a healthy store reported %d violations: %v", len(violations), violations)
	}
}

// I1, the direction that matters for a failed compaction cleanup: a file on
// the disk that no level knows about is what it leaves behind, and a reopen
// loads it beside the tables that replaced it.
func TestCheckInvariantsDetectsAnOrphanFileOnDisk(t *testing.T) {
	l := newInvariantStore(t)

	orphan := filepath.Join(l.dataDir, "L0-999999.sst")
	if err := os.WriteFile(orphan, []byte("not a real sstable"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "L0-999999.sst") {
		t.Errorf("an orphan SSTable on the disk was not reported: %v", violations)
	}
}

// I1, the other direction: a level holding a table whose file is gone.
func TestCheckInvariantsDetectsAMissingFile(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.RLock()
	path := l.levels[0][0].path
	l.mu.RUnlock()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, filepath.Base(path)) {
		t.Errorf("a level holding a table with no file was not reported: %v", violations)
	}
}

// I3: the file name records the level, and ListSSTables rebuilds the levels
// from it. A table held elsewhere changes level across a reopen, silently.
func TestCheckInvariantsDetectsAWrongLevel(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	moved := l.levels[0][0]
	l.levels[0] = nil
	l.levels = append(l.levels, []*SSTable{moved})
	l.mu.Unlock()

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "level") {
		t.Errorf("a table filed under the wrong level was not reported: %v", violations)
	}
}

// I2: the same table in two places.
func TestCheckInvariantsDetectsADuplicate(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	dup := l.levels[0][0]
	l.levels[0] = append(l.levels[0], dup)
	l.mu.Unlock()

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "twice") {
		t.Errorf("a duplicated SSTable was not reported: %v", violations)
	}
}

// A nil entry in a level must be reported, not dereferenced.
//
// No production path produces one: ListSSTables skips a table it cannot open,
// flush appends the table it just wrote, and compact copies the slices it was
// given. The check is here because CheckInvariants exists to be run on a store
// somebody already suspects, and a checker that panics on the corruption it
// was called to find is worse than no checker.
//
// The nil is removed before the test ends. LSMStorage.Close dereferences every
// entry in every level, so leaving it in place crashes the cleanup and the
// failure would be reported against Close rather than against this check.
func TestCheckInvariantsDetectsANilTable(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	original := l.levels[0]
	l.levels[0] = append(append([]*SSTable{}, original...), nil)
	l.mu.Unlock()

	violations, err := CheckInvariants(l)

	l.mu.Lock()
	l.levels[0] = original
	l.mu.Unlock()

	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "nil") {
		t.Errorf("a nil SSTable was not reported: %v", violations)
	}
}

// I4 states as an invariant what an interrupted flush leaves behind. flush
// moves the memtable into immutableTable and clears it once the SSTable is
// written; still set at quiescence means it never got there, and every later
// flush takes the "already flushing" arm — including the one inside Sync.
func TestCheckInvariantsDetectsAStrandedImmutableTable(t *testing.T) {
	l := newInvariantStore(t)

	l.mu.Lock()
	stranded := NewMemTable(1024)
	_ = stranded.Put([]byte("stranded"), []byte("x"))
	l.immutableTable = stranded
	l.mu.Unlock()

	violations, err := CheckInvariants(l)

	l.mu.Lock()
	l.immutableTable = nil
	l.mu.Unlock()

	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "immutable") {
		t.Errorf("a stranded immutable memtable was not reported: %v", violations)
	}
}

// Data waiting for its first flush is the ordinary state of an LSM store, and
// it must not be reported. Without this the checker would cry on every store
// that had been written to.
func TestCheckInvariantsAcceptsAnUnflushedMemtable(t *testing.T) {
	l := newInvariantStore(t)

	if err := l.Put([]byte("unflushed"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("a store with unflushed data reported %v", violations)
	}
}

// I5 is the block cache, the row of the coupling analysis with no evidence of
// any kind. A cached value that no level holds is a reader serving something
// the store does not contain.
func TestCheckInvariantsDetectsAStaleCacheEntry(t *testing.T) {
	l := newInvariantStore(t)

	if _, ok := l.Get([]byte("k")); !ok {
		t.Fatal("the key is missing, so the test cannot prove anything")
	}
	l.cache.Put("k", []byte("a value no level holds"))

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "cache") {
		t.Errorf("a stale cache entry was not reported: %v", violations)
	}
}

// A cached key that no level holds at all, which is the shape a delete that
// failed to invalidate would leave.
func TestCheckInvariantsDetectsACachedKeyThatDoesNotExist(t *testing.T) {
	l := newInvariantStore(t)

	l.cache.Put("ghost", []byte("v"))

	violations, err := CheckInvariants(l)
	if err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if !containsSubstring(violations, "ghost") {
		t.Errorf("a cached key held by no level was not reported: %v", violations)
	}
}

// The checker must not repair what it inspects. lookupEntry exists so the I5
// comparison neither reads the cache nor writes to it; a checker that
// populated the cache would silently fix the disagreement it was called to
// find, and report a clean store on the second run.
func TestCheckInvariantsDoesNotWriteToTheCache(t *testing.T) {
	l := newInvariantStore(t)

	before := len(l.cache.Snapshot())
	if _, err := CheckInvariants(l); err != nil {
		t.Fatalf("CheckInvariants: %v", err)
	}
	if after := len(l.cache.Snapshot()); after != before {
		t.Errorf("the cache grew from %d to %d entries during a check", before, after)
	}
}
