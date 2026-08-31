package storage

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Malformed-database testing, scorecard row 11.
//
// The CRC covers the header, the directories and the metadata. It does NOT
// cover the record area. That split decides what each corruption target can
// reach:
//
//   - FuzzMmapSnapshotCorruption flips one byte anywhere. A byte in the record
//     area gets through the guard and reaches the decoders. A byte in a
//     directory or a count is caught by the CRC and never does.
//   - FuzzMmapSnapshotTruncation measures the open guard and almost never gets
//     past it: 1 of 209 sampled lengths opened.
//
// So nothing exercised the case that actually breaks lazy readers — a file
// whose DIRECTORIES disagree with its data. A directory entry pointing past the
// records, a node count larger than the records hold, a membership run whose
// length exceeds its section: each is a plausible consequence of a partial
// write, and each is caught today only because the CRC happens to notice.
// Remove that accident and the decoder is on its own.
//
// This target removes it. It mutates a byte anywhere, then RECOMPUTES the
// checksum so the file passes validation regardless, and runs the read paths
// against the result.
//
// The contract is unchanged and absolute: refuse the file or serve it, never
// panic, never read out of bounds.

// repairCRC recomputes the checksum over whatever structure the mutated bytes
// now describe, so the open guard admits the file.
//
// It reports false when the mutation broke the section bounds themselves. Those
// inputs are the open guard's job and FuzzMmapSnapshotCorruption already covers
// them; repairing a file whose sections cannot be located is not possible and
// not the point.
func repairCRC(b []byte) bool {
	if len(b) < mmapHeaderSize {
		return false
	}
	hdr, err := unmarshalMmapHeader(b)
	if err != nil {
		return false
	}
	nodeDir, edgeDir, adjDir, membDir, meta, err := sections(b, hdr)
	if err != nil {
		return false
	}
	binary.LittleEndian.PutUint32(b[hCRC:], computeCRC(b[:hCRC], nodeDir, edgeDir, adjDir, membDir, meta))
	return true
}

func FuzzMmapSnapshotCRCRepaired(f *testing.F) {
	base := validSnapshotBytes(f)

	// Seeds aim at the fields the CRC otherwise protects: the counts and the
	// directory offsets, which are what a decoder trusts.
	for _, off := range []int{hNodeCount, hEdgeCount, hMaxNodeID, hNodeDir, hEdgeDir, hAdjDir, hMembDir, hMembDirLen, hMetaLen} {
		f.Add(off, byte(0xFF))
		f.Add(off, byte(0x00))
	}
	f.Add(mmapHeaderSize+1, byte(0xFF)) // into the record area, for contrast

	f.Fuzz(func(t *testing.T, offset int, val byte) {
		if len(base) == 0 {
			t.Skip("no corpus")
		}
		if offset < 0 {
			offset = -offset
		}
		offset %= len(base)

		mutated := make([]byte, len(base))
		copy(mutated, base)
		mutated[offset] = val
		if !repairCRC(mutated) {
			// The mutation broke the section bounds. That is the open guard's
			// case, not this target's.
			return
		}

		dir := t.TempDir()
		path := filepath.Join(dir, "snapshot.mmap")
		if err := os.WriteFile(path, mutated, 0o600); err != nil {
			t.Skip("could not stage the file")
		}

		snap, err := openMmapSnapshot(path)
		if err != nil {
			return
		}
		defer func() { _ = snap.close() }()

		_ = exerciseSnapshot(snap)
	})
}

// TestCRCRepairedTargetReachesTheReadPaths is a gate on the TARGET, not on the
// code under test.
//
// Row 11 of the scorecard was wrong about its own coverage until someone
// counted: FuzzMmapSnapshotTruncation opened 1 of 209 inputs, so a passing run
// said almost nothing about the decoders. A fuzz target that cannot get past
// the front door is indistinguishable from one that found no bugs.
//
// This asserts the new target does get past it. If a future change to the open
// guard starts rejecting repaired files, this fails and says so, rather than
// the target quietly going hollow the way the truncation one did.
func TestCRCRepairedTargetReachesTheReadPaths(t *testing.T) {
	base := validSnapshotBytes(t)
	if len(base) == 0 {
		t.Skip("no corpus")
	}

	const samples = 200
	opened, repaired := 0, 0
	for i := 0; i < samples; i++ {
		mutated := make([]byte, len(base))
		copy(mutated, base)
		off := (i * 7919) % len(base) // spread across the file, deterministically
		mutated[off] ^= 0xFF
		if !repairCRC(mutated) {
			continue
		}
		repaired++

		path := filepath.Join(t.TempDir(), "snapshot.mmap")
		if err := os.WriteFile(path, mutated, 0o600); err != nil {
			t.Fatalf("stage: %v", err)
		}
		snap, err := openMmapSnapshot(path)
		if err != nil {
			continue
		}
		opened++
		_ = exerciseSnapshot(snap)
		_ = snap.close()
	}

	t.Logf("%d/%d mutations repaired to a loadable checksum, %d of those opened and ran the read paths",
		repaired, samples, opened)

	// The truncation target managed 1 of 209. Anything in that region means
	// this target is decoration too.
	if opened < samples/10 {
		t.Fatalf("only %d of %d mutations reached the read paths; this target is near-vacuous, like the truncation one it replaces", opened, samples)
	}
}

// TestExerciseSnapshotReachesMembershipContains is a gate on the TARGET, like
// TestCRCRepairedTargetReachesTheReadPaths above, at function grain.
//
// FuzzMmapSnapshotCRCRepaired exists to manufacture one specific state: bytes
// damaged, checksum recomputed so the file still loads. The run bytes lie
// outside computeCRC (mmap_snapshot_format.go:185 takes the directories and the
// metadata, and no records or runs), so that state is exactly a damaged
// membership run behind a valid CRC.
//
// membershipContains is the function that state corrupts. tenantOwnsUnreadableNode
// calls it to decide tenant membership, and node_operations.go:405 already says
// in words that a damaged run "can return true". The target built the state and
// then never asked the question — it reached the door and turned around.
//
// Reported by the github.com/dd0wney/fault session, which went looking for this
// gap before building a harness to cover it and found the harness already here.
func TestExerciseSnapshotReachesMembershipContains(t *testing.T) {
	base := validSnapshotBytes(t)
	if len(base) == 0 {
		t.Skip("no corpus")
	}
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := os.WriteFile(path, base, 0o600); err != nil {
		t.Fatalf("stage: %v", err)
	}
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("the untouched corpus snapshot must open, or this gate proves nothing: %v", err)
	}
	defer func() { _ = snap.close() }()

	counts := exerciseSnapshot(snap)

	// The control. If the fixture carries no membership runs at all then a zero
	// below means "nothing to reach", not "reached nothing", and the gate would
	// be the vacuous kind it exists to catch.
	if counts.membershipRuns == 0 {
		t.Fatalf("the fixture has no membership runs, so this gate cannot distinguish " +
			"an unreached function from an absent one")
	}
	if counts.membershipContains == 0 {
		t.Errorf("exerciseSnapshot drove %d membership runs and never called "+
			"membershipContains, so both corruption targets manufacture a damaged run "+
			"behind a valid CRC and never ask the tenant question it corrupts",
			counts.membershipRuns)
	}
}
