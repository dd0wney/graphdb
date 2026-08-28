package btree

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// pkg/btree's pager writes pages to disk and had no way to make that fail.

func TestPager_OnFaultDriver_OpenFailureIsReported(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "pager-open-fault")
	fs.FailOpen(vfstest.Always)

	if _, err := NewPagerWithFS(filepath.Join(t.TempDir(), "pages.db"), fs); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("NewPagerWithFS error = %v, want the injected fault", err)
	}
}

func TestPager_OnFaultDriver_SyncFailureIsReported(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "pager-sync-fault")

	p, err := NewPagerWithFS(filepath.Join(t.TempDir(), "pages.db"), fs)
	if err != nil {
		t.Fatalf("NewPagerWithFS: %v", err)
	}
	defer func() { fs.Clear(); _ = p.Close() }()

	fs.FailSync(vfstest.Always)
	if err := p.Flush(); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("Flush error = %v, want the injected fault. A nil here means the pager "+
			"reported pages durable after fsync failed", err)
	}
	if fs.Syncs() == 0 {
		t.Error("the pager never called Sync; the assertion above proved nothing")
	}
}
