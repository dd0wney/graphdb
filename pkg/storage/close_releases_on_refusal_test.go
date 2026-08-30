package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// walBytes reports the total size of the WAL directory. A truncate empties it,
// so the number is the observable for "the WAL survived".
func walBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	walDir := filepath.Join(dir, "wal")
	entries, err := os.ReadDir(walDir)
	if err != nil {
		t.Fatalf("read wal dir %q: %v", walDir, err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatalf("stat %q: %v", e.Name(), err)
		}
		total += info.Size()
	}
	return total
}

// A Close that refuses to snapshot must still release what it holds.
//
// Close sets `closed` with CompareAndSwap and then calls Snapshot. Before this
// fix a Snapshot error returned immediately, so Close never reached the mmap
// unmap, the EdgeStore close, or the WAL close. Munmap is not managed by the
// garbage collector, and `closed` is already true, so a second Close answers
// "storage already closed" and cleans nothing. The mapping and the file handle
// were unreachable for the life of the process.
//
// PR #521 made that path reachable from ordinary bit rot rather than only from
// a full disk, which is what turns a latent leak into a live one.
//
// The two assertions are a pair, and the second is the more important:
//
//   - the mapping must be released, which is the leak;
//   - the WAL must NOT be truncated, which is what makes refusing to snapshot
//     safe in the first place. The old snapshot plus the WAL still reconstruct
//     the store on the next open. A "fix" that released the mapping by running
//     the whole tail of Close would truncate the WAL after a failed snapshot
//     and destroy exactly the data the refusal was protecting.
func TestCloseReleasesResourcesWhenSnapshotRefuses(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if _, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("beta")}); err != nil {
		t.Fatalf("create survivor: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("first close must succeed, nothing is damaged yet: %v", err)
	}

	damageNodeRecordOnDisk(t, mmapSnapshotPath(dir), target.ID)

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	// A write after reopen puts an entry in the WAL, so "the WAL survived"
	// is a claim about content and not about an empty file.
	if _, err := gs2.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("gamma")}); err != nil {
		t.Fatalf("create gamma: %v", err)
	}

	before := walBytes(t, dir)
	if before == 0 {
		t.Fatalf("the WAL is empty before Close, so this test cannot tell a truncate from a no-op")
	}

	closeErr := gs2.Close()
	if closeErr == nil {
		t.Fatalf("Close must refuse: node %d is damaged and Snapshot cannot write it", target.ID)
	}

	if gs2.mmapSnap != nil {
		t.Errorf("LEAK: Close refused the snapshot and returned before releasing the mapping. "+
			"mmapSnap is still set, so Munmap never ran, and `closed` is already true so no "+
			"second Close can clean up. The address space is held until the process exits. "+
			"(Close returned: %v)", closeErr)
	}

	after := walBytes(t, dir)
	if after < before {
		t.Errorf("the WAL was truncated after a REFUSED snapshot: %d bytes before Close, %d after. "+
			"The refusal exists to keep the old snapshot and the WAL as the recovery pair. "+
			"Truncating here destroys the data the refusal was protecting", before, after)
	}
}
