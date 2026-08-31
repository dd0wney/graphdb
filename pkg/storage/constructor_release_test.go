package storage

// A resource acquired must be released, on every path out.
//
// NewGraphStorageWithConfig opens a WAL, an EdgeStore and an mmap mapping, and
// every error return after that abandoned them. The caller receives no store on
// a failure, so it has nothing to Close and the resources are unreclaimable for
// the life of the process. CERT FIO42-C, and the shape that rule predicts:
// violated on the error paths, because the happy path closes.
//
// Except the happy path did not close either. Close's switch had a case for
// batchedWAL and a case for wal and none for compressedWAL, and
// CompressedWAL.Close was called nowhere in this package. Every store opened
// with EnableCompression leaked its WAL handle on every ORDINARY shutdown.
//
// Reported by the github.com/dd0wney/fault session, which measured the reach of
// the constructor half across injected failures: 4 leaking points on an
// ordinary open, 6 with disk-backed edges, and 17 on the encryption-toggle
// path, which does the most work after the WAL opens.
//
// It matters most on Windows, where an open handle blocks removal of the
// directory holding it. Linux tests stay green with the leak in place.
//
// ON THE INSTRUMENT. The first version of this file counted "open" and "close"
// in a vfstest trace. That is wrong, and it was wrong in the direction that
// invents defects: an Open that FAILS is traced and produces no handle and so
// no Close, so a fresh store looking for an absent snapshot reads as a leak.
// It reported an imbalance against already-fixed code. A handle tracker is the
// only correct instrument here, because the question is about handles and not
// about calls.

import (
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// trackingFS records the handles Open actually returned and Close took back.
// A count alone can fail a sweep and cannot help fix one, so this keeps names.
type trackingFS struct {
	vfs.FileSystem
	mu   sync.Mutex
	open map[*trackedFile]string
}

type trackedFile struct {
	vfs.File
	owner *trackingFS
}

func newTrackingFS(base vfs.FileSystem) *trackingFS {
	return &trackingFS{FileSystem: base, open: make(map[*trackedFile]string)}
}

func (t *trackingFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f, err := t.FileSystem.Open(name, flag, perm)
	if err != nil {
		return nil, err // no handle was created, so there is nothing to track
	}
	tf := &trackedFile{File: f, owner: t}
	t.mu.Lock()
	t.open[tf] = name
	t.mu.Unlock()
	return tf, nil
}

// Close removes the handle even when the underlying Close fails. The descriptor
// is gone either way, which is the same reason vfstest's CrashFS releases it.
func (f *trackedFile) Close() error {
	f.owner.mu.Lock()
	delete(f.owner.open, f)
	f.owner.mu.Unlock()
	return f.File.Close()
}

func (t *trackingFS) outstanding() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := make([]string, 0, len(t.open))
	for _, n := range t.open {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// opened reports how many handles were ever handed out, so a zero-outstanding
// result cannot be confused with a driver that was never consulted.
type countingFS struct {
	*trackingFS
	total int
}

func (c *countingFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f, err := c.trackingFS.Open(name, flag, perm)
	if err == nil {
		c.total++
	}
	return f, err
}

// The happy path, with compression on. No fault and no corruption: open a
// store, write to it, close it.
func TestCloseReleasesTheCompressedWAL(t *testing.T) {
	fs := &countingFS{trackingFS: newTrackingFS(vfs.OS())}

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:           t.TempDir(),
		EnableCompression: true,
		FS:                fs,
	})
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if _, err := gs.CreateNode([]string{"A"}, nil); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}

	// The control. Zero outstanding means nothing if nothing was ever opened.
	if fs.total == 0 {
		t.Fatalf("the driver handed out no handle at all, so it is not installed and a " +
			"clean result here says nothing")
	}
	if left := fs.outstanding(); len(left) > 0 {
		t.Errorf("%d of %d handles are still open after an ordinary Close: %v",
			len(left), fs.total, left)
	}
}

// The error paths. Sweep an injected Open failure across the constructor;
// whenever construction fails the caller holds no store, so every handle the
// constructor took must already be closed.
func TestConstructorReleasesWhatItAcquired(t *testing.T) {
	failed := 0
	leaks := map[int][]string{}

	for n := 1; n <= 40; n++ {
		tracker := newTrackingFS(vfs.OS())
		fs := &failNthOpenFS{FileSystem: tracker, n: n}

		gs, err := NewGraphStorageWithConfig(StorageConfig{DataDir: t.TempDir(), FS: fs})
		if err == nil {
			_ = gs.Close()
			continue
		}
		failed++
		if left := tracker.outstanding(); len(left) > 0 {
			leaks[n] = left
		}
	}

	// The control. A sweep that never entered an error path proves nothing, and
	// reports the same clean result as one that entered every path.
	if failed == 0 {
		t.Fatalf("no injected failure made the constructor fail, so this sweep never " +
			"reached an error path")
	}
	if len(leaks) > 0 {
		t.Errorf("the constructor returned an error and left handles open at %d of %d "+
			"failing points: %v\nthe caller has no store, so nothing can ever close them",
			len(leaks), failed, leaks)
	}
	t.Logf("%d of 40 injected failures made the constructor fail, %d leaked", failed, len(leaks))
}

// failNthOpenFS fails the n-th Open. Deliberately not vfstest.FaultFS: this
// wraps the tracker so the tracker sees the real outcome of every call.
type failNthOpenFS struct {
	vfs.FileSystem
	n, seen int
}

func (f *failNthOpenFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f.seen++
	if f.seen == f.n {
		return nil, os.ErrPermission
	}
	return f.FileSystem.Open(name, flag, perm)
}
