package wal

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// These tests make the same assertions as the fault tests next door, with one
// difference that is the whole point: the fault arrives through
// NewWALWithFS and a driver installed by published API, so the code path under
// test is the one a production WAL takes. wal_io_fault_test.go reaches into an
// unexported field and swaps a test type, which tests a specially-constructed
// object instead.
//
// This is the distinction SQLite draws between its TCL suite and TH3.

func TestWAL_OnFaultDriver_AppendSurfacesSyncFailure(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "wal-sync-fault")

	w, err := NewWALWithFS(t.TempDir(), fs)
	if err != nil {
		t.Fatalf("NewWALWithFS: %v", err)
	}
	defer func() { fs.Clear(); _ = w.Close() }()

	if _, err := w.Append(OpCreateNode, []byte("before")); err != nil {
		t.Fatalf("Append before the fault: %v", err)
	}

	fs.FailSync(vfstest.Always)
	_, err = w.Append(OpCreateNode, []byte("during"))
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("Append error = %v, want the injected fault. A nil here means the WAL "+
			"reported a durable commit after fsync failed", err)
	}

	// The fault must have reached the driver, not merely been armed. Without
	// this the test cannot tell a fired fault from a code path that never
	// called Sync at all.
	if fs.Syncs() == 0 {
		t.Error("the WAL never called Sync; the assertion above proved nothing")
	}
}

func TestWAL_OnFaultDriver_OpenFailureIsReported(t *testing.T) {
	fs := vfstest.NewFaults(vfs.OS(), "wal-open-fault")
	fs.FailOpen(vfstest.Always)

	if _, err := NewWALWithFS(t.TempDir(), fs); !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("NewWALWithFS error = %v, want the injected fault", err)
	}
}

// TestWAL_OnCrashDriver_ReorderedWritesDoNotInventEntries is the case graphdb
// could not previously construct. A truncation always removes a suffix; a
// buffered filesystem can lose a write in the middle and keep the one after it.
//
// Recovery is allowed to stop early. It is not allowed to return an entry that
// was never written, or to return entries out of order.
func TestWAL_OnCrashDriver_ReorderedWritesDoNotInventEntries(t *testing.T) {
	const appends = 6

	for seed := int64(0); seed < 25; seed++ {
		dir := t.TempDir()
		crash := vfstest.NewCrash(vfs.OS(), "wal-crash", seed)
		crash.SetPolicy(vfstest.ReorderAndLose)

		w, err := NewWALWithFS(dir, crash)
		if err != nil {
			t.Fatalf("seed %d: NewWALWithFS: %v", seed, err)
		}
		for i := 0; i < appends; i++ {
			if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
				t.Fatalf("seed %d: Append %d: %v", seed, i, err)
			}
		}
		// Write without syncing, so the crash has something to reorder.
		if err := w.writer.WriteByte(0x5A); err != nil {
			t.Fatalf("seed %d: seed an unsynced byte: %v", seed, err)
		}
		if err := w.writer.Flush(); err != nil {
			t.Fatalf("seed %d: flush: %v", seed, err)
		}
		if _, err := crash.Crash(); err != nil {
			t.Fatalf("seed %d: Crash: %v", seed, err)
		}

		reopened, err := NewWALWithFS(dir, vfs.OS())
		if err != nil {
			continue // refusing to open a damaged WAL is acceptable
		}
		entries, err := reopened.ReadAll()
		_ = reopened.Close()
		if err != nil {
			continue
		}

		if len(entries) > appends {
			t.Fatalf("seed %d: recovery returned %d entries, more than the %d written",
				seed, len(entries), appends)
		}
		for i, e := range entries {
			if e.LSN != uint64(i+1) {
				t.Fatalf("seed %d: entry %d has LSN %d, want %d — recovery did not return a prefix",
					seed, i, e.LSN, i+1)
			}
		}
	}
}
