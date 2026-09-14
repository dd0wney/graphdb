package storage

// Red-first tests for coord task graphdb:v1.4-snapshot-records-wal-boundary
// (spec: "a snapshot records the WAL boundary LSN, replay skips entries at or
// below it"). These are T1, T2 and T4 of the spec's section 5; T3 (the
// pkg/wal truncate-preserves-LSN assertions) is a separate task.
//
// The defect: Close writes the snapshot first, then truncates the WAL. When
// the truncate fails for a reason other than the sticky bufio error (a rename
// error from the disk, for example), the WAL keeps entries the snapshot
// already covers. The next open replays them over the snapshot, resurrecting
// whatever they undid. T1 reproduces the harmful sequence end to end: create,
// delete (WAL append fails, in-memory delete still applies), Close (snapshot
// lands without the node, WAL truncate fails, Close reports the rename
// error), reopen (replay brings the node back on pre-fix code).
//
// T1 embeds T4 (fingerprint parity): the same reopen must serve the snapshot,
// not a replayed stale WAL, for every public getter, not just GetNode.
//
// T2 is the guard for the raise-on-open half of the fix: it passes today and
// must keep passing after the fix, so an implementation that records the
// boundary but forgets to raise the WAL's LSN counter on open doesn't trade
// one data-loss bug for another (a write right after a clean close, lost to a
// crash before the next Close, because replay would skip its LSN as already
// covered).
import (
	"errors"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// roleWALRename is the RoleFS role for the WAL truncate's rename, and only
// that operation. See the doc comment on T1 below for why this test uses
// vfstest.RoleFS instead of faultsim.WALRotateReopen: the faultsim site
// reports Truncate's rename as failed AFTER the real rename already
// succeeded, so the on-disk WAL is genuinely truncated regardless of what
// Close reports. RoleFS's FailAllOpForRole refuses the real syscall, so the
// old (un-truncated) WAL file genuinely survives on disk, which is what the
// defect in section 1 of the spec requires.
const roleWALRename = vfstest.Role("wal-rename")

// walRenameClassifier puts only the WAL truncate's rename into roleWALRename.
// The plain and batched backends share the old path "<dir>/wal/wal.log.new"
// (BatchedWAL.Truncate delegates to the wrapped WAL's Truncate); the
// compressed backend uses "<dir>/wal/wal_compressed.log.new" instead, a
// distinct name that does not contain "wal.log.new" as a substring, so both
// must be matched explicitly. Every other operation — including the
// JSON/mmap snapshot's own publish rename — gets roleOther, so arming a
// fault on roleWALRename cannot touch the snapshot write.
func walRenameClassifier(op vfstest.Op, name string, _ int) vfstest.Role {
	if op == vfstest.OpRename && (strings.Contains(name, "wal.log.new") || strings.Contains(name, "wal_compressed.log.new")) {
		return roleWALRename
	}
	return vfstest.Role("other")
}

// snapshotBoundaryFormats is the table every test in this file runs over:
// both snapshot formats must show the same behaviour, since the boundary LSN
// and the raise-on-open step are WAL-level, not format-level. jsonConfig and
// mmapConfig both leave EnableBatching and EnableCompression at
// DefaultStorageConfig's false, so those two rows alone exercise only the
// plain WAL's RaiseLSNTo. The batched and compressed rows below exist so
// BatchedWAL.RaiseLSNTo and CompressedWAL.RaiseLSNTo run under T1 and T2 too.
var snapshotBoundaryFormats = []struct {
	name string
	cfg  func(dir string) StorageConfig
}{
	{"json", jsonConfig},
	{"mmap", mmapConfig},
	{"json-batched", func(dir string) StorageConfig {
		cfg := jsonConfig(dir)
		cfg.EnableBatching = true
		return cfg
	}},
	{"json-compressed", func(dir string) StorageConfig {
		cfg := jsonConfig(dir)
		cfg.EnableCompression = true
		return cfg
	}},
}

// T1: a deleted node stays deleted when the WAL truncate fails at Close.
//
// Sequence: CreateNode A. DeleteNode A, with its WAL append made to fail (the
// in-memory delete still applies, per ErrWALWriteFailed's contract). Close,
// with the WAL truncate's rename made to fail. Reopen. A must stay deleted,
// and the reopened store's fingerprint must match the live store's fingerprint
// taken just before Close (T4).
//
// Expected on pre-fix code: the "node is gone after reopen" assertion fails —
// replay brings A back over the snapshot that already omits it.
//
// The append fault (the delete's WAL write) uses vfstest.FaultFS, layered
// over a vfstest.RoleFS that carries the rename fault: FaultFS.Rename has no
// failure mode of its own and forwards straight to RoleFS, so the two faults
// don't interfere with each other.
func TestSnapshotBoundary_DeletedNodeStaysDeletedWhenTruncateFailsAtClose(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()
			roles := vfstest.NewRoles(vfs.OS(), "T1-"+f.name+"-roles", walRenameClassifier)
			faults := vfstest.NewFaults(roles, "T1-"+f.name+"-faults")
			cfg := f.cfg(dir)
			cfg.FS = faults

			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			// Setup: create A with the fault disarmed, so the fault below hits
			// only the delete's WAL append, not the create's.
			a, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("A")})
			if err != nil {
				t.Fatalf("setup: create node A: %v", err)
			}

			// Arm the append fault now, after the create returned, so it fires
			// on the delete's WAL write.
			faults.FailWrite(vfstest.Once, 0)
			delErr := gs.DeleteNodeForTenant(a.ID, rtTenantA)
			if delErr == nil {
				t.Fatal("DeleteNodeForTenant returned a nil error: the WAL append fault did not reach the delete's write path")
			}
			if !errors.Is(delErr, ErrWALWriteFailed) {
				t.Fatalf("DeleteNodeForTenant error does not satisfy errors.Is(err, ErrWALWriteFailed): %v", delErr)
			}
			if !faults.Fired() {
				t.Fatal("the append fault never fired, so this test proves nothing about the delete's WAL write")
			}

			// In memory, the delete applied despite the WAL append failure. This
			// is the fingerprint T4 checks against the reopened store below —
			// the reopen must reproduce it exactly, not a stale replay of the
			// pre-delete state.
			before := fingerprintTenant(t, gs, rtTenantA)

			// Arm the truncate's rename failure: every rename classified into
			// roleWALRename fails, which per walRenameClassifier is only the
			// WAL's "wal.log.new" -> "wal.log" rename.
			roles.FailAllOpForRole(roleWALRename, vfstest.OpRename)
			closeErr := gs.Close()
			if closeErr == nil {
				t.Fatal("Close returned a nil error: the truncate fault did not reach WAL.Truncate")
			}
			if !strings.Contains(closeErr.Error(), "failed to rename WAL file") {
				t.Fatalf("Close error does not name the rename failure (positive control for the fault): %v", closeErr)
			}
			if !roles.Fired() {
				t.Fatal("the rename fault never fired; Close's error proves nothing about the truncate path")
			}

			// Reopen with a plain config: no fault driver, so both faults are
			// disarmed by construction.
			gs2, err := NewGraphStorageWithConfig(f.cfg(dir))
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer func() { _ = gs2.Close() }()

			// Pin that the boundary skip actually ran, not only its result: the
			// leftover WAL holds exactly one entry (create A, LSN 1) at or below
			// the recorded boundary, and replayEntry must have skipped it rather
			// than never seeing it.
			if gs2.walSkippedEntries != 1 {
				t.Fatalf("walSkippedEntries = %d, want 1", gs2.walSkippedEntries)
			}

			if _, err := gs2.GetNodeForTenant(a.ID, rtTenantA); !errors.Is(err, ErrNodeNotFound) {
				t.Fatalf("after reopen: expected ErrNodeNotFound for the deleted node, got %v", err)
			}

			assertGraphInvariants(t, gs2)

			// T4: fingerprint parity. Proves the reopen serves the snapshot, not
			// a replayed stale WAL, for every public getter — not just GetNode.
			after := fingerprintTenant(t, gs2, rtTenantA)
			assertFingerprintEqual(t, before, after, "reopen after truncate-failure close")
		})
	}
}

// T2: a write after a clean close survives a crash.
//
// Create A, close cleanly, reopen. Create B, then crash (no Close — the WAL
// after a clean close must already reflect that B's write takes the LSN
// right after the recorded boundary, not restart at 1). Reopen again: B must
// be there.
//
// This test passes today. It is the guard for the raise-on-open step: an
// implementation that records the boundary but keeps the WAL's reset-to-0 on
// truncate would make a post-close write collide with an already-covered LSN,
// and replay would skip it as covered — losing it on the next crash.
func TestSnapshotBoundary_WriteAfterCleanCloseSurvivesCrash(t *testing.T) {
	for _, f := range snapshotBoundaryFormats {
		t.Run(f.name, func(t *testing.T) {
			dir := t.TempDir()

			gs, err := NewGraphStorageWithConfig(f.cfg(dir))
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			a, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("A")})
			if err != nil {
				t.Fatalf("create node A: %v", err)
			}
			if err := gs.Close(); err != nil {
				t.Fatalf("clean close: %v", err)
			}

			// Reopen cleanly, write B, then crash: don't call Close.
			// testCrashableStorage registers a t.Cleanup(gs.Close) so the test
			// doesn't leak the handle, but nothing closes it before the next
			// open below runs — the same crash-simulation idiom
			// append_wal_batch_test.go uses.
			var bID uint64
			{
				gs2 := testCrashableStorage(t, dir, f.cfg(dir))
				b, err := gs2.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("B")})
				if err != nil {
					t.Fatalf("create node B: %v", err)
				}
				bID = b.ID
				// DON'T Close — crash sim (testCrashableStorage handles cleanup).
			}

			gs3, err := NewGraphStorageWithConfig(f.cfg(dir))
			if err != nil {
				t.Fatalf("reopen after crash: %v", err)
			}
			defer func() { _ = gs3.Close() }()

			if _, err := gs3.GetNodeForTenant(a.ID, rtTenantA); err != nil {
				t.Fatalf("node A missing after crash reopen: %v", err)
			}
			if _, err := gs3.GetNodeForTenant(bID, rtTenantA); err != nil {
				t.Fatalf("node B missing after crash reopen: %v", err)
			}
		})
	}
}
