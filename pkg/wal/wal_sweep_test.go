package wal

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/faultsim"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// TestWAL_SweepEveryIOFailurePoint walks the point of failure through every I/O
// operation an open-append-close cycle performs, and requires that recovery is
// sane at each one.
//
// SQLite's loop: "Rig the alternative interface to give an I/O error on the
// N-th system call, for N=1,2,3,.... Repeat until no I/O errors occur." A test
// that fails one chosen operation picks its point by luck. The interesting
// points are between a write and its fsync, and on the close after a failed
// sync, and nobody guesses those.
//
// The invariant at every step, stated precisely because the first version of
// this test got it wrong: a later reader must see a contiguous PREFIX of the
// entries that were ATTEMPTED, and it must see at least every entry that was
// ACKNOWLEDGED. Recovery returning more than were acknowledged is legitimate —
// an Append whose fsync failed has already written its record — and the sweep
// is what surfaced that, at N=3.
func TestWAL_SweepEveryIOFailurePoint(t *testing.T) {
	const appends = 4

	var dir string
	var acked []uint64
	var attempted int

	run := func(fs vfs.FileSystem) error {
		dir = t.TempDir()
		acked = nil
		attempted = 0

		w, err := NewWALWithFS(dir, fs)
		if err != nil {
			return err
		}
		for i := 0; i < appends; i++ {
			attempted++
			lsn, err := w.Append(OpCreateNode, []byte("payload"))
			if err != nil {
				// An acknowledged append is one that returned nil. This one
				// did not, so it is not owed durability.
				_ = w.Close()
				return err
			}
			acked = append(acked, lsn)
		}
		return w.Close()
	}

	check := func(t *testing.T, n int, runErr error) {
		t.Helper()

		reopened, err := NewWALWithFS(dir, vfs.OS())
		if err != nil {
			// Refusing to reopen is acceptable; losing data silently is not.
			return
		}
		entries, err := reopened.ReadAll()
		_ = reopened.Close()
		if err != nil {
			return
		}

		// Recovery may legitimately return MORE than were acknowledged. An
		// Append that fails at its fsync has already written the record, so
		// the data is on disk while the caller was told the commit failed.
		// The sweep found this at N=3, and the first version of this test
		// asserted the opposite. What the WAL owes is weaker and precise: no
		// entry that was never attempted, and no gap.
		if len(entries) > attempted {
			t.Fatalf("N=%d: recovery returned %d entries but only %d appends were attempted",
				n, len(entries), attempted)
		}
		if len(entries) < len(acked) {
			t.Fatalf("N=%d: recovery returned %d entries but %d appends were acknowledged as durable",
				n, len(entries), len(acked))
		}
		for i, e := range entries {
			if e.LSN != uint64(i+1) {
				t.Fatalf("N=%d: entry %d has LSN %d, want %d — recovery is not a prefix",
					n, i, e.LSN, i+1)
			}
		}
	}

	// Count first, so the test reports how much it actually covers. A sweep
	// over three operations is not what a reader assumes from the word.
	ops, err := vfstest.SweepCount(run)
	if err != nil {
		t.Fatalf("counting run failed: %v", err)
	}
	t.Logf("the scenario performs %d I/O operations; the sweep fails each in turn", ops)
	if ops < appends {
		t.Fatalf("only %d operations counted for %d appends — the driver is not seeing the I/O", ops, appends)
	}

	vfstest.Sweep(t, 128, run, check)
}

// TestWAL_LSNExhaustionPathIsReachable executes a branch that 2^64 appends
// would otherwise be needed to reach.
//
// Before faultsim this guard was code nobody had ever run. It could have
// returned the wrong error, or fallen through, and every test would still pass.
func TestWAL_LSNExhaustionPathIsReachable(t *testing.T) {
	faultsim.Reset()
	defer faultsim.Reset()

	w, err := NewWAL(t.TempDir())
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer func() { _ = w.Close() }()

	faultsim.Arm(faultsim.WALLSNExhausted, 0)

	_, err = w.Append(OpCreateNode, []byte("payload"))
	if err == nil {
		t.Fatal("Append succeeded with the LSN space reported exhausted")
	}
	if !strings.Contains(err.Error(), "LSN space exhausted") {
		t.Errorf("error = %q, want it to name LSN exhaustion", err)
	}

	// The negative control. Without it, an error from anywhere else in Append
	// would satisfy the assertion above and the guard would still be untested.
	if faultsim.Calls(faultsim.WALLSNExhausted) == 0 {
		t.Error("the fault site was never reached; the assertion above proved nothing")
	}

	// And the guard must not be sticky: disarming restores normal service.
	faultsim.Disarm(faultsim.WALLSNExhausted)
	if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
		t.Errorf("Append failed after the fault was disarmed: %v", err)
	}
}

// TestWAL_RotateRecoveryPathIsReachable executes the branch that reopens the
// old WAL after a rename fails partway through rotation. It runs in production
// only when a rename fails after the replacement file already exists.
func TestWAL_RotateRecoveryPathIsReachable(t *testing.T) {
	faultsim.Reset()
	defer faultsim.Reset()

	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer func() { _ = w.Close() }()

	if _, err := w.Append(OpCreateNode, []byte("before rotation")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	faultsim.Arm(faultsim.WALRotateReopen, 0)
	err = w.Truncate()
	if err == nil {
		t.Fatal("Truncate succeeded with the rename reported as failed")
	}
	if faultsim.Calls(faultsim.WALRotateReopen) == 0 {
		t.Fatal("the fault site was never reached")
	}
	faultsim.Disarm(faultsim.WALRotateReopen)

	// The point of the recovery branch: the WAL must still be usable. A store
	// whose rotation failed and left an unusable WAL has turned a transient
	// rename error into an outage.
	if _, err := w.Append(OpCreateNode, []byte("after failed rotation")); err != nil {
		t.Errorf("the WAL is unusable after a failed rotation: %v", err)
	}

	// And the temporary file must not be left behind.
	if _, err := vfs.Default().Stat(filepath.Join(dir, "wal.log.new")); err == nil {
		t.Error("the replacement file was left on disk after the failed rotation")
	}
}
