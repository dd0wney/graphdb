// Package storagetest gives tests in OTHER packages the fault-injection step
// that pkg/storage's own tests perform with unexported helpers.
//
// pkg/storage/damaged_record_test.go damages a record through mmapConfig,
// mmapSnapshotPath, openMmapSnapshot and (*mmapSnapshot).nodeOffset. Every one
// of those is unexported, and a test in pkg/api cannot call any of them. Go
// gives a subpackage no access to its parent's unexported identifiers, so
// living under pkg/storage/ buys nothing on its own; this package reaches the
// same bytes by reading the snapshot's own header and directory.
//
// The shape follows pkg/vfs/vfstest and pkg/alloc/alloctest: a test-support
// package beside the package it supports, importable by anyone.
//
// The format knowledge below is a SECOND copy of what mmap_snapshot_format.go
// defines, which is the price of the package boundary. Four checks keep that
// copy honest, and all four must stay:
//
//   - the magic must read "GMNP", so a file that is not an mmap snapshot is
//     refused instead of misread;
//   - the version check refuses any snapshot that is not v4, so a format bump
//     stops this package loudly instead of corrupting the wrong byte;
//   - the record at the offset the directory names must start with the ID
//     that was asked for;
//   - a positive control checks the corruption itself, before it is written:
//     the file must actually be too short to satisfy the claimed tenant
//     length.
//
// Without the record-ID check a wrong offset would damage some other part of
// the file, the target record would still decode, and every caller's test
// would pass while injecting no fault at all.
//
// The exported entry points take a testing.TB and stop the test. The work
// happens in an error-returning function underneath, so damage_test.go can
// drive the refusal paths and prove this injector reports a failure instead of
// corrupting the wrong thing.
package storagetest

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Header field byte offsets, from mmap_snapshot_format.go. Only the fields
// this package reads are listed.
const (
	hMagic     = 0
	hVersion   = 4
	hNodeCount = 12
	hEdgeCount = 20
	hMinNodeID = 28
	hMaxNodeID = 36
	hMinEdgeID = 44
	hMaxEdgeID = 52
	hNodeDir   = 60
	hEdgeDir   = 68

	mmapHeaderSize = 148

	// wantVersion is the only snapshot version whose header layout this
	// package knows. A bump must be reflected here deliberately.
	wantVersion = 4

	// dirAbsent marks a directory slot with no record.
	dirAbsent = int64(-1)

	// tenantLenOffset is the byte offset, inside a node OR an edge record, of
	// the uint16 tenant-length prefix. Both records start with the ID as a
	// uint64 and then that prefix (encodeNodeRecord / encodeEdgeRecord).
	tenantLenOffset = 8

	// damagedTenantLen asks for more bytes than the fixture file holds, so the
	// decoder's bounds check refuses the record. The snapshot CRC does not
	// cover record bodies, so the file still opens.
	damagedTenantLen = 0xFFFF
)

// recordKind selects the header fields that describe one record section.
type recordKind struct {
	name     string
	hCount   int
	hMinID   int
	hMaxID   int
	hDirOffs int
}

var (
	nodeKind = recordKind{name: "node", hCount: hNodeCount, hMinID: hMinNodeID, hMaxID: hMaxNodeID, hDirOffs: hNodeDir}
	edgeKind = recordKind{name: "edge", hCount: hEdgeCount, hMinID: hMinEdgeID, hMaxID: hMaxEdgeID, hDirOffs: hEdgeDir}
)

// SnapshotPath is the mmap snapshot inside a data directory. A caller that
// must stat, copy or restore the file needs the same answer mmapSnapshotPath
// gives.
func SnapshotPath(dataDir string) string {
	return filepath.Join(dataDir, "snapshot.mmap")
}

// DamageNodeRecord makes the node record for id undecodable, in place, in the
// mmap snapshot under dataDir.
//
// The store must be CLOSED before the call and reopened after it. The damage
// lands on the record body, which the CRC does not cover, so the snapshot
// still opens and the damaged record reaches a decoder.
func DamageNodeRecord(tb testing.TB, dataDir string, id uint64) {
	tb.Helper()
	if err := damageRecord(dataDir, id, nodeKind); err != nil {
		tb.Fatalf("damage node %d: %v", id, err)
	}
}

// DamageEdgeRecord is DamageNodeRecord for an edge record.
func DamageEdgeRecord(tb testing.TB, dataDir string, id uint64) {
	tb.Helper()
	if err := damageRecord(dataDir, id, edgeKind); err != nil {
		tb.Fatalf("damage edge %d: %v", id, err)
	}
}

func damageRecord(dataDir string, id uint64, kind recordKind) error {
	path := SnapshotPath(dataDir)
	//nolint:gosec // G304: test support. The path is SnapshotPath of a directory the calling test created.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot %s: %w", path, err)
	}
	if err := checkHeader(raw, path); err != nil {
		return err
	}

	off, err := recordOffset(raw, id, kind)
	if err != nil {
		return err
	}

	// The offset must name the record that was asked for. This check stops a
	// wrong offset from damaging an unrelated byte while every caller's test
	// keeps passing.
	if got := binary.LittleEndian.Uint64(raw[off:]); got != id {
		return fmt.Errorf("the directory sends %s %d to offset %d, where the record ID reads %d: "+
			"the offset is wrong and the damage would land somewhere else", kind.name, id, off, got)
	}

	// POSITIVE CONTROL on the corruption, before it is written. A tenant
	// length the file cannot satisfy is refused by the decoder's bounds check.
	// Assert that the file really is too short for it; on a large snapshot it
	// would not be, and the record would decode a garbage tenant instead.
	tenantStart := off + tenantLenOffset + 2
	if remaining := int64(len(raw)) - tenantStart; remaining >= damagedTenantLen {
		return fmt.Errorf("%s %d at offset %d has %d bytes after its tenant prefix, so a claimed "+
			"length of %d would still decode: this injector needs a smaller fixture or another byte",
			kind.name, id, off, remaining, damagedTenantLen)
	}

	binary.LittleEndian.PutUint16(raw[off+tenantLenOffset:], damagedTenantLen)
	//nolint:gosec // G703: same path, same directory, and the bytes are the ones just read back.
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write snapshot %s: %w", path, err)
	}
	return nil
}

func checkHeader(raw []byte, path string) error {
	if len(raw) < mmapHeaderSize {
		return fmt.Errorf("snapshot %s is %d bytes, shorter than the %d-byte header",
			path, len(raw), mmapHeaderSize)
	}
	if got := string(raw[hMagic : hMagic+4]); got != "GMNP" {
		return fmt.Errorf("snapshot %s carries magic %q, want \"GMNP\": is the store in mmap mode?", path, got)
	}
	if v := binary.LittleEndian.Uint32(raw[hVersion:]); v != wantVersion {
		return fmt.Errorf("snapshot %s is format version %d, and this package knows only version %d: "+
			"update storagetest with the new header layout", path, v, wantVersion)
	}
	return nil
}

// recordOffset walks the snapshot's dense directory, as
// (*mmapSnapshot).nodeOffset does, and returns the record's absolute offset.
func recordOffset(raw []byte, id uint64, kind recordKind) (int64, error) {
	count := binary.LittleEndian.Uint64(raw[kind.hCount:])
	minID := binary.LittleEndian.Uint64(raw[kind.hMinID:])
	maxID := binary.LittleEndian.Uint64(raw[kind.hMaxID:])
	dir := binary.LittleEndian.Uint64(raw[kind.hDirOffs:])

	if count == 0 {
		return 0, fmt.Errorf("the snapshot holds no %s records", kind.name)
	}
	if id < minID || id > maxID {
		return 0, fmt.Errorf("%s %d is outside the snapshot's ID range [%d, %d]: "+
			"was the store closed after the record was created?", kind.name, id, minID, maxID)
	}

	entry := dir + (id-minID)*8
	if entry+8 > uint64(len(raw)) {
		return 0, fmt.Errorf("the %s directory entry for %d sits at %d, past the end of a %d-byte file",
			kind.name, id, entry, len(raw))
	}
	off := int64(binary.LittleEndian.Uint64(raw[entry:]))
	if off == dirAbsent {
		return 0, fmt.Errorf("%s %d has no directory entry in the snapshot", kind.name, id)
	}
	if off < mmapHeaderSize || off+tenantLenOffset+2 > int64(len(raw)) {
		return 0, fmt.Errorf("%s %d points at offset %d, outside the record area of a %d-byte file",
			kind.name, id, off, len(raw))
	}
	return off, nil
}
