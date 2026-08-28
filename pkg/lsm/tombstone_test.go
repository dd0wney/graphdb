package lsm

import "testing"

// newTombstoneStore returns a store with the background workers off, so a
// flush happens only where the test asks for one.
func newTombstoneStore(t *testing.T) *LSMStorage {
	t.Helper()
	opts := DefaultLSMOptions(t.TempDir())
	opts.EnableAutoCompaction = false
	l, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// A tombstone in the memtable must hide a value that has already been flushed
// into an SSTable. Before this test, Delete had no effect on a flushed key:
// MemTable.Get returns (nil, false) for a tombstone, which lsm.Get cannot tell
// from "absent", so it fell through to the SSTable and returned the old value.
func TestGetTombstoneMasksFlushedValue(t *testing.T) {
	l := newTombstoneStore(t)

	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil { // forces the memtable into an SSTable
		t.Fatalf("Sync: %v", err)
	}
	if _, ok := l.Get([]byte("k")); !ok {
		t.Fatal("the key vanished across the flush, so the test cannot prove anything")
	}
	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if v, ok := l.Get([]byte("k")); ok {
		t.Errorf("Get returned %q for a deleted key, want absent", v)
	}
}

// The same, with the tombstone itself flushed into a newer SSTable.
func TestGetFlushedTombstoneMasksOlderValue(t *testing.T) {
	l := newTombstoneStore(t)

	if err := l.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := l.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := l.Sync(); err != nil { // the tombstone reaches its own SSTable
		t.Fatalf("Sync: %v", err)
	}

	if v, ok := l.Get([]byte("k")); ok {
		t.Errorf("Get returned %q for a deleted key, want absent", v)
	}
}
