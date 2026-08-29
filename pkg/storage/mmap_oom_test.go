package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc/alloctest"
)

// Out-of-memory testing for the snapshot read path, scorecard row 4.
//
// Row 4 was ◐ because only pkg/wal's record buffer was gated. Snapshot
// assembly was not, so nothing could ask the question SQLite asks of every
// allocation: when this one fails, does the library degrade safely, or does it
// crash, leak, or answer wrongly?
//
// The property under test is SQLite's, and it is about SAFETY, not liveness:
// a refused allocation may cost you the answer, but it must never give you the
// WRONG answer, and it must never take the process down.

// snapshotOnDisk writes a valid snapshot and returns its path.
func snapshotOnDisk(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("seed a snapshot: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	return path
}

// readAll opens the snapshot and returns every node it could decode, keyed by
// ID, plus a count of the IDs the directory listed but the decoder refused.
func readAll(path string) (map[uint64]*Node, int, error) {
	snap, err := openMmapSnapshot(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = snap.close() }()

	got := make(map[uint64]*Node)
	absent := 0
	snap.forEachNodeID(func(id uint64, _ int64) {
		if n, err := snap.getNode(id); err == nil {
			got[id] = n
		} else {
			absent++
		}
	})
	return got, absent, nil
}

func TestSnapshotReadPathUnderAllocationFailure(t *testing.T) {
	path := snapshotOnDisk(t)

	// Reference decode with no injection. Every later comparison is against
	// this; without it the sweep could pass while returning nothing at all.
	want, absent, err := readAll(path)
	if err != nil {
		t.Fatalf("reference read: %v", err)
	}
	if absent != 0 || len(want) == 0 {
		t.Fatalf("reference read is not clean: %d decoded, %d absent", len(want), absent)
	}

	for _, mode := range []alloctest.Mode{alloctest.FailOnce, alloctest.FailAllFrom} {
		t.Run(mode.String(), func(t *testing.T) {
			worstAbsent := 0
			alloctest.Sweep(t, mode, 512,
				func() error {
					got, missing, err := readAll(path)
					if err != nil {
						return err
					}
					if missing > worstAbsent {
						worstAbsent = missing
					}
					// SAFETY: whatever came back must be RIGHT. A refused
					// allocation may cost a node; it must never produce a
					// different one.
					for id, n := range got {
						ref, ok := want[id]
						if !ok {
							return errUnexpectedNode(id)
						}
						if n.ID != ref.ID || n.TenantID != ref.TenantID || len(n.Labels) != len(ref.Labels) {
							return errWrongNode(id)
						}
						for k, v := range ref.Properties {
							gv, present := n.Properties[k]
							if !present {
								// A property whose value allocation was
								// refused: the record decode fails as a whole,
								// so a half-built node must never surface.
								return errPartialNode(id, k)
							}
							if gv.Type != v.Type || string(gv.Data) != string(v.Data) {
								return errWrongProperty(id, k)
							}
						}
					}
					return nil
				},
				func(t *testing.T, n int, runErr error) {
					if runErr != nil {
						t.Fatalf("%s at allocation %d: %v", mode, n, runErr)
					}
				})
			t.Logf("%s: swept to termination; worst case %d of %d nodes unreadable under refusal",
				mode, worstAbsent, len(want))
		})
	}
}

type nodeErr struct{ msg string }

func (e nodeErr) Error() string { return e.msg }

func errUnexpectedNode(id uint64) error {
	return nodeErr{msg: "decoded a node the reference read did not contain: " + strconv.FormatUint(id, 10)}
}
func errWrongNode(id uint64) error {
	return nodeErr{msg: "node " + strconv.FormatUint(id, 10) + " differs from the reference decode"}
}
func errPartialNode(id uint64, key string) error {
	return nodeErr{msg: "node " + strconv.FormatUint(id, 10) + " surfaced without property " + key + ": a half-built record escaped"}
}
func errWrongProperty(id uint64, key string) error {
	return nodeErr{msg: "node " + strconv.FormatUint(id, 10) + " property " + key + " differs from the reference decode"}
}
