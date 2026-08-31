package storage

// The mmap writer builds a DENSE directory sized by the ID span, and until this
// guard nothing bounded it.
//
// FOUND BY A CRASH SWEEP. The github.com/dd0wney/fault session drove a damaged
// snapshot through StorageConfig.FS and then closed the store normally. Close
// calls Snapshot (persistence.go:384), which re-publishes from whatever the
// damaged bytes produced. One state did not return: 99.5% CPU for 18 seconds,
// with the stack in newDirectory's fill loop.
//
// The record area lies outside computeCRC (mmap_snapshot_format.go:185 takes
// the directories and the metadata, and no records or runs). So one flipped
// byte in a node record's ID field passes the checksum, loads cleanly, sorts
// last, and becomes maxNodeID. A dense directory over that span is the spin.
//
// Measured here: newDirectory fills at 3.8 ns/entry, so a 2^32 span is 16.3
// seconds of CPU before the 34 GiB allocation is even resolved. That is the
// reported 18.
//
// The READ path already guards this. checkDirRange (mmap_snapshot_reader.go:67)
// refuses a snapshot whose declared span disagrees with its directory, which is
// why forEachNodeID's O(span) loop is safe on a loaded snapshot. The write path
// had no equivalent: it took nodes[len-1].ID on trust. The span was validated
// coming off the disk and not going back on.
//
// A refusal is safe. persistence.go:408 truncates the WAL ONLY after a snapshot
// that succeeded, so the old snapshot plus the WAL stay the recovery pair. That
// invariant already exists for the damaged-record refusal in #521.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// The predicate, table-driven. Cheap, and it covers the shape the integration
// test below cannot afford to allocate.
func TestCheckDirectorySpan(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count        uint64
		minID, maxID uint64
		wantRefusal  bool
	}{
		{name: "empty", count: 0, minID: 0, maxID: 0},
		{name: "dense and small", count: 3, minID: 1, maxID: 5},
		{name: "dense and huge is fine: a big healthy store is still healthy",
			count: 1 << 30, minID: 1, maxID: 1 << 30},
		{name: "sparse but small stays under the floor",
			count: 2, minID: 1, maxID: 1 << 20},
		{name: "sparse and huge is the corruption shape",
			count: 2, minID: 1, maxID: 1 << 32, wantRefusal: true},
		{name: "one surviving node after four billion IDs",
			count: 1, minID: 1, maxID: 4_000_000_000, wantRefusal: true},
		{name: "inverted range",
			count: 2, minID: 100, maxID: 1, wantRefusal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkDirectorySpan(tc.count, tc.minID, tc.maxID, "node")
			if tc.wantRefusal && err == nil {
				t.Errorf("span %d..%d for %d records was accepted; a dense directory here "+
					"is enormous and almost entirely holes", tc.minID, tc.maxID, tc.count)
			}
			if !tc.wantRefusal && err != nil {
				t.Errorf("span %d..%d for %d records was refused, but this is legitimate "+
					"data and a guard that refuses it is worse than the spin: %v",
					tc.minID, tc.maxID, tc.count, err)
			}
		})
	}
}

// The writer must consult the guard, and must refuse WITHOUT allocating.
//
// The timing assertion is the point. A guard that returns the right error after
// sixteen seconds of filling has not fixed anything.
func TestWriterRefusesAnImplausibleSpanImmediately(t *testing.T) {
	nodes := []*Node{
		{ID: 1, TenantID: "t1"},
		{ID: 1 << 32, TenantID: "t1"}, // what one flipped byte in an ID field produces
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- writeMmapSnapshotDataWithFS(vfs.OS(),
			filepath.Join(t.TempDir(), "snapshot.mmap"), nodes, nil, sampleMeta())
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatalf("the writer accepted a 2^32 node ID span and built a 34 GiB dense directory")
		}
		if !strings.Contains(err.Error(), "refusing to write a snapshot") {
			t.Errorf("the refusal does not use the established wording, so it will not read "+
				"like the damaged-record refusal beside it: %v", err)
		}
		if elapsed > 2*time.Second {
			t.Errorf("the writer refused, but only after %v; it filled the directory before "+
				"deciding, which is the defect", elapsed)
		}
		t.Logf("refused in %v: %v", elapsed, err)
	case <-time.After(20 * time.Second):
		t.Fatal("the writer did not return within 20s: this is the reported spin")
	}
}

// A healthy store must still write. The control on the guard: without it, a
// test suite full of refusals looks identical to a correct one.
func TestWriterStillWritesAHealthySnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	if err := writeMmapSnapshotDataWithFS(vfs.OS(), path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("the guard refused a healthy snapshot: %v", err)
	}
	snap, err := openMmapSnapshot(path)
	if err != nil {
		t.Fatalf("the written snapshot does not reopen: %v", err)
	}
	defer func() { _ = snap.close() }()
	if got, want := snap.nodeCount(), len(sampleNodes()); got != want {
		t.Errorf("nodeCount = %d, want %d", got, want)
	}
}
