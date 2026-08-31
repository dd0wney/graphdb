package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// SQLite's malformed-database testing corrupts a well-formed file "by some
// means other than SQLite", then requires that the library reports the error
// "without overflowing buffers, dereferencing NULL pointers, or performing
// other unwholesome actions" (sqlite.org/testing.html §11).
//
// graphdb has the same exposure and less of the defence. snapshot.mmap is
// customer-data-equivalent, it is the DEFAULT reopen path since #447, and its
// CRC covers the header, the directories and the metadata blob — NOT the node
// and edge record bodies. A record corrupted in place therefore survives open
// and is parsed lazily on first read, far from any validation.
//
// The contract this target pins: for ANY byte-level corruption, the reader
// either refuses the file at open or serves it without panicking. Go turns an
// out-of-range slice into a panic rather than a buffer overrun, so a panic here
// is the availability bug rather than a memory-safety one — still a crash on
// untrusted-by-then data.
//
// FuzzMmapReopenParity next door tests something different and weaker: that a
// VALID mmap store enumerates identically to JSON. It never presents a damaged
// file.

var (
	corpusOnce  sync.Once
	corpusBytes []byte
)

// validSnapshotBytes builds a small store once, closes it so snapshot.mmap is
// written, and returns the file's bytes for the fuzzer to damage.
func validSnapshotBytes(tb testing.TB) []byte {
	corpusOnce.Do(func() {
		dir, err := os.MkdirTemp("", "mmapfuzz")
		if err != nil {
			return
		}
		defer func() { _ = os.RemoveAll(dir) }()

		gs, err := NewGraphStorageWithConfig(mmapConfig(dir))
		if err != nil {
			return
		}
		a, err := gs.CreateNode([]string{"Thing"}, map[string]Value{"name": StringValue("alpha")})
		if err != nil {
			return
		}
		b, err := gs.CreateNode([]string{"Thing", "Other"}, map[string]Value{"name": StringValue("beta")})
		if err != nil {
			return
		}
		if _, err := gs.CreateEdge(a.ID, b.ID, "LINKS", map[string]Value{"w": StringValue("x")}, 1.0); err != nil {
			return
		}
		if err := gs.Close(); err != nil {
			return
		}
		corpusBytes, _ = os.ReadFile(mmapSnapshotPath(dir))
	})
	if len(corpusBytes) == 0 {
		tb.Skip("could not build a valid snapshot to corrupt")
	}
	return corpusBytes
}

// exerciseSnapshot drives every read path a served store would reach. The
// assertion is the absence of a panic; results are deliberately unchecked,
// because a corrupted file is allowed to yield wrong data, just not a crash.
// exerciseCounts records which read paths exerciseSnapshot actually drove.
//
// A fuzz target that reaches a decoder and a fuzz target that stops at the
// front door report identically: both pass. Row 11 of the scorecard was wrong
// about its own coverage until someone counted, and
// TestCRCRepairedTargetReachesTheReadPaths exists because of it. This is the
// same instrument at function grain — returned rather than kept in a package
// variable, because the fuzz engine runs these concurrently.
type exerciseCounts struct {
	membershipRuns     int
	membershipContains int
}

func exerciseSnapshot(snap *mmapSnapshot) exerciseCounts {
	var counts exerciseCounts
	_ = snap.nodeCount()
	_ = snap.edgeCount()
	_, _ = snap.nodeIDRange()
	_, _ = snap.edgeIDRange()

	snap.forEachNodeID(func(id uint64, _ int64) {
		_, _ = snap.getNode(id)
		_ = snap.outgoingCSR(id)
		_ = snap.incomingCSR(id)
		_, _, _, _, _ = snap.adjDirEntry(id)
	})
	snap.forEachEdgeID(func(id uint64, _ int64) {
		_, _ = snap.getEdge(id)
	})

	// Probe IDs for membershipContains. The range bounds and a midpoint drive
	// the binary search to its first, last and middle steps rather than one
	// path, and 0 and MaxUint64 sit outside any legitimate run. On a corrupt
	// snapshot the reported range is itself nonsense, which is the point — the
	// guard clauses in membershipContains are what this exercises.
	nodeMin, nodeMax := snap.nodeIDRange()
	edgeMin, edgeMax := snap.edgeIDRange()
	probeIDs := []uint64{0, nodeMin, nodeMax, edgeMin, edgeMax, ^uint64(0)}
	if nodeMax >= nodeMin {
		probeIDs = append(probeIDs, nodeMin+(nodeMax-nodeMin)/2)
	}
	if edgeMax >= edgeMin {
		probeIDs = append(probeIDs, edgeMin+(edgeMax-edgeMin)/2)
	}

	for _, tenant := range snap.tenantList() {
		for _, kind := range []byte{membKindNodeTenant, membKindNodeLabel, membKindEdgeTenant, membKindEdgeType} {
			for _, name := range snap.membershipKeys(kind, tenant) {
				_ = snap.membershipRun(kind, tenant, name)
				counts.membershipRuns++
				// The question a damaged run corrupts. membershipRun copies the
				// whole run and membershipContains binary-searches the mapped
				// bytes in place, so they are different code with different
				// bounds arithmetic; driving one is not driving the other.
				for _, id := range probeIDs {
					_ = snap.membershipContains(kind, tenant, name, id)
					counts.membershipContains++
				}
			}
		}
	}
	_ = snap.metadata()
	return counts
}

// FuzzMmapSnapshotCorruption flips one byte of a valid snapshot and requires
// that the reader neither panics at open nor panics while serving.
func FuzzMmapSnapshotCorruption(f *testing.F) {
	base := validSnapshotBytes(f)

	// Seeds aimed at each structural region rather than at random offsets: the
	// magic, the version, the counts, the section offsets, the CRC itself, and
	// a byte deep in the record area that the CRC does not cover.
	f.Add(0, byte(0x00))                // magic
	f.Add(4, byte(0xFF))                // version
	f.Add(hNodeCount, byte(0xFF))       // a count field
	f.Add(hMembDir, byte(0xFF))         // a section offset
	f.Add(hCRC, byte(0xFF))             // the CRC itself
	f.Add(mmapHeaderSize+8, byte(0xFF)) // first record bytes
	f.Add(len(base)-1, byte(0xFF))      // last byte

	f.Fuzz(func(t *testing.T, offset int, val byte) {
		if len(base) == 0 {
			t.Skip("no corpus")
		}
		if offset < 0 {
			offset = -offset
		}
		offset %= len(base)

		corrupted := make([]byte, len(base))
		copy(corrupted, base)
		corrupted[offset] = val

		dir := t.TempDir()
		path := filepath.Join(dir, "snapshot.mmap")
		if err := os.WriteFile(path, corrupted, 0o600); err != nil {
			t.Skip("could not stage the corrupted file")
		}

		snap, err := openMmapSnapshot(path)
		if err != nil {
			// A clean refusal is the good outcome, and the common one for a
			// byte the CRC covers.
			return
		}
		defer func() { _ = snap.close() }()

		_ = exerciseSnapshot(snap)
	})
}

// FuzzMmapSnapshotTruncation covers the corruption shape that actually happens
// in the field. A single flipped byte models bit rot; a short file models the
// far more common case — a write interrupted by a crash, a full disk, or a copy
// that stopped early.
//
// The contract is the same: refuse the file or serve it, never panic.
//
// MEASURED COVERAGE, because this target is weaker than it looks. Sampling 209
// truncation lengths across a 1040-byte snapshot, exactly ONE opened
// successfully — the untruncated one. Every other length is rejected at open,
// because any truncation breaks either the section bounds or the CRC over the
// directories and metadata.
//
// So this target verifies the OPEN GUARD and almost never reaches the read
// paths. That is a genuine result about the open path being well defended, and
// it is not read-path coverage. Do not read a passing run here as evidence that
// the decoders handle short input; FuzzMmapSnapshotCorruption is what exercises
// those, because a flipped byte in the record area does not disturb the CRC.
//
// To reach the read paths with a short file, a future variant would have to
// recompute the CRC after truncating so the file passes validation.
func FuzzMmapSnapshotTruncation(f *testing.F) {
	base := validSnapshotBytes(f)

	f.Add(0)                   // empty
	f.Add(1)                   // shorter than the magic
	f.Add(mmapHeaderSize - 1)  // one byte short of a header
	f.Add(mmapHeaderSize)      // header only, no sections
	f.Add(mmapHeaderSize + 16) // into the record area
	f.Add(len(base) - 1)       // one byte short of whole
	f.Add(len(base) / 2)       // mid-file

	f.Fuzz(func(t *testing.T, n int) {
		if len(base) == 0 {
			t.Skip("no corpus")
		}
		if n < 0 {
			n = -n
		}
		n %= len(base) + 1

		dir := t.TempDir()
		path := filepath.Join(dir, "snapshot.mmap")
		if err := os.WriteFile(path, base[:n], 0o600); err != nil {
			t.Skip("could not stage the truncated file")
		}

		snap, err := openMmapSnapshot(path)
		if err != nil {
			return
		}
		defer func() { _ = snap.close() }()

		_ = exerciseSnapshot(snap)
	})
}
