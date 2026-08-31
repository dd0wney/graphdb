package backup_test

// Gate for threading pkg/vfs through the archive-extract path.
//
// Before this, Extract called os.MkdirAll and os.OpenFile directly: no
// driver observed a single one of its operations. See archive_vfs_test.go's
// package comment for why that matters and what RED-FIRST means here — the
// same reasoning applies, on the write side of the restore.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// ExtractWithFS must write through the supplied driver, not bypass it to the
// os package, and the bytes it writes must be exactly what was archived.
func TestExtractWithFS_WritesThroughTheSuppliedDriver(t *testing.T) {
	srcDir := t.TempDir()
	buildVFSFixture(t, srcDir)
	archive := buildArchiveBytes(t, vfs.OS(), srcDir)

	destDir := t.TempDir()
	handles := vfstest.NewHandles(vfs.OS())
	if err := backup.ExtractWithFS(handles, bytes.NewReader(archive), destDir); err != nil {
		t.Fatalf("ExtractWithFS: %v", err)
	}
	// The control: without Opened() > 0, "no handles outstanding" is
	// indistinguishable from "the driver was never consulted at all" — both
	// read as a clean Outstanding() list.
	if handles.Opened() == 0 {
		t.Fatal("the driver observed zero Open calls while extracting a non-empty archive, " +
			"so ExtractWithFS did not write through the supplied driver")
	}
	if got := handles.Outstanding(); len(got) != 0 {
		t.Errorf("ExtractWithFS left handles open: %v", got)
	}

	// The extracted bytes must match what was archived — checked against the
	// pre-change golden hashes, on the real disk underneath the driver.
	for _, g := range preChangeGolden {
		b, err := os.ReadFile(filepath.Join(destDir, filepath.FromSlash(g.path)))
		if err != nil {
			t.Errorf("%s: %v", g.path, err)
			continue
		}
		if int64(len(b)) != g.size {
			t.Errorf("%s: extracted size = %d, want %d", g.path, len(b), g.size)
		}
		if sum := sha256Hex(b); sum != g.sha256 {
			t.Errorf("%s: extracted content does not match the archived bytes", g.path)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, backup.ManifestName)); !os.IsNotExist(err) {
		t.Errorf("manifest should not be extracted into destDir (err=%v)", err)
	}

	// Strong form: a driver that refuses every open must fail the extract.
	// An implementation that fell back to the os package for writes would
	// still succeed here, because the refusal never reaches the code path
	// that actually touches the disk.
	faults := vfstest.NewFaults(vfs.OS(), "extract-refuses-open")
	faults.FailOpen(vfstest.Always)
	destDir2 := t.TempDir()
	err := backup.ExtractWithFS(faults, bytes.NewReader(archive), destDir2)
	if err == nil {
		t.Fatal("ExtractWithFS succeeded while the driver refused every open, so it wrote " +
			"the extracted files through something other than the supplied driver")
	}
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Errorf("ExtractWithFS failed for a reason other than the driver's refusal: %v", err)
	}
}

// Extract (the pre-existing, unchanged-signature entry point) must keep
// delegating through the default driver and produce the same on-disk result
// as calling ExtractWithFS(vfs.OS(), ...) directly.
func TestExtract_StillDelegatesThroughTheDefaultDriver(t *testing.T) {
	srcDir := t.TempDir()
	buildVFSFixture(t, srcDir)
	archive := buildArchiveBytes(t, vfs.OS(), srcDir)

	viaDefault := t.TempDir()
	if err := backup.Extract(bytes.NewReader(archive), viaDefault); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	viaWithFS := t.TempDir()
	if err := backup.ExtractWithFS(vfs.OS(), bytes.NewReader(archive), viaWithFS); err != nil {
		t.Fatalf("ExtractWithFS: %v", err)
	}

	for _, g := range preChangeGolden {
		a, err := os.ReadFile(filepath.Join(viaDefault, filepath.FromSlash(g.path)))
		if err != nil {
			t.Fatalf("%s: via Extract: %v", g.path, err)
		}
		b, err := os.ReadFile(filepath.Join(viaWithFS, filepath.FromSlash(g.path)))
		if err != nil {
			t.Fatalf("%s: via ExtractWithFS: %v", g.path, err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s: Extract and ExtractWithFS(vfs.OS(), ...) wrote different bytes", g.path)
		}
	}
}
