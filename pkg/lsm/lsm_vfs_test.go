package lsm

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// pkg/lsm had no way to make a disk fail before this. Every fault, crash and
// sweep facility graphdb built reached pkg/wal and stopped, while the SSTables
// holding the data were untestable for I/O failure.

func TestLSM_OnFaultDriver_CreateSurfacesWriteFailure(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "lsm-write-fault")
	path := filepath.Join(t.TempDir(), "table.sst")

	entries := []*Entry{
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
	}

	fs.FailWrite(vfstest.Always, 0)
	if _, err := NewSSTableWithFS(path, entries, fs); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("NewSSTableWithFS error = %v, want the injected fault", err)
	}
	if fs.Writes() == 0 {
		t.Error("no write reached the driver; the assertion above proved nothing")
	}
}

func TestLSM_OnFaultDriver_OpenSurfacesFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.sst")

	// Write a real table first, on the plain driver.
	if _, err := NewSSTable(path, []*Entry{{Key: []byte("k"), Value: []byte("v")}}); err != nil {
		t.Fatalf("NewSSTable: %v", err)
	}

	fs := vfstest.NewFaults(vfs.OS(), "lsm-open-fault")
	fs.FailOpen(vfstest.Always)
	if _, err := OpenSSTableWithFS(path, fs); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("OpenSSTableWithFS error = %v, want the injected fault", err)
	}
}

// TestLSM_ListSSTablesSurfacesADirectoryFailure is why the driver interface
// grew a ReadDir method. ListSSTables used filepath.Glob, which goes straight
// to the real filesystem — so the error path for "cannot enumerate the tables"
// could not be reached by any test.
func TestLSM_ListSSTablesSurfacesADirectoryFailure(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewSSTable(SSTablePath(dir, 0, 1), []*Entry{{Key: []byte("k"), Value: []byte("v")}}); err != nil {
		t.Fatalf("NewSSTable: %v", err)
	}

	// Sanity first: the listing works on the plain driver.
	levels, err := ListSSTables(dir)
	if err != nil {
		t.Fatalf("ListSSTables on a healthy directory: %v", err)
	}
	found := 0
	for _, lvl := range levels {
		found += len(lvl)
	}
	if found == 0 {
		t.Fatal("the healthy listing found no tables; the failure case below would prove nothing")
	}

	fs := vfstest.NewFaults(vfs.OS(), "lsm-readdir-fault")
	fs.FailNthOp(1) // the listing is the first operation
	if _, err := ListSSTablesWithFS(dir, fs); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("ListSSTablesWithFS error = %v, want the injected fault", err)
	}
	if !fs.Fired() {
		t.Error("the fault never fired; the listing did not go through the driver")
	}
}

// TestLSM_SSTableCarriesItsDriver pins the field that was missing when this
// migration was first written: the constructors returned an SSTable with a nil
// fs, so the first Close or Remove would have panicked rather than failed.
func TestLSM_SSTableCarriesItsDriver(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "lsm-carry")
	path := filepath.Join(t.TempDir(), "carried.sst")

	sst, err := NewSSTableWithFS(path, []*Entry{{Key: []byte("k"), Value: []byte("v")}}, fs)
	if err != nil {
		t.Fatalf("NewSSTableWithFS: %v", err)
	}
	if err := sst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Remove goes through sst.fs. A nil driver panics here instead.
	if err := sst.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := vfs.Default().Stat(path); err == nil {
		t.Error("the table is still on disk after Delete")
	}
}

// TestLSM_SweepEveryIOFailurePoint walks a failure through every operation an
// LSM open-and-list cycle performs.
func TestLSM_SweepEveryIOFailurePoint(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewSSTable(SSTablePath(dir, 0, 1), []*Entry{{Key: []byte("k"), Value: []byte("v")}}); err != nil {
		t.Fatalf("seed table: %v", err)
	}

	run := func(fs vfs.FileSystem) error {
		_, err := ListSSTablesWithFS(dir, fs)
		return err
	}

	ops, err := vfstest.SweepCount(run)
	if err != nil {
		t.Fatalf("counting run: %v", err)
	}
	t.Logf("an LSM listing performs %d I/O operations", ops)

	vfstest.Sweep(t, 64, run, func(t *testing.T, n int, runErr error) {
		// The contract is modest and worth stating: a failure anywhere in the
		// listing must be reported, never swallowed into a partial result that
		// a caller would mistake for the whole set.
		if runErr == nil {
			return
		}
		if !errors.Is(runErr, vfstest.ErrInjected) {
			t.Fatalf("N=%d: error = %v, want the injected fault to be reported", n, runErr)
		}
	})
}
