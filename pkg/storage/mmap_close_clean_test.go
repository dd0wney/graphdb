package storage

import (
	"crypto/sha256"
	"os"
	"testing"
	"time"
)

// snapshotFingerprint is the on-disk identity of snapshot.mmap: its mtime and
// a hash of its bytes. Two fields because each catches what the other misses —
// a rewrite with identical bytes still moves the mtime, and a same-second
// rewrite with different bytes still changes the hash.
type snapshotFingerprint struct {
	mtime time.Time
	sum   [32]byte
}

func snapshotFileIdentity(t *testing.T, dir string) snapshotFingerprint {
	t.Helper()
	path := mmapSnapshotPath(dir)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return snapshotFingerprint{mtime: fi.ModTime(), sum: sha256.Sum256(b)}
}

// openMmapWithBase opens dir in mmap mode and fails the test unless the store
// took the mmap path — the behaviour under test only exists on that path.
func openMmapWithBase(t *testing.T, dir string) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if gs.mmapSnap == nil {
		t.Fatal("open did not take the mmap path")
	}
	return gs
}

// writeMmapFixture builds the reopen fixture, closes it so snapshot.mmap
// exists, and returns the fingerprints of the file and of the data.
func writeMmapFixture(t *testing.T, dir string) (snapshotFingerprint, fingerprint) {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	buildReopenFixture(t, gs)
	want := fingerprintTenant(t, gs, rtTenantA)
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return snapshotFileIdentity(t, dir), want
}

// A session that only reads must leave snapshot.mmap byte-for-byte and
// mtime-for-mtime as it found it. Before the fix, Close rewrote the whole
// file on every exit: on the 2.0M-node ICIJ corpus that was 8.3 s and 7.9 GB
// for a read-only consumer, and a read-only consumer rewriting the
// customer-data file at all is the wrong shape (COI_SCREEN_REAL_CORPUS_2026-09-13.md).
func TestMmapClose_ReadOnlySessionLeavesSnapshotUntouched(t *testing.T) {
	dir := t.TempDir()
	before, wantData := writeMmapFixture(t, dir)

	gs := openMmapWithBase(t, dir)
	// Reads of every kind the consumer makes: point lookup, label enumeration,
	// adjacency, and a query-timed path.
	if _, err := gs.GetNodeForTenant(wantData.personIDs[0], rtTenantA); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := gs.GetNodesByLabelForTenant(rtTenantA, "Person"); err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	got := fingerprintTenant(t, gs, rtTenantA)
	assertFingerprintEqual(t, wantData, got, "read-only session")
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	after := snapshotFileIdentity(t, dir)
	if after.sum != before.sum {
		t.Errorf("read-only Close rewrote snapshot.mmap: bytes changed")
	}
	if !after.mtime.Equal(before.mtime) {
		t.Errorf("read-only Close rewrote snapshot.mmap: mtime %v -> %v", before.mtime, after.mtime)
	}

	// The data must still be there for the next session.
	gs2 := openMmapWithBase(t, dir)
	defer gs2.Close()
	assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs2, rtTenantA), "after read-only close")
}

// One write is enough to require the rewrite: the next open must see it.
func TestMmapClose_SessionWithOneWriteRewritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	before, _ := writeMmapFixture(t, dir)

	gs := openMmapWithBase(t, dir)
	n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("late")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := n.ID
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	after := snapshotFileIdentity(t, dir)
	if after.sum == before.sum {
		t.Fatal("Close after a write left snapshot.mmap unchanged")
	}

	gs2 := openMmapWithBase(t, dir)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(id, rtTenantA); err != nil {
		t.Fatalf("the written node did not survive reopen: %v", err)
	}
}

// A delete leaves the overlay empty and lands only in the tombstone set. It
// must still force the rewrite, or the next open resurrects the node.
func TestMmapClose_SessionWithOnlyADeleteRewritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	before, wantData := writeMmapFixture(t, dir)

	gs := openMmapWithBase(t, dir)
	victim := wantData.personIDs[0]
	if err := gs.DeleteNodeForTenant(victim, rtTenantA); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if snapshotFileIdentity(t, dir).sum == before.sum {
		t.Fatal("Close after a delete left snapshot.mmap unchanged")
	}
	gs2 := openMmapWithBase(t, dir)
	defer gs2.Close()
	if _, err := gs2.GetNodeForTenant(victim, rtTenantA); err != ErrNodeNotFound {
		t.Fatalf("deleted node came back after reopen: err=%v", err)
	}
}

// An index definition changes only the metadata tail. It must still force
// the rewrite, or the index is gone at the next open.
func TestMmapClose_SessionWithOnlyAnIndexDefinitionRewritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	before, _ := writeMmapFixture(t, dir)

	gs := openMmapWithBase(t, dir)
	if err := gs.CreatePropertyIndex("name", TypeString); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if snapshotFileIdentity(t, dir).sum == before.sum {
		t.Fatal("Close after an index definition left snapshot.mmap unchanged")
	}
	gs2 := openMmapWithBase(t, dir)
	defer gs2.Close()
	if _, ok := gs2.propertyIndexes["name"]; !ok {
		t.Fatal("the index definition did not survive reopen")
	}
}

// The file can vanish under a running store. A clean session must then
// write, not skip, or the next open finds nothing.
func TestMmapClose_ReadOnlySessionWritesWhenFileIsGone(t *testing.T) {
	dir := t.TempDir()
	_, wantData := writeMmapFixture(t, dir)

	gs := openMmapWithBase(t, dir)
	if err := os.Remove(mmapSnapshotPath(dir)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(mmapSnapshotPath(dir)); err != nil {
		t.Fatalf("Close skipped the write and left no snapshot on disk: %v", err)
	}
	gs2 := openMmapWithBase(t, dir)
	defer gs2.Close()
	assertFingerprintEqual(t, wantData, fingerprintTenant(t, gs2, rtTenantA), "after file removal")
}
