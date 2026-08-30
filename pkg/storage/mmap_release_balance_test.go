package storage

// mmap_release_balance_test.go proves that every error path in
// openMmapSnapshotWithFS releases the mapping it acquired.
//
// vfs.MapFile hands the reader a []byte plus a release func. The Go garbage
// collector does not manage that mapping, so a return that skips release
// leaks address space until the process exits. openMmapSnapshotWithFS
// (pkg/storage/mmap_snapshot_reader.go) has eight `_ = release()` call sites,
// one per error path between a successful Map and a successful open. This
// file drives each one with a payload malformed for EXACTLY that check, using
// vfstest.MapCounter to count Map calls against release calls, and fails if
// they ever disagree.
//
// Each case also asserts on a substring of the returned error, so a
// corruption that trips the WRONG check is caught as a test-authoring
// mistake rather than silently reported as covering a path it did not reach.
// The parent test additionally fails if fewer than all eight named cases
// completed — see the "reached N of 8" assertion at the bottom.
//
// See docs/internals/design (PR body) for two mutation-testing runs that
// prove this file is a gate: removing a release() call at two different
// sites makes the corresponding case fail with an unbalanced count.

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// validMmapSnapshotBytes builds one small, valid snapshot with the repo's
// shared fixtures (sampleNodes/sampleEdges/sampleMeta, also used by
// mmap_oom_test.go and vfs_driver_test.go) and returns its raw bytes. Every
// case below corrupts a fresh copy of these bytes in memory; no corrupt file
// is ever written to disk.
func validMmapSnapshotBytes(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("seed a valid snapshot: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if len(data) <= mmapHeaderSize {
		t.Fatalf("fixture is too small to corrupt meaningfully: %d bytes", len(data))
	}
	return data
}

// corruptHeaderOnly returns a copy of base with mutate's edits to the header
// applied, and the CRC left stale.
//
// Use this ONLY for a corruption meant to fail sections() — the reader checks
// section layout before it ever compares the CRC (mmap_snapshot_reader.go),
// so the stale CRC is never reached on that path.
func corruptHeaderOnly(t *testing.T, base []byte, mutate func(hdr *mmapSnapshotHeader)) []byte {
	t.Helper()
	hdr, err := unmarshalMmapHeader(base)
	if err != nil {
		t.Fatalf("parse header of the valid fixture: %v", err)
	}
	mutate(hdr)
	out := make([]byte, len(base))
	copy(out, base)
	copy(out[:mmapHeaderSize], hdr.marshal())
	return out
}

// corruptWithFixedCRC returns a copy of base with corrupt's edits applied to
// the header and/or the body, then the CRC recomputed over the result so the
// corruption survives the CRC gate and reaches a LATER validation step.
//
// It calls the production sections() function on the mutated header to learn
// the byte ranges openMmapSnapshotWithFS will independently compute when it
// parses the returned bytes for real, so the recomputed CRC covers exactly
// what production covers. If corrupt makes sections() itself fail, that is a
// test-authoring mistake for this helper — use corruptHeaderOnly instead —
// so this fails loudly rather than silently proving nothing.
func corruptWithFixedCRC(t *testing.T, base []byte, corrupt func(out []byte, hdr *mmapSnapshotHeader)) []byte {
	t.Helper()
	hdr, err := unmarshalMmapHeader(base)
	if err != nil {
		t.Fatalf("parse header of the valid fixture: %v", err)
	}
	out := make([]byte, len(base))
	copy(out, base)
	corrupt(out, hdr)
	copy(out[:mmapHeaderSize], hdr.marshal())

	nodeDir, edgeDir, adjDir, membDir, meta, err := sections(out, hdr)
	if err != nil {
		t.Fatalf("corruption broke sections() (%v); use corruptHeaderOnly for a sections-failure case", err)
	}
	hdr.crc = computeCRC(out[:hCRC], nodeDir, edgeDir, adjDir, membDir, meta)
	copy(out[:mmapHeaderSize], hdr.marshal())
	return out
}

// releasePathCase targets one `_ = release()` call site in
// openMmapSnapshotWithFS (mmap_snapshot_reader.go). line documents which one,
// as of this writing — it is not asserted against; wantErrPart is, and that
// is what actually proves the intended path fired.
type releasePathCase struct {
	name        string
	line        int
	build       func(t *testing.T, base []byte) []byte
	wantErrPart string
}

var releasePathCases = []releasePathCase{
	{
		name: "too_small",
		line: 41,
		build: func(t *testing.T, base []byte) []byte {
			return base[:mmapHeaderSize-1]
		},
		wantErrPart: "too small",
	},
	{
		name: "bad_header",
		line: 47,
		build: func(t *testing.T, base []byte) []byte {
			out := make([]byte, len(base))
			copy(out, base)
			out[0] ^= 0xFF // corrupt the magic byte; unmarshalMmapHeader rejects it
			return out
		},
		wantErrPart: "bad magic",
	},
	{
		name: "sections_out_of_order",
		line: 53,
		build: func(t *testing.T, base []byte) []byte {
			return corruptHeaderOnly(t, base, func(hdr *mmapSnapshotHeader) {
				// Must be >= mmapHeaderSize; sections() rejects this before the
				// CRC comparison ever runs, so the stale CRC does not matter.
				hdr.outCSROffset = 0
			})
		},
		wantErrPart: "CSR sections out of order",
	},
	{
		name: "crc_mismatch",
		line: 57,
		build: func(t *testing.T, base []byte) []byte {
			out := make([]byte, len(base))
			copy(out, base)
			out[hFlags] ^= 0xFF // a byte the CRC covers but no structural check inspects
			return out
		},
		wantErrPart: "CRC mismatch",
	},
	{
		name: "node_id_range_inverted",
		line: 68,
		build: func(t *testing.T, base []byte) []byte {
			return corruptWithFixedCRC(t, base, func(out []byte, hdr *mmapSnapshotHeader) {
				// minID = maxID+1 wraps mmapSnapshotHeader.nodeDirLen() to exactly
				// 0 (maxNodeID - minNodeID + 1 underflows and wraps), so sections()
				// still accepts an empty node directory slice and only
				// checkDirRange's own "maxID < minID" test catches the inversion.
				hdr.minNodeID = hdr.maxNodeID + 1
			})
		},
		wantErrPart: "node ID range inverted",
	},
	{
		name: "edge_id_range_inverted",
		line: 72,
		build: func(t *testing.T, base []byte) []byte {
			return corruptWithFixedCRC(t, base, func(out []byte, hdr *mmapSnapshotHeader) {
				hdr.minEdgeID = hdr.maxEdgeID + 1 // same wrap, on the edge directory
			})
		},
		wantErrPart: "edge ID range inverted",
	},
	{
		name: "metadata_unmarshal_fails",
		line: 78,
		build: func(t *testing.T, base []byte) []byte {
			return corruptWithFixedCRC(t, base, func(out []byte, hdr *mmapSnapshotHeader) {
				for i := hdr.metaOffset; i < hdr.metaOffset+hdr.metaLen; i++ {
					out[i] = 0xFF // no longer valid JSON, same length so sections() is unaffected
				}
			})
		},
		wantErrPart: "mmap snapshot metadata:",
	},
	{
		name: "membership_dir_parse_fails",
		line: 83,
		build: func(t *testing.T, base []byte) []byte {
			return corruptWithFixedCRC(t, base, func(out []byte, hdr *mmapSnapshotHeader) {
				// The directory's own entry count, claiming far more entries than
				// its buffer could ever hold.
				binary.LittleEndian.PutUint32(out[hdr.membDirOffset:], 0xFFFFFFFF)
			})
		},
		wantErrPart: "membership directory entry count",
	},
}

// TestOpenMmapSnapshotWithFS_ReleaseBalanceAcrossErrorPaths drives each of the
// eight release() call sites in openMmapSnapshotWithFS and asserts the
// mapping was released exactly once every time.
func TestOpenMmapSnapshotWithFS_ReleaseBalanceAcrossErrorPaths(t *testing.T) {
	base := validMmapSnapshotBytes(t)
	reached := make(map[string]bool, len(releasePathCases))

	for _, tc := range releasePathCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := tc.build(t, base)

			counter := vfstest.NewMapCounter(vfs.OS())
			counter.ServePayload(payload)

			_, err := openMmapSnapshotWithFS(counter, "in-memory-snapshot")

			if err == nil {
				t.Fatalf("line %d: expected an error, got none (maps=%d releases=%d)",
					tc.line, counter.Maps(), counter.Releases())
			}
			if !strings.Contains(err.Error(), tc.wantErrPart) {
				t.Fatalf("line %d: error %q does not confirm this is the intended path (want a message containing %q)",
					tc.line, err.Error(), tc.wantErrPart)
			}
			if counter.Maps() != 1 {
				t.Fatalf("line %d: Map was called %d times, want exactly 1", tc.line, counter.Maps())
			}
			if !counter.Balanced() {
				t.Fatalf("line %d LEAKED the mapping: %d Map call(s), %d release(s)",
					tc.line, counter.Maps(), counter.Releases())
			}
			t.Logf("reached mmap_snapshot_reader.go:%d (release balanced, maps=%d releases=%d): %v",
				tc.line, counter.Maps(), counter.Releases(), err)
			reached[tc.name] = true
		})
	}

	if len(reached) != len(releasePathCases) {
		var missed []string
		for _, tc := range releasePathCases {
			if !reached[tc.name] {
				missed = append(missed, tc.name)
			}
		}
		t.Fatalf("reached %d of %d release() paths; missed: %v", len(reached), len(releasePathCases), missed)
	}
	t.Logf("reached all %d of %d release() paths in openMmapSnapshotWithFS", len(reached), len(releasePathCases))
}
