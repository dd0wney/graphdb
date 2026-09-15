package wal

// T2 of coord task graphdb:v1.4-wal-sync-failure-rollback. Same shape as T1
// (wal_vfs_test.go's TestWAL_OnFaultDriver_SyncFailurePoisonsFurtherAppends)
// but run over the three remaining append paths: WAL.AppendBatchAtomic,
// CompressedWAL.Append, and BatchedWAL.Enqueue+Wait (which wraps a plain
// WAL, so its poison comes from the same field WAL.Append checks).
//
// Each case: append once (ok), arm a Once sync fault, append again (returns
// the injected fault), disarm by exhausting the Once, append a third time.
// Pre-fix, the third append in every case succeeds — the WAL backends don't
// know a prior sync failed.

import (
	"errors"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// walPoisonCase opens one of the three append paths under test and exposes
// just enough of it — append, current LSN, and a way to read back every
// entry the file holds — for the shared assertions in
// TestWALBackends_SyncFailurePoisonsFurtherAppends to run against any of
// them identically.
type walPoisonCase struct {
	name string
	open func(t *testing.T, dir string, fs vfs.FileSystem) (
		doAppend func(data string) error,
		currentLSN func() uint64,
		readAll func() ([]*Entry, error),
		closeFn func() error,
	)
}

func TestWALBackends_SyncFailurePoisonsFurtherAppends(t *testing.T) {
	cases := []walPoisonCase{
		{
			name: "WAL.AppendBatchAtomic",
			open: func(t *testing.T, dir string, fs vfs.FileSystem) (
				func(string) error, func() uint64, func() ([]*Entry, error), func() error,
			) {
				t.Helper()
				w, err := NewWALWithFS(dir, fs)
				if err != nil {
					t.Fatalf("NewWALWithFS: %v", err)
				}
				doAppend := func(data string) error {
					return w.AppendBatchAtomic([]BatchEntry{{OpType: OpCreateNode, Data: []byte(data)}})
				}
				return doAppend, w.GetCurrentLSN, w.ReadAll, w.Close
			},
		},
		{
			name: "CompressedWAL.Append",
			open: func(t *testing.T, dir string, fs vfs.FileSystem) (
				func(string) error, func() uint64, func() ([]*Entry, error), func() error,
			) {
				t.Helper()
				w, err := NewCompressedWALWithFS(dir, fs)
				if err != nil {
					t.Fatalf("NewCompressedWALWithFS: %v", err)
				}
				doAppend := func(data string) error {
					_, err := w.Append(OpCreateNode, []byte(data))
					return err
				}
				return doAppend, w.GetCurrentLSN, w.ReadAll, w.Close
			},
		},
		{
			name: "BatchedWAL.Enqueue",
			open: func(t *testing.T, dir string, fs vfs.FileSystem) (
				func(string) error, func() uint64, func() ([]*Entry, error), func() error,
			) {
				t.Helper()
				// A large batch size and a long flush interval keep the
				// background flusher from racing the explicit bw.flush()
				// calls below — every append in this test is flushed by
				// hand, deterministically, never by the ticker.
				bw, err := NewBatchedWALWithFS(dir, 100, time.Hour, fs)
				if err != nil {
					t.Fatalf("NewBatchedWALWithFS: %v", err)
				}
				doAppend := func(data string) error {
					pending := bw.Enqueue(OpCreateNode, []byte(data))
					bw.flush()
					return pending.Wait()
				}
				// bw.wal is the wrapped *WAL: BatchedWAL keeps no ReadAll of
				// its own, and this is the same package, so the field is
				// reachable directly.
				return doAppend, bw.GetCurrentLSN, bw.wal.ReadAll, bw.Close
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := vfstest.NewFaults(vfs.OS(), c.name)
			doAppend, currentLSN, readAll, closeFn := c.open(t, t.TempDir(), fs)
			defer func() { fs.Clear(); _ = closeFn() }()

			if err := doAppend("first"); err != nil {
				t.Fatalf("append before the fault: %v", err)
			}

			fs.FailSync(vfstest.Once)
			if err := doAppend("during"); !errors.Is(err, vfstest.ErrInjected) {
				t.Fatalf("append during the fault: got %v, want the injected fault", err)
			}
			if !fs.Fired() {
				t.Fatal("the sync fault never fired; the assertions below prove nothing about the poison path")
			}
			lsnAfterFault := currentLSN()

			// The fault mode was Once, so it is now disarmed. On code that
			// does not poison, this third append succeeds normally.
			thirdErr := doAppend("after")
			if !errors.Is(thirdErr, ErrWALPoisoned) {
				t.Fatalf("append after the fault: got %v, want an error satisfying errors.Is(err, ErrWALPoisoned)", thirdErr)
			}

			if got := currentLSN(); got != lsnAfterFault {
				t.Fatalf("current LSN = %d after the poisoned append, want unchanged %d (the second append's own LSN)", got, lsnAfterFault)
			}

			entries, err := readAll()
			if err != nil {
				t.Fatalf("readAll: %v", err)
			}
			// Two entries, not three: "first" landed cleanly, "during" was
			// flushed to the file before its fsync failed, and "after" was
			// refused before it ever reached the file.
			if len(entries) != 2 {
				t.Fatalf("readAll returned %d entries, want 2 (the poisoned append must not have written a third)", len(entries))
			}
		})
	}
}
