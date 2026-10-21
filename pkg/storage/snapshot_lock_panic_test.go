package storage

import "testing"

// A panic while the snapshot capture holds gs.mu.RLock must release it. The
// JSON path and the mmap path each have their own capture function, so each
// gets a test: a lock left held hangs every later write and Close.

func TestLockPanic_SnapshotCaptureJSON(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(jsonConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	n, err := gs.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(1)})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if gs.useMmapSnapshot {
		t.Fatal("store is in mmap mode; the test needs JSON mode")
	}
	assertPanicReleasesLock(t, gs, "captureJSONSnapshot",
		func() { _ = gs.Snapshot() },
		updateAgain(gs, n))
}

func TestLockPanic_SnapshotCaptureMmap(t *testing.T) {
	dir := t.TempDir()
	first, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	n, err := first.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(1)})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Close writes snapshot.mmap, so the reopen below maps a base file and the
	// capture walks real base records.
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if gs.mmapSnap == nil {
		t.Fatal("reopened store has no mmap base; the test would not cover the mmap walk")
	}
	got, err := gs.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	assertPanicReleasesLock(t, gs, "captureMmapSnapshot",
		func() { _ = gs.Snapshot() },
		updateAgain(gs, got))
}
