package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// These tests are the point of ADR 0002 stage 4. Before pkg/storage took a
// driver, none of them could be written at all: the only seam was the os
// package, and a test that substitutes os is testing a different program.

// An I/O fault armed on the driver must surface from Snapshot. If any snapshot
// write path went back to calling os directly, the fault would not fire and
// this test would fail — which is what makes it a gate rather than a demo.
func TestSnapshotSurfacesAnInjectedWriteFault(t *testing.T) {
	dir := t.TempDir()
	faults := vfstest.NewFaults(vfs.OS(), "faulty")

	cfg := jsonConfig(dir)
	cfg.FS = faults
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, nil); err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	faults.FailOpen(vfstest.Always)
	err = gs.Snapshot()
	faults.FailOpen(vfstest.Off)

	if err == nil {
		t.Fatal("Snapshot succeeded while every Open was failing: the snapshot write path is not going through the driver")
	}
	// Ops(), not Fired(): Fired() is set only by FailNthOp, per its doc
	// comment. Asserting the driver was actually reached is still the point —
	// without it, an error from anywhere would satisfy this test.
	if faults.Ops() == 0 {
		t.Fatal("the driver saw no operations, so the error came from somewhere else")
	}
}

// walOnlyFaultFS fails Open for the WAL's files and nothing else.
//
// A blanket FailOpen does not test the WAL wiring: construction also reads the
// snapshot through the driver, so the store fails either way. The first version
// of this test used a blanket fault, passed, and kept passing when the WAL was
// reverted to wal.NewWAL — it was a gate for the load path wearing the WAL's
// name. Scoping the fault to the WAL directory is what makes it a real one.
type walOnlyFaultFS struct {
	vfs.FileSystem
	hits int
}

func (w *walOnlyFaultFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	if strings.Contains(filepath.ToSlash(name), "/wal/") {
		w.hits++
		return nil, fmt.Errorf("open %s: %w", name, vfstest.ErrInjected)
	}
	return w.FileSystem.Open(name, flag, perm)
}

// The WAL must be built on the store's driver, not on vfs.Default() — for
// every WAL flavour, not just the default one. pkg/storage chooses between
// three constructors, and each is a separate chance to forget the driver.
// CompressedWAL was in fact the one that was forgotten.
func TestConstructionRoutesTheWALThroughTheDriver(t *testing.T) {
	cases := []struct {
		name string
		tune func(*StorageConfig)
	}{
		{"plain", func(*StorageConfig) {}},
		{"batched", func(c *StorageConfig) { c.EnableBatching = true }},
		{"compressed", func(c *StorageConfig) { c.EnableCompression = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := &walOnlyFaultFS{FileSystem: vfs.OS()}
			cfg := jsonConfig(t.TempDir())
			cfg.FS = fs
			tc.tune(&cfg)

			gs, err := NewGraphStorageWithConfig(cfg)
			if err == nil {
				_ = gs.Close()
				t.Fatalf("construction succeeded while every WAL Open was failing: the %s WAL is not going through the driver", tc.name)
			}
			if fs.hits == 0 {
				t.Fatal("the driver saw no WAL open at all")
			}
			if !errors.Is(err, vfstest.ErrInjected) {
				t.Fatalf("construction failed for some other reason: %v", err)
			}
		})
	}
}

// corruptMapper serves snapshot bytes the test chose. This is the capability
// vfs.Mapper was added for: the production reader parses a corrupt snapshot
// that was never written to disk, so the corruption cannot be an artefact of
// the test's own file handling.
type corruptMapper struct {
	vfs.FileSystem
	payload []byte
}

func (c corruptMapper) Map(string) ([]byte, func() error, error) {
	return c.payload, func() error { return nil }, nil
}

func TestMmapReaderRejectsACorruptSnapshotFromTheDriver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("seed a valid snapshot: %v", err)
	}
	good, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}

	// Control: the untouched bytes must open, or the corruption below proves
	// nothing about corruption.
	if _, err := openMmapSnapshotWithFS(corruptMapper{FileSystem: vfs.OS(), payload: good}, "served-from-memory"); err != nil {
		t.Fatalf("control: a valid snapshot served through a Mapper failed to open: %v", err)
	}

	bad := make([]byte, len(good))
	copy(bad, good)
	bad[len(bad)/2] ^= 0xFF

	_, err = openMmapSnapshotWithFS(corruptMapper{FileSystem: vfs.OS(), payload: bad}, "served-from-memory")
	if err == nil {
		t.Fatal("the reader accepted a corrupt snapshot")
	}
	if !strings.Contains(err.Error(), "CRC") && !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error does not identify the corruption: %v", err)
	}
}

// A driver with no Mapper still serves the reader, through Open. Existing
// drivers were written before the capability existed and must keep working.
func TestMmapReaderWorksWithADriverThatCannotMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.mmap")
	if err := writeMmapSnapshotData(path, sampleNodes(), sampleEdges(), sampleMeta()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	faults := vfstest.NewFaults(vfs.OS(), "no-mapper")
	snap, err := openMmapSnapshotWithFS(faults, path)
	if err != nil {
		t.Fatalf("open through a Mapper-less driver: %v", err)
	}
	defer func() { _ = snap.close() }()
	if got := snap.nodeCount(); got != len(sampleNodes()) {
		t.Fatalf("node count %d, want %d", got, len(sampleNodes()))
	}

	// And that path is faultable, which is the whole reason it matters.
	faults.FailOpen(vfstest.Always)
	if _, err := openMmapSnapshotWithFS(faults, path); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("injected open fault did not reach the reader: %v", err)
	}
}
