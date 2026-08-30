package wal

// Durability gates for the WAL's rotation and rewrite paths.
//
// A rename is atomic on POSIX. The name it creates is not durable until the
// directory that holds it is itself fsynced. A power cut between the rename
// and the parent directory's next writeback leaves the new file's bytes on the
// disk with no name pointing at them, and the WAL comes back as the file the
// rename was supposed to replace — or as nothing at all.
//
// These tests drive the real rotation paths through a vfstest.RoleFS and read
// the operation trace. The classifier puts the rename AND the data directory
// itself in one role, so the directory's open and sync land in the same trace
// as the rename and the ORDER between them is visible. An open in that role's
// trace can only be the directory: no other path is classified into it.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const (
	// rolePublish collects the rename and every operation on the data
	// directory itself. Nothing else is classified into it.
	rolePublish = vfstest.Role("publish")
	// roleWALData collects every operation on a file inside the directory.
	roleWALData = vfstest.Role("waldata")
)

// publishClassifier attributes the rename and the data directory to
// rolePublish, and everything else to roleWALData.
func publishClassifier(dir string) vfstest.Classifier {
	clean := filepath.Clean(dir)
	return func(op vfstest.Op, name string, _ int) vfstest.Role {
		if op == vfstest.OpRename || filepath.Clean(name) == clean {
			return rolePublish
		}
		return roleWALData
	}
}

// requireDirSyncSupport skips when the platform has no directory fsync. On
// such a platform vfs.SyncParentDir does nothing by design, so asserting that
// it did something would fail for the wrong reason.
func requireDirSyncSupport(t *testing.T) {
	t.Helper()
	if !vfs.DirSyncSupported {
		t.Skip("this platform cannot sync a directory handle")
	}
}

// assertDirSyncAfterRename requires an open and a sync of the directory to
// follow the last rename in the trace.
//
// The order is the whole assertion. A sync before the rename proves nothing:
// it publishes a directory entry that the rename has not yet created.
func assertDirSyncAfterRename(t *testing.T, trace []string) {
	t.Helper()
	last := -1
	for i, op := range trace {
		if op == "rename" {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("no rename in the publish trace, so the scenario did not rotate: %v", trace)
	}
	if len(trace) < last+3 || trace[last+1] != "open" || trace[last+2] != "sync" {
		t.Fatalf("the parent directory is not opened and synced after the rename\ntrace: %s",
			strings.Join(trace, ","))
	}
}

// assertDirSynced requires the trace to end with an open, a sync and a close
// of the directory.
//
// TruncateUpTo and FileRotator.Rotate rename through the os package directly,
// so the driver never sees their rename and the order cannot be read from this
// trace. The order is fixed at the call site instead: the sync follows the
// rename in the source, and this test proves the sync happens at all.
func assertDirSynced(t *testing.T, trace []string) {
	t.Helper()
	n := len(trace)
	if n < 3 || trace[n-3] != "open" || trace[n-2] != "sync" || trace[n-1] != "close" {
		t.Fatalf("the parent directory is not opened, synced and closed\ntrace: %s",
			strings.Join(trace, ","))
	}
}

// TestWALTruncate_SyncsParentDirectoryAfterRename gates wal.go's rotation.
func TestWALTruncate_SyncsParentDirectoryAfterRename(t *testing.T) {
	requireDirSyncSupport(t)

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "wal-truncate-dirsync", publishClassifier(dir))

	w, err := NewWALWithFS(dir, fs)
	if err != nil {
		t.Fatalf("NewWALWithFS: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 4; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	assertDirSyncAfterRename(t, fs.Trace(rolePublish))
}

// TestCompressedWALTruncate_SyncsParentDirectoryAfterRename gates
// compressed_wal.go's rotation.
func TestCompressedWALTruncate_SyncsParentDirectoryAfterRename(t *testing.T) {
	requireDirSyncSupport(t)

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "cwal-truncate-dirsync", publishClassifier(dir))

	w, err := NewCompressedWALWithFS(dir, fs)
	if err != nil {
		t.Fatalf("NewCompressedWALWithFS: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 4; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	assertDirSyncAfterRename(t, fs.Trace(rolePublish))
}

// TestWALTruncateUpTo_SyncsParentDirectory gates the rewrite path shared by
// TruncateUpTo (truncate_upto.go, swapInRewrittenFile).
func TestWALTruncateUpTo_SyncsParentDirectory(t *testing.T) {
	requireDirSyncSupport(t)

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "wal-truncateupto-dirsync", publishClassifier(dir))

	w, err := NewWALWithFS(dir, fs)
	if err != nil {
		t.Fatalf("NewWALWithFS: %v", err)
	}
	defer func() { _ = w.Close() }()

	var boundary uint64
	for i := 0; i < 6; i++ {
		lsn, err := w.Append(OpCreateNode, []byte("payload"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if i == 2 {
			boundary = lsn
		}
	}
	if err := w.TruncateUpTo(boundary); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	assertDirSynced(t, fs.Trace(rolePublish))
}

// TestCompressedWALTruncateUpTo_SyncsParentDirectory gates the compressed
// rewrite path (truncate_upto.go).
func TestCompressedWALTruncateUpTo_SyncsParentDirectory(t *testing.T) {
	requireDirSyncSupport(t)

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "cwal-truncateupto-dirsync", publishClassifier(dir))

	w, err := NewCompressedWALWithFS(dir, fs)
	if err != nil {
		t.Fatalf("NewCompressedWALWithFS: %v", err)
	}
	defer func() { _ = w.Close() }()

	var boundary uint64
	for i := 0; i < 6; i++ {
		lsn, err := w.Append(OpCreateNode, []byte("payload"))
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if i == 2 {
			boundary = lsn
		}
	}
	if err := w.TruncateUpTo(boundary); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	assertDirSynced(t, fs.Trace(rolePublish))
}

// TestFileRotatorRotate_SyncsParentDirectory gates fileutil.go's Rotate.
func TestFileRotatorRotate_SyncsParentDirectory(t *testing.T) {
	requireDirSyncSupport(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "rotate.log")
	fs := vfstest.NewRoles(vfs.OS(), "rotator-dirsync", publishClassifier(dir))

	fr := newFileRotatorWithFS(path, 0, fs)
	if err := fr.Open(); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = fr.Close() }()

	if _, err := fr.Writer().Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fr.Rotate(); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	assertDirSynced(t, fs.Trace(rolePublish))
}
