package wal

// TruncateUpTo rewrote and renamed the WAL through the os package while every
// sibling used the driver.
//
//	wal.go:314,326             w.fs.Open, w.fs.Rename
//	compressed_wal.go:103,112  w.fs.Open, w.fs.Rename
//	truncate_upto.go:49,88     os.OpenFile, os.Rename
//
// Three implementations of one rewrite-and-swap, and only this one bypassed.
// It was deliberate and recorded — swapInRewrittenFile said "the rename above
// still goes through the os package, which predates pkg/vfs" — so this change
// finishes a migration rather than repairing an oversight.
//
// The file used the driver for ONE thing: vfs.SyncParentDir at lines 108 and
// 212. A parent-directory sync, through the driver, guarding a rename that
// happened on a different filesystem. The seam was in the author's hands at
// that line.
//
// WHY IT MATTERS BEYOND TESTABILITY. A caller who sets StorageConfig.FS to
// serve a store from memory got wal.log.new created and renamed on the real
// disk. And a crash harness that rebuilds a state into a temporary directory
// still had this function writing to, and renaming over, the directory it had
// copied FROM: the github.com/dd0wney/fault session lost an afternoon to a
// sweep that scanned the rebuilt copy while the purge ran on the original, and
// nearly reported a plaintext-remanence defect that did not exist.
//
// Until this landed, the crash-safety of the encryption toggle was not merely
// unmeasured but unmeasurable, by us or by anyone.

import (
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const roleTruncate = vfstest.Role("truncate")

func truncateClassifier() vfstest.Classifier {
	return func(vfstest.Op, string, int) vfstest.Role { return roleTruncate }
}

// The rename must be visible to the driver.
//
// The control is the directory sync. It already went through the driver before
// this change, so a trace holding a sync and no rename is the half-used seam
// stated exactly.
func TestTruncateUpToRenamesThroughTheDriver(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(dir string, fs vfs.FileSystem) (interface{ TruncateUpTo(uint64) error }, func() error)
	}{
		{
			name: "plain",
			build: func(dir string, fs vfs.FileSystem) (interface{ TruncateUpTo(uint64) error }, func() error) {
				w, err := NewWALWithFS(dir, fs)
				if err != nil {
					t.Fatalf("open WAL: %v", err)
				}
				return w, func() error {
					_, e := w.Append(OpCreateNode, []byte("payload"))
					return e
				}
			},
		},
		{
			name: "compressed",
			build: func(dir string, fs vfs.FileSystem) (interface{ TruncateUpTo(uint64) error }, func() error) {
				w, err := NewCompressedWALWithFS(dir, fs)
				if err != nil {
					t.Fatalf("open compressed WAL: %v", err)
				}
				return w, func() error {
					_, e := w.Append(OpCreateNode, []byte("payload"))
					return e
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := vfstest.NewRoles(vfs.OS(), "truncate-driver", truncateClassifier())
			w, appendOne := tc.build(t.TempDir(), fs)
			for i := 0; i < 4; i++ {
				if err := appendOne(); err != nil {
					t.Fatalf("append %d: %v", i, err)
				}
			}

			before := len(fs.Trace(roleTruncate))
			if err := w.TruncateUpTo(2); err != nil {
				t.Fatalf("TruncateUpTo: %v", err)
			}
			trace := fs.Trace(roleTruncate)[before:]
			joined := strings.Join(trace, ",")

			// The control. The directory sync already went through the driver,
			// so if this is missing the driver is not installed and the rename
			// assertion below would be measuring nothing.
			if !strings.Contains(joined, "sync") {
				t.Fatalf("the driver observed no sync during the truncate, so it is not "+
					"installed and this test proves nothing\ntrace: %s", joined)
			}
			if !strings.Contains(joined, "rename") {
				t.Errorf("the driver observed the directory sync and NOT the rename, so the "+
					"rewrite happened on the real filesystem while the sync that publishes it "+
					"happened on the driver's\ntrace: %s", joined)
			}
		})
	}
}

// The decisive form: a driver that refuses to rename must fail the truncate.
//
// An implementation calling os.Rename succeeds here, on a real file, whatever
// the driver says.
func TestTruncateUpToFailsWhenTheDriverRefusesTheRename(t *testing.T) {
	fs := vfstest.NewRoles(vfs.OS(), "truncate-rename-refused", truncateClassifier())
	w, err := NewWALWithFS(t.TempDir(), fs)
	if err != nil {
		t.Fatalf("open WAL: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	fs.FailAllOpForRole(roleTruncate, vfstest.OpRename)

	if err := w.TruncateUpTo(2); err == nil {
		t.Fatal("the truncate succeeded while the driver refused every rename, so it " +
			"renamed past the driver and straight on the real filesystem")
	}
}
