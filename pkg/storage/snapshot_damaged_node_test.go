package storage

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"testing"
)

// A snapshot write must refuse a damaged node record instead of dropping it.
//
// snapshotMmapLocked collects the live node set and writes snapshot.mmap.
// Close() calls Snapshot(). So the first clean shutdown after a record goes
// bad writes a new snapshot, and a dropped record is not in it. The directory
// entry goes with it, and every diagnostic PR #512 added for that record goes
// quiet. The damage becomes silent, permanent data loss.
//
// The edge branch of the same function already refuses. This test holds the
// node branch to the same rule.
//
// The two assertions below are one pair. The first says Snapshot must fail.
// The second says WHY it must fail: against the pre-fix code Snapshot
// succeeded, and the record was gone from the file it wrote.
// CONSUMER CONTRACT: CC11-unreadable-not-missing — all readers (PR B)
func TestSnapshotRefusesADamagedNodeRecord(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	// The survivor makes the data-loss report below readable: it separates
	// "the damaged record was dropped" from "the whole snapshot was empty".
	survivor, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("beta")})
	if err != nil {
		t.Fatalf("create survivor: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)
	off := damageNodeRecordOnDisk(t, path, target.ID)

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	snapErr := gs2.Snapshot()
	if snapErr == nil {
		// THE DATA LOSS ITSELF. Snapshot claimed success, so read back what
		// it actually wrote and report whether the record survived.
		after, openErr := openMmapSnapshot(path)
		if openErr != nil {
			t.Fatalf("Snapshot() returned nil, and the snapshot it wrote will not open: %v", openErr)
		}
		_, targetPresent := after.nodeOffset(target.ID)
		_, survivorPresent := after.nodeOffset(survivor.ID)
		_ = after.close()
		if !targetPresent {
			t.Fatalf("DATA LOSS: Snapshot() returned nil, and node %d has no directory entry "+
				"in the snapshot it wrote (survivor node %d present=%v). "+
				"The damaged record was dropped from the write, not refused.",
				target.ID, survivor.ID, survivorPresent)
		}
		t.Fatal("Snapshot() must refuse a damaged node record, got a nil error")
	}

	// The error must name the node, so an operator can act on it. A bare
	// "the snapshot is damaged" would pass a weaker assertion and tell
	// nobody which record to restore.
	idStr := strconv.FormatUint(target.ID, 10)
	if !strings.Contains(snapErr.Error(), idStr) {
		t.Fatalf("the refusal must name node %d, got: %v", target.ID, snapErr)
	}
	// "node record" pins the message to the node branch. Without it the test
	// would also pass on the edge branch's refusal, which is a different
	// code path and is already covered.
	if !strings.Contains(snapErr.Error(), "node record") {
		t.Fatalf("the refusal must say which kind of record is damaged, got: %v", snapErr)
	}

	// The snapshot on disk must be the ORIGINAL file, damage and all. A
	// refusal that still renamed a partial file over the good one would be
	// the same data loss by another route.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if binary.LittleEndian.Uint16(raw[off+8:]) != 0xFFFF {
		t.Fatal("the refused write replaced the snapshot on disk")
	}
	after, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("the snapshot on disk will not open after the refusal: %v", err)
	}
	if _, ok := after.nodeOffset(target.ID); !ok {
		t.Fatalf("node %d lost its directory entry although the write was refused", target.ID)
	}
	_ = after.close()
}

// POSITIVE CONTROL for TestSnapshotRefusesADamagedNodeRecord. Without it, a
// snapshot writer that refused EVERY store would pass the test above.
func TestSnapshotSucceedsOnAnUndamagedStore(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	kept, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen, so the node is a BASE record and the rewritten node branch is
	// the code that carries it into the next snapshot. A store that never
	// reopened would exercise the shard overlay only.
	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := gs2.Snapshot(); err != nil {
		t.Fatalf("an undamaged store must snapshot: %v", err)
	}
	if err := gs2.Close(); err != nil {
		t.Fatalf("close after snapshot: %v", err)
	}

	// The node must survive the rewrite, read back through a third open.
	gs3, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	defer func() { _ = gs3.Close() }()
	got, err := gs3.GetNodeForTenant(kept.ID, "")
	if err != nil {
		t.Fatalf("node %d did not survive the snapshot rewrite: %v", kept.ID, err)
	}
	name, err := got.Properties["name"].AsString()
	if err != nil || name != "alpha" {
		t.Fatalf("node %d came back changed: name=%q err=%v", kept.ID, name, err)
	}
}

// damageNodeRecordOnDisk corrupts one node record body in snapshot.mmap and
// returns the record offset. Byte 8 of a node record is the tenant-length
// prefix, as in damaged_record_test.go: 0xFFFF asks for more bytes than the
// file holds, so the bounds check refuses the record. The snapshot CRC does
// not cover record bodies, so the file still opens.
func damageNodeRecordOnDisk(t *testing.T, path string, id uint64) int64 {
	t.Helper()

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(id)
	if !ok {
		t.Fatalf("node %d has no directory entry", id)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption. Without it, a test that stopped
	// corrupting anything would still pass.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}
	return off
}
