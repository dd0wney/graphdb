package storage

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

// The snapshot CRC covers the header, the directories and the metadata blob —
// NOT record bodies (mmap_snapshot_format.go, "bounds-checked record reading").
// So a record damaged by bit rot, a partial write or a truncated copy survives
// open and reaches a decoder. Until this test, that record read as a MISSING
// node, and a query returned an incomplete result with nothing to signal it.
//
// SQLite's equivalent cannot happen: sqlite3_malloc failure returns
// SQLITE_NOMEM and a malformed page returns SQLITE_CORRUPT, neither of which
// can be mistaken for an empty result (sqlite.org/malloc.html).
func TestDamagedRecordIsNotReportedAsMissing(t *testing.T) {
	dir := t.TempDir()

	gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	target, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	survivor, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("beta")})
	if err != nil {
		t.Fatalf("create survivor: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := mmapSnapshotPath(dir)

	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	off, ok := snap.nodeOffset(target.ID)
	if !ok {
		t.Fatalf("node %d has no directory entry", target.ID)
	}
	_ = snap.close()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Byte 8 of a node record is the tenant-length prefix. 0xFFFF asks for more
	// bytes than the file holds, so the bounds check refuses the record. The
	// CRC does not cover this byte, so the file still opens.
	binary.LittleEndian.PutUint16(raw[off+8:], 0xFFFF)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// POSITIVE CONTROL on the corruption. Without it, a test that stopped
	// corrupting anything would still pass.
	if _, decErr := decodeNodeRecordAt(raw, off); decErr == nil {
		t.Fatalf("corruption did not take: record at %d still decodes", off)
	}

	gs2, err := NewGraphStorageWithConfig(mmapConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs2.Close() }()

	_, err = gs2.GetNodeForTenant(target.ID, "")
	if err == nil {
		t.Fatal("a damaged record must not read as a successful lookup")
	}
	if errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a damaged record was reported as missing: %v", err)
	}
	if !errors.Is(err, ErrRecordUnreadable) {
		t.Fatalf("want ErrRecordUnreadable, got %v", err)
	}

	// NEGATIVE CONTROL: the undamaged neighbour still reads. Without this the
	// test would pass just as well if reopen had failed entirely.
	if _, err := gs2.GetNodeForTenant(survivor.ID, ""); err != nil {
		t.Fatalf("the undamaged node must still read: %v", err)
	}

	// An ID that was never in the directory is still plain absence.
	if _, err := gs2.GetNodeForTenant(survivor.ID+9999, ""); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("a genuinely missing node must stay ErrNodeNotFound, got %v", err)
	}
}
