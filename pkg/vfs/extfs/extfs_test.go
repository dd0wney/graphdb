package extfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vfs/extfs"
)

// recordingFS is a stand-in for the external library: it implements extfs.FS
// by SHAPE ONLY — it imports nothing from any external package — and passes
// every call through to the real filesystem while recording it.
//
// Pass-through matters. An in-memory fake could not be mmapped, so it could
// not drive graphdb's real snapshot publish, and a test that cannot reach the
// real write path proves nothing about the adapter.
type recordingFS struct {
	mu  sync.Mutex
	ops []string
}

func (r *recordingFS) note(op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
}

func (r *recordingFS) trace() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ops))
	copy(out, r.ops)
	return out
}

func (r *recordingFS) OpenFile(name string, flag int, perm os.FileMode) (extfs.File, error) {
	r.note("open:" + filepath.Base(name))
	f, err := os.OpenFile(name, flag, perm) //nolint:gosec // a test driver over a t.TempDir path
	if err != nil {
		return nil, err
	}
	return &recordingFile{File: f, fs: r, base: filepath.Base(name)}, nil
}

func (r *recordingFS) Remove(name string) error {
	r.note("remove:" + filepath.Base(name))
	return os.Remove(name)
}

func (r *recordingFS) Rename(oldpath, newpath string) error {
	r.note("rename:" + filepath.Base(newpath))
	return os.Rename(oldpath, newpath)
}

func (r *recordingFS) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }

func (r *recordingFS) MkdirAll(path string, perm os.FileMode) error {
	r.note("mkdirall")
	return os.MkdirAll(path, perm)
}

func (r *recordingFS) ReadDir(name string) ([]os.DirEntry, error) { return os.ReadDir(name) }

type recordingFile struct {
	*os.File
	fs   *recordingFS
	base string
}

func (f *recordingFile) Sync() error {
	f.fs.note("sync:" + f.base)
	return f.File.Sync()
}

// The external File interface has no Seek, ReadAt or WriteAt. Hiding os.File's
// versions keeps this fake honest: it must offer exactly what extfs.File
// declares, or the adapter would be tested against a richer type than any real
// external library provides.
func (f *recordingFile) Read(p []byte) (int, error)  { return f.File.Read(p) }
func (f *recordingFile) Write(p []byte) (int, error) { return f.File.Write(p) }
func (f *recordingFile) Truncate(n int64) error      { return f.File.Truncate(n) }
func (f *recordingFile) Close() error                { return f.File.Close() }

var _ extfs.FS = (*recordingFS)(nil)
var _ extfs.File = (*recordingFile)(nil)

// storeOnExternalFS opens a store whose only filesystem is one that cannot
// seek and cannot write positionally, and returns whatever went wrong.
func storeOnExternalFS(t *testing.T, bulk bool) error {
	t.Helper()
	cfg := storage.DefaultStorageConfig(t.TempDir())
	cfg.UseMmapSnapshot = true
	cfg.BulkImportMode = bulk
	cfg.FS = extfs.New(&recordingFS{}, "no-positional")

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		return err
	}
	if _, err := gs.CreateNode([]string{"Thing"}, map[string]storage.Value{
		"name": storage.StringValue("alpha"),
	}); err != nil {
		_ = gs.Close()
		return err
	}
	return gs.Close() // Close publishes the snapshot
}

// The snapshot publish needs WriteAt. This is a GATE on a fact, not a wish.
//
// mmap_snapshot_writer.go:184 backpatches the header with WriteAt(hdr, 0)
// after the body is written — the same shape as pkg/lsm's SSTable writer. An
// external recorder that tracked offsets by addition would place that write at
// the END of the file. The header is the one write in that path where a wrong
// offset produces a structurally VALID but wrong result: a zeroed or misplaced
// header parses, and the reader then walks the body as if it were a directory.
//
// If this test starts failing because the publish SUCCEEDS, the path stopped
// writing positionally and became reachable by an external crash simulator.
func TestSnapshotPublishNeedsWriteAt(t *testing.T) {
	err := storeOnExternalFS(t, true) // bulk mode skips the WAL, isolating the publish
	if err == nil {
		t.Fatal("the snapshot published on a filesystem with no WriteAt. " +
			"mmap_snapshot_writer.go:184 used to backpatch the header; if that changed, " +
			"the publish path is now sweepable and this gate should be replaced by a sweep")
	}
	if !errors.Is(err, extfs.ErrUnsupported) {
		t.Fatalf("the publish failed for a reason other than the positional-write boundary: %v", err)
	}
	for _, want := range []string{"WriteAt", "snapshot.mmap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the boundary error does not mention %q, so it does not say what to fix: %v", want, err)
		}
	}
}

// The WAL needs Seek. Same shape, different call.
//
// pkg/wal seeks at wal.go:138 (rewind to read the log) and wal.go:217 (seek to
// the end to append). Two calls stand between an external crash simulator and
// every WAL durability path in this repository.
func TestTheWALNeedsSeek(t *testing.T) {
	err := storeOnExternalFS(t, false) // WAL constructed
	if err == nil {
		t.Fatal("the store opened on a filesystem with no Seek. pkg/wal used to seek at " +
			"wal.go:138 and :217; if that changed, the WAL paths are now sweepable")
	}
	if !errors.Is(err, extfs.ErrUnsupported) {
		t.Fatalf("the store failed for a reason other than the seek boundary: %v", err)
	}
	for _, want := range []string{"Seek", "wal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the boundary error does not mention %q, so it does not say what to fix: %v", want, err)
		}
	}
}

// The three refusals must name the operation and wrap ErrUnsupported.
//
// This is the property that stops a recorder producing fiction. A silent
// approximation of Seek would put pkg/lsm's SSTable header backpatch at the
// end of the file instead of offset 0.
func TestUnsupportedOperationsRefuseAndNameThemselves(t *testing.T) {
	dir := t.TempDir()
	rec := &recordingFS{}
	fsys := extfs.New(rec, "probe")

	f, err := fsys.Open(filepath.Join(dir, "x"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	cases := []struct {
		op   string
		call func() error
	}{
		{"Seek", func() error { _, err := f.Seek(0, 0); return err }},
		{"ReadAt", func() error { _, err := f.ReadAt(make([]byte, 4), 0); return err }},
		{"WriteAt", func() error { _, err := f.WriteAt([]byte("abcd"), 0); return err }},
	}
	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatalf("%s returned no error: a silent approximation here makes every "+
					"recorded offset after it wrong", c.op)
			}
			if !errors.Is(err, extfs.ErrUnsupported) {
				t.Errorf("%s error does not wrap ErrUnsupported, so a caller cannot tell "+
					"the boundary from a real I/O failure: %v", c.op, err)
			}
			if !strings.Contains(err.Error(), c.op) {
				t.Errorf("%s error does not name the operation, so the caller cannot tell "+
					"which call site needs support: %v", c.op, err)
			}
		})
	}
}

// Name is graphdb's, not the external library's, because extfs.File has none.
func TestAdapterSuppliesTheNamesTheExternalFilesystemLacks(t *testing.T) {
	dir := t.TempDir()
	fsys := extfs.New(&recordingFS{}, "my-driver")
	if got := fsys.Name(); got != "my-driver" {
		t.Errorf("FileSystem.Name = %q, want %q", got, "my-driver")
	}
	path := filepath.Join(dir, "named")
	f, err := fsys.Open(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if got := f.Name(); got != path {
		t.Errorf("File.Name = %q, want %q", got, path)
	}
}

// An empty name must not produce a driver that reports "" in diagnostics.
func TestEmptyNameFallsBack(t *testing.T) {
	if got := extfs.New(&recordingFS{}, "").Name(); got != "extfs" {
		t.Errorf("Name = %q, want the %q fallback", got, "extfs")
	}
}
