package storage

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
	"github.com/dd0wney/graphdb/pkg/wal"
)

func TestHealth_OpenStoreIsHealthy(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()

	if err := gs.Health(); err != nil {
		t.Fatalf("Health on an open store: %v", err)
	}
}

func TestHealth_ClosedStoreReportsClosed(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := gs.Health(); !errors.Is(err, ErrStorageClosed) {
		t.Fatalf("Health after Close = %v, want ErrStorageClosed", err)
	}
}

// BulkImportMode has no WAL to poison; Health must not treat that as a fault.
func TestHealth_BulkImportModeIsHealthy(t *testing.T) {
	cfg := DefaultStorageConfig(t.TempDir())
	cfg.BulkImportMode = true
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()

	if err := gs.Health(); err != nil {
		t.Fatalf("Health in bulk mode: %v", err)
	}
}

// A failed fsync poisons the WAL until restart (#624): every later write
// reaches memory and not the disk. Health must report that, on every backend.
func TestHealth_PoisonedWALReportsPoisoned(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			faults := vfstest.NewFaults(vfs.OS(), "health-"+f.name)
			cfg := f.cfg(dir)
			cfg.FS = faults

			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer gs.Close()

			if _, err := gs.CreateNode([]string{"A"}, nil); err != nil {
				t.Fatalf("create A: %v", err)
			}
			if err := gs.Health(); err != nil {
				t.Fatalf("Health before the fault: %v", err)
			}

			faults.FailSync(vfstest.Once)
			if _, err := gs.CreateNode([]string{"B"}, nil); !errors.Is(err, ErrWALWriteFailed) {
				t.Fatalf("create B: got %v, want ErrWALWriteFailed", err)
			}
			if !faults.Fired() {
				t.Fatal("the sync fault never fired; this test proves nothing")
			}

			if err := gs.Health(); !errors.Is(err, wal.ErrWALPoisoned) {
				t.Fatalf("Health after a sync failure = %v, want wal.ErrWALPoisoned", err)
			}
		})
	}
}
