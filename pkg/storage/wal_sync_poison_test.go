package storage

// T3 and T4 of coord task graphdb:v1.4-wal-sync-failure-rollback: the
// residual left by graphdb:v1.4-boundary-lsn-downgrade-guard (PR #619).
//
// The defect: a WAL sync (fsync) failure left the LSN counter advanced while
// the flushed bytes' durability was unknown, and the next Append proceeded
// as if nothing had happened. Snapshot() then recorded a WAL boundary LSN
// that the WAL file could end up short of after a real crash — the boundary
// can exceed the durable LSN by the number of sync failures — and the next
// open refused with ErrWALBehindSnapshot, with no way back in.
//
// The fix (pkg/wal) poisons a WAL after a sync failure: every later append
// is refused until the process restarts, so no further LSN is ever handed
// out for an entry the caller believes failed. The fix here (pkg/storage)
// makes Snapshot() truncate an already-poisoned WAL after a successful
// snapshot, the same way Close does: a poisoned WAL holds only entries with
// LSN at or below the poison LSN, and every one of them was already applied
// to memory (and so captured by the snapshot) before the poison took hold —
// truncating it can never drop a write the snapshot lacks.
import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
	"github.com/dd0wney/graphdb/pkg/wal"
)

// poisonTestWALPath returns the on-disk path of the active WAL backend's
// single log file for cfg — "wal.log" for the plain and batched backends
// (BatchedWAL delegates to a wrapped *wal.WAL using that name), "wal_compressed.log"
// for the compressed backend. Mirrors walRenameClassifier's naming in
// snapshot_boundary_test.go.
func poisonTestWALPath(dir string, cfg StorageConfig) string {
	name := "wal.log"
	if cfg.EnableCompression {
		name = "wal_compressed.log"
	}
	return filepath.Join(dir, "wal", name)
}

// truncateWALFileIfLonger simulates a crash that loses every byte written
// since node A's durable append: it rolls the physical WAL file back to
// maxLen, and does nothing if the file is already that short or shorter —
// which is what a poisoned WAL's Snapshot-triggered Truncate (this task's
// fix) already leaves behind, since it empties the file outright.
func truncateWALFileIfLonger(t *testing.T, path string, maxLen int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() <= maxLen {
		return
	}
	if err := os.Truncate(path, maxLen); err != nil {
		t.Fatalf("truncate %s to %d bytes: %v", path, maxLen, err)
	}
}

// TestSnapshotBoundaryGuard_PoisonedWALSurvivesReopen is T3. It runs over
// every backend/format row TestSnapshotBoundaryGuard_DowngradeWriteIsRefused
// covers (plain+json, plain+mmap, batched+json, compressed+json).
//
// Sequence: create A (durable). Arm a Once sync fault. Create B — the fault
// fires, so this returns ErrWALWriteFailed; the in-memory node still exists.
// Create C — on the fix, the WAL is now poisoned, so this ALSO returns
// ErrWALWriteFailed (wrapping wal.ErrWALPoisoned); the in-memory node still
// exists regardless. Snapshot(). Simulate a crash that discards whatever the
// sync failure left undurable. Reopen.
//
// Pre-fix, node C's create succeeds outright (the fault was Once and already
// spent, and pre-fix nothing remembers the earlier failure) — logged as a
// non-fatal mismatch below so the test can still reach the real residual:
// the reopen fails with ErrWALBehindSnapshot (recovered 1, boundary 3) or
// loses a node.
func TestSnapshotBoundaryGuard_PoisonedWALSurvivesReopen(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			faults := vfstest.NewFaults(vfs.OS(), "poison-"+f.name)
			cfg := f.cfg(dir)
			cfg.FS = faults
			walPath := poisonTestWALPath(dir, cfg)

			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			a, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("A")})
			if err != nil {
				t.Fatalf("create node A: %v", err)
			}

			info, err := os.Stat(walPath)
			if err != nil {
				t.Fatalf("stat WAL after A: %v", err)
			}
			lenAfterA := info.Size()
			if lenAfterA == 0 {
				t.Fatalf("test premise broken: WAL file is empty after a durable create")
			}

			faults.FailSync(vfstest.Once)
			b, bErr := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("B")})
			if !errors.Is(bErr, ErrWALWriteFailed) {
				t.Fatalf("create node B: got %v, want ErrWALWriteFailed", bErr)
			}
			if b == nil {
				t.Fatal("create node B: returned a nil node alongside the error")
			}
			if !faults.Fired() {
				t.Fatal("the sync fault never fired; this test proves nothing about the poison path")
			}

			c, cErr := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("C")})
			if c == nil {
				t.Fatal("create node C: returned a nil node")
			}
			// Non-fatal: on unfixed code the WAL is not poisoned, so this
			// create succeeds with a nil error. Recorded, not fatal, so the
			// test still reaches the reopen below and surfaces the real
			// residual (ErrWALBehindSnapshot or a missing node).
			if !errors.Is(cErr, ErrWALWriteFailed) {
				t.Errorf("create node C: got %v, want ErrWALWriteFailed (poisoned)", cErr)
			}
			if !errors.Is(cErr, wal.ErrWALPoisoned) {
				t.Errorf("create node C error does not satisfy errors.Is(err, wal.ErrWALPoisoned): %v", cErr)
			}

			if err := gs.Snapshot(); err != nil {
				t.Fatalf("Snapshot: %v", err)
			}

			// Simulate the crash: whatever the sync failure left undurable
			// on disk is gone. A no-op on the fix, whose Snapshot already
			// truncated the poisoned WAL to empty.
			truncateWALFileIfLonger(t, walPath, lenAfterA)

			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("reopen after simulated crash: %v", err)
			}
			defer func() { _ = gs2.Close() }()

			for _, n := range []*Node{a, b, c} {
				if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
					t.Errorf("node %d missing after reopen: %v", n.ID, err)
				}
			}

			// T4: CheckInvariants must pass on the reopened store.
			assertGraphInvariants(t, gs2)
		})
	}
}
