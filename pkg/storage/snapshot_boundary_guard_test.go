package storage

// Red-first tests for coord task graphdb:v1.4-boundary-lsn-downgrade-guard
// (spec: refuse the open when the recovered WAL LSN is below the recorded
// snapshot boundary).
//
// The defect this guards: since #611 every snapshot records WALBoundaryLSN,
// and replay skips WAL entries at or below it. A binary that does not know
// about the boundary (built before #611) resets the WAL's LSN counter to 0
// on its own truncate, so its next write takes a low LSN — one the loaded
// snapshot's boundary already covers. Replay then silently skips that write.
//
// G1 (TestSnapshotBoundaryGuard_DowngradeWriteIsRefused) and G2
// (TestSnapshotBoundaryGuard_WriteAboveBoundaryStillOpens) reuse
// snapshotBoundaryFormats from snapshot_boundary_test.go, so the guard runs
// under the plain, batched and compressed WAL backends and both snapshot
// formats. G3 (an empty WAL opens) is already covered by
// TestSnapshotBoundary_WriteAfterCleanCloseSurvivesCrash; this file does not
// duplicate it.
import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// rawWAL is the subset of *wal.WAL, *wal.BatchedWAL and *wal.CompressedWAL
// these tests need in order to simulate a write made by a binary that does
// not go through GraphStorage at all — the "previous binary" in the spec's
// defect narrative.
type rawWAL interface {
	Append(wal.OpType, []byte) (uint64, error)
	RaiseLSNTo(uint64)
	Close() error
}

// openDirectWAL opens the WAL backend cfg selects, directly on cfg's "wal"
// subdirectory, bypassing GraphStorage (and so bypassing
// raiseWALLSNToSnapshotBoundary) entirely. A fresh WAL opened this way on an
// already-truncated file starts its own counter at 0, no matter what
// boundary LSN the paired snapshot recorded — which is the shape of the
// downgrade the guard exists to catch.
func openDirectWAL(t *testing.T, dir string, cfg StorageConfig) rawWAL {
	t.Helper()
	walDir := filepath.Join(dir, "wal")
	switch {
	case cfg.EnableCompression:
		w, err := wal.NewCompressedWALWithFS(walDir, nil)
		if err != nil {
			t.Fatalf("open direct compressed WAL: %v", err)
		}
		return w
	case cfg.EnableBatching:
		w, err := wal.NewBatchedWALWithFS(walDir, cfg.BatchSize, cfg.FlushInterval, nil)
		if err != nil {
			t.Fatalf("open direct batched WAL: %v", err)
		}
		return w
	default:
		w, err := wal.NewWALWithFS(walDir, nil)
		if err != nil {
			t.Fatalf("open direct WAL: %v", err)
		}
		return w
	}
}

// downgradeEntryData returns a create-node WAL payload replayCreateNode
// accepts: a JSON-encoded Node with the given ID, distinct from the fixture
// node A that snapshotBoundaryAfterCleanClose creates.
func downgradeEntryData(t *testing.T, id uint64) []byte {
	t.Helper()
	data, err := json.Marshal(Node{
		ID:         id,
		TenantID:   rtTenantA,
		Labels:     []string{"Person"},
		Properties: map[string]Value{"name": StringValue("downgrade-write")},
	})
	if err != nil {
		t.Fatalf("marshal downgrade entry: %v", err)
	}
	return data
}

// snapshotBoundaryAfterCleanClose opens cfg, creates three nodes, closes
// cleanly (so the boundary the snapshot records is at least 3 and the WAL
// file is truncated to empty), then reopens to read back the boundary the
// clean Close recorded, and closes that handle too. The store is not left
// open, so the direct-WAL step that follows in each test owns the WAL file
// alone.
//
// Three creates, not one: a fresh direct WAL's first Append always takes
// LSN 1 (recoverLSN on an empty file starts the counter at 0), so a
// boundary of 1 would make that write equal to the boundary, not below it —
// the T1 case (a failed truncate), which must still open, not the downgrade
// this file's G1 test exercises. Three creates gives room between them.
func snapshotBoundaryAfterCleanClose(t *testing.T, dir string, cfg StorageConfig) uint64 {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, name := range []string{"A", "B", "C"} {
		if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue(name)}); err != nil {
			t.Fatalf("create node %s: %v", name, err)
		}
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("clean close: %v", err)
	}

	gs2, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen to read the recorded boundary: %v", err)
	}
	boundary := gs2.snapshotBoundaryLSN
	if err := gs2.Close(); err != nil {
		t.Fatalf("close the boundary-reading reopen: %v", err)
	}
	return boundary
}

// G1: a downgrade write is refused.
//
// On the code before the fix, the final reopen succeeds with no error and
// the node the direct-WAL append carried is absent — replay silently skips
// it as already covered by the boundary. That output is quoted verbatim in
// the PR body.
func TestSnapshotBoundaryGuard_DowngradeWriteIsRefused(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := f.cfg(dir)

			boundary := snapshotBoundaryAfterCleanClose(t, dir, cfg)
			if boundary < 2 {
				t.Fatalf("test premise broken: snapshot boundary is %d, want at least 2", boundary)
			}

			direct := openDirectWAL(t, dir, cfg)
			entryLSN, err := direct.Append(wal.OpCreateNode, downgradeEntryData(t, 9001))
			if err != nil {
				t.Fatalf("append the downgrade entry: %v", err)
			}
			if err := direct.Close(); err != nil {
				t.Fatalf("close the direct WAL: %v", err)
			}

			// Positive control: the guard has nothing to catch unless this
			// entry's LSN is genuinely below the recorded boundary.
			if entryLSN >= boundary {
				t.Fatalf("test premise broken: appended entry LSN %d is not below boundary %d, so this test does not exercise the guard", entryLSN, boundary)
			}

			gs, err := NewGraphStorageWithConfig(cfg)
			if err == nil {
				_ = gs.Close()
				t.Fatalf("reopen: want an error naming recovered LSN %d below boundary %d, got nil", entryLSN, boundary)
			}
			if !errors.Is(err, ErrWALBehindSnapshot) {
				t.Fatalf("reopen error does not satisfy errors.Is(err, ErrWALBehindSnapshot): %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, strconv.FormatUint(entryLSN, 10)) {
				t.Errorf("error text %q does not name the recovered LSN %d", msg, entryLSN)
			}
			if !strings.Contains(msg, strconv.FormatUint(boundary, 10)) {
				t.Errorf("error text %q does not name the snapshot boundary %d", msg, boundary)
			}

			// A missed release in guardWALNotBehindSnapshot's failed-open
			// cleanup path would leave the WAL directory unusable for
			// anything else. Reopening it directly, appending one entry,
			// and closing it must all succeed with no error.
			verify := openDirectWAL(t, dir, cfg)
			if _, err := verify.Append(wal.OpCreateNode, downgradeEntryData(t, 9003)); err != nil {
				t.Fatalf("append after the refused open: %v — the refusal did not release the WAL directory", err)
			}
			if err := verify.Close(); err != nil {
				t.Fatalf("close after the refused open: %v — the refusal did not release the WAL directory", err)
			}
		})
	}
}

// G2: a write above the boundary still opens and replays. Passes before the
// fix too — it guards against a guard that fires too wide.
func TestSnapshotBoundaryGuard_WriteAboveBoundaryStillOpens(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := f.cfg(dir)

			boundary := snapshotBoundaryAfterCleanClose(t, dir, cfg)
			if boundary < 2 {
				t.Fatalf("test premise broken: snapshot boundary is %d, want at least 2", boundary)
			}

			direct := openDirectWAL(t, dir, cfg)
			direct.RaiseLSNTo(boundary)
			entryLSN, err := direct.Append(wal.OpCreateNode, downgradeEntryData(t, 9002))
			if err != nil {
				t.Fatalf("append the above-boundary entry: %v", err)
			}
			if err := direct.Close(); err != nil {
				t.Fatalf("close the direct WAL: %v", err)
			}

			if entryLSN != boundary+1 {
				t.Fatalf("test premise broken: appended entry LSN = %d, want boundary+1 = %d", entryLSN, boundary+1)
			}

			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = gs.Close() }()

			if _, err := gs.GetNodeForTenant(9002, rtTenantA); err != nil {
				t.Fatalf("GetNodeForTenant(9002): %v, want the replayed entity present", err)
			}
		})
	}
}
