package storage

// Durability gate for the mmap snapshot publish.
//
// snapshotMmapLocked writes snapshot.mmap.tmp, syncs it, and renames it over
// snapshot.mmap. The rename is atomic on POSIX, but the name it creates lives
// in the parent directory, and that directory is not durable because the file
// was synced. A power cut between the rename and the directory's next
// writeback leaves the new snapshot's bytes on the disk with no name pointing
// at them, and the store reopens on the previous snapshot or on nothing.
//
// This test drives a real snapshot through a vfstest.RoleFS and reads the
// operation trace. The classifier puts the rename AND the data directory
// itself into one role, so the directory's open and sync land in the same
// trace as the rename and the ORDER between them is visible. An open in that
// role's trace can only be the directory: no other path is classified into it.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const (
	// rolePublishDir collects the rename and every operation on the data
	// directory itself. Nothing else is classified into it.
	rolePublishDir = vfstest.Role("publish")
	// roleStoreData collects every operation on a file inside the directory.
	roleStoreData = vfstest.Role("storedata")
)

// publishDirClassifier attributes the rename and the data directory to
// rolePublishDir, and everything else to roleStoreData.
func publishDirClassifier(dir string) vfstest.Classifier {
	clean := filepath.Clean(dir)
	return func(op vfstest.Op, name string, _ int) vfstest.Role {
		if op == vfstest.OpRename || filepath.Clean(name) == clean {
			return rolePublishDir
		}
		return roleStoreData
	}
}

// TestMmapSnapshot_SyncsParentDirectoryAfterRename is the durability gate for
// the mmap snapshot publish.
//
// The order is the whole assertion. A directory sync BEFORE the rename proves
// nothing: it publishes an entry the rename has not yet created.
func TestMmapSnapshot_SyncsParentDirectoryAfterRename(t *testing.T) {
	if !vfs.DirSyncSupported {
		t.Skip("this platform cannot sync a directory handle")
	}

	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "mmap-snapshot-dirsync", publishDirClassifier(dir))

	cfg := mmapConfig(dir)
	cfg.FS = fs
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	defer func() { _ = gs.Close() }()

	for i := 0; i < 4; i++ {
		if _, err := gs.CreateNode([]string{"Person"}, map[string]Value{"name": StringValue("n")}); err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
	}

	if err := gs.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	trace := fs.Trace(rolePublishDir)
	last := -1
	for i, op := range trace {
		if op == "rename" {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("no rename in the publish trace, so the snapshot did not publish: %v", trace)
	}
	if len(trace) < last+3 || trace[last+1] != "open" || trace[last+2] != "sync" {
		t.Fatalf("the data directory is not opened and synced after the rename\ntrace: %s",
			strings.Join(trace, ","))
	}
}
