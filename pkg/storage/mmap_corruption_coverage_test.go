package storage

// Where does the mmap snapshot detect damage, and where does it not?
//
// mmap_snapshot_format.go states the design in a comment:
//
//	Integrity: a CRC32 over the header (excluding the CRC field) + all three
//	directories (node, edge, adjacency) + the metadata blob protects the
//	structural index — the parts read at open. Record bytes are paged in
//	lazily and bounds-checked at decode.
//
// The first half was gated. The second half was a promise with nothing behind
// it: no test corrupted a record body, so "bounds-checked at decode" was an
// assertion about the code rather than a measurement of it. A snapshot file is
// customer-data-equivalent, so the difference matters.
//
// This file measures both halves and pins the boundary between them:
//
//	structural index   damage is refused at OPEN, by the CRC
//	record bytes       open succeeds, and damage surfaces at DECODE, per record
//
// The second row is the interesting one. It says damage is CONTAINED: a broken
// record reports itself and its neighbours still read. The alternative — a torn
// record quietly yielding a plausible wrong node — is the failure this
// repository refuses, and it would look identical to success.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// seedSnapshotBytes writes a valid snapshot and returns its bytes.
func seedSnapshotBytes(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("seed a valid snapshot: %v", err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	return good
}

// nodeRecordByte returns a byte offset inside the node RECORD region, which the
// CRC deliberately does not cover.
//
// The region is [mmapHeaderSize, nodeDirOffset). Both bounds come from the
// format, so this stays correct when the fixture changes size — which is
// exactly what the midpoint arithmetic in vfs_driver_test.go did not.
func nodeRecordByte(t *testing.T, snapshot []byte) uint64 {
	t.Helper()
	nodeDirOffset := binary.LittleEndian.Uint64(snapshot[hNodeDir:])
	if nodeDirOffset <= mmapHeaderSize {
		t.Fatalf("the node record region is empty (header ends at %d, node directory "+
			"starts at %d), so there is no record byte to corrupt and this test measures "+
			"nothing", mmapHeaderSize, nodeDirOffset)
	}
	return (mmapHeaderSize + nodeDirOffset) / 2
}

// Damage to a record body must NOT stop the snapshot opening. The CRC covers
// the structural index only, on purpose: records are paged in lazily, so
// hashing them at open would give back the cheap reopen the format exists for.
//
// It must also not go unnoticed. Open succeeds, the damaged record reports an
// error when it is read, and every other record still decodes.
func TestMmapRecordDamageIsCaughtAtDecodeAndIsContained(t *testing.T) {
	good := seedSnapshotBytes(t)
	pos := nodeRecordByte(t, good)

	bad := make([]byte, len(good))
	copy(bad, good)
	bad[pos] ^= 0xFF

	snap, err := openMmapSnapshotWithFS(corruptMapper{FileSystem: vfs.OS(), payload: bad}, "served-from-memory")
	if err != nil {
		t.Fatalf("a snapshot with a damaged RECORD byte failed to open: %v.\n"+
			"The CRC is not supposed to cover record bytes — if it now does, the cheap "+
			"reopen this format exists for is paying to hash every record.", err)
	}
	defer func() { _ = snap.close() }()

	var broken, intact int
	for _, seeded := range sampleNodes() {
		node, err := snap.getNode(seeded.ID)
		switch {
		case err != nil:
			broken++
		case node == nil:
			t.Errorf("getNode(%d) returned no node and no error, which a caller cannot "+
				"tell from an absent node", seeded.ID)
		default:
			intact++
		}
	}

	// The gate. A decode that returned a plausible wrong node instead of an
	// error would leave broken at zero, and a caller would act on data that is
	// not what was written.
	if broken == 0 {
		t.Errorf("flipping byte %d inside the node record region produced no decode error "+
			"on any of the %d seeded nodes. Either the damage was silently accepted, or "+
			"the byte did not land in a record after all.", pos, len(sampleNodes()))
	}
	// Containment. One torn record must not take the others with it.
	if intact == 0 {
		t.Errorf("every node failed to decode after one damaged byte at %d; damage in one "+
			"record is not contained to that record", pos)
	}
}

// The structural index is the other half: damage there IS refused at open,
// because the CRC covers it.
//
// vfs_driver_test.go asserts this through a Mapper-capable driver. This asserts
// the same rule from the format's side, and names the region it corrupts, so a
// change to what computeCRC hashes fails a test that says which part moved.
//
// Only the node-directory case is a CRC gate. Measured 2026-09-07: with the CRC
// comparison disabled, the header case still fails, because a damaged version
// field is refused by the version check first. That case is kept — an unopenable
// snapshot is the right outcome either way — but do not read it as evidence
// about the CRC.
func TestMmapStructuralIndexDamageIsRefusedAtOpen(t *testing.T) {
	good := seedSnapshotBytes(t)

	nodeDirOffset := binary.LittleEndian.Uint64(good[hNodeDir:])
	if nodeDirOffset+8 > uint64(len(good)) {
		t.Fatalf("the node directory does not fit in a %d-byte snapshot", len(good))
	}

	for _, tc := range []struct {
		region string
		pos    uint64
	}{
		{"header", hVersion + 1},
		{"node directory", nodeDirOffset + 3},
	} {
		bad := make([]byte, len(good))
		copy(bad, good)
		bad[tc.pos] ^= 0xFF

		snap, err := openMmapSnapshotWithFS(corruptMapper{FileSystem: vfs.OS(), payload: bad}, "served-from-memory")
		if err == nil {
			_ = snap.close()
			t.Errorf("a snapshot with a damaged byte in the %s (offset %d) opened without "+
				"complaint; the CRC is supposed to cover that region", tc.region, tc.pos)
		}
	}
}
