package lsm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

const (
	roleFlush   vfstest.Role = "flush"
	roleCompact vfstest.Role = "compact"
	roleRead    vfstest.Role = "read"
)

// lsmRoles names the actor behind an operation, from the path and the open
// flags alone.
//
// SSTablePath writes L<level>-<id>.sst, so the level is in the name: the flush
// worker creates level 0, the compaction worker creates the rest, and it is
// the only actor that removes an SSTable. Any read-only open is a foreground
// reader.
//
// The role is derived rather than declared, which costs pkg/lsm no production
// change at all. It does not carry over to pkg/storage, where a flush and a
// reader touch one file; that case needs a declared role, and pkg/vfs can gain
// one additively when it does.
func lsmRoles(op vfstest.Op, name string, flag int) vfstest.Role {
	base := filepath.Base(name)
	if !strings.HasSuffix(base, ".sst") {
		return vfstest.RoleUnknown
	}
	if op == vfstest.OpRemove {
		return roleCompact
	}
	if op != vfstest.OpOpen {
		return vfstest.RoleUnknown
	}
	if flag&os.O_CREATE == 0 {
		return roleRead
	}
	var level, id int
	if _, err := fmt.Sscanf(base, "L%d-%d.sst", &level, &id); err != nil {
		return vfstest.RoleUnknown
	}
	if level == 0 {
		return roleFlush
	}
	return roleCompact
}

// The classifier must read the file name, not the whole path. A data directory
// whose own name contains "L0-" would otherwise put every table in the flush
// role, and the sweep would be attributing operations to the wrong actor while
// looking exactly as it does when it is right.
func TestLSMRolesReadsTheBaseName(t *testing.T) {
	cases := []struct {
		name string
		op   vfstest.Op
		path string
		flag int
		want vfstest.Role
	}{
		{"level 0 create is a flush", vfstest.OpOpen, "/d/L0-000001.sst", os.O_CREATE | os.O_RDWR, roleFlush},
		{"level 1 create is a compaction", vfstest.OpOpen, "/d/L1-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"level 10 create is a compaction", vfstest.OpOpen, "/d/L10-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"read-only open is a reader", vfstest.OpOpen, "/d/L0-000001.sst", os.O_RDONLY, roleRead},
		{"a misleading directory name", vfstest.OpOpen, "/tmp/L0-store/L2-000001.sst", os.O_CREATE | os.O_RDWR, roleCompact},
		{"a remove is a compaction cleanup", vfstest.OpRemove, "/d/L1-000001.sst", 0, roleCompact},
		{"not an SSTable", vfstest.OpOpen, "/d/snapshot.json", os.O_CREATE | os.O_RDWR, vfstest.RoleUnknown},
		{"a directory listing is nobody's", vfstest.OpReadDir, "/d", 0, vfstest.RoleUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lsmRoles(tc.op, tc.path, tc.flag); got != tc.want {
				t.Errorf("lsmRoles(%v, %q, %#o) = %q, want %q", tc.op, tc.path, tc.flag, got, tc.want)
			}
		})
	}
}

// errorsIsInjected reports whether an error came from the fault driver rather
// than from the code under test. An injected fault is an expected outcome of a
// sweep step; anything else is a finding.
func errorsIsInjected(err error) bool {
	return err != nil && strings.Contains(err.Error(), vfstest.ErrInjected.Error())
}

// S1 walks the point of failure through every I/O operation the SHIPPED flush
// worker performs, while a foreground reader runs against the same store.
//
// The oracle is the durability promise. Close waits for the workers and then
// flushes what is left, so a Close that returns nil says every acknowledged
// write is on the disk. The store may lose the flush — that is what the fault
// does — but it must not report success and lose the data.
//
// Three things make the sweep sound, and each one was necessary:
//
//   - The keys are written BEFORE the flush is triggered, and the memtable is
//     large enough that no write triggers one by itself. Otherwise the worker
//     snapshots the memtable at a moment that varies per run, so it writes a
//     different number of entries each time and the trace diverges.
//   - The scenario waits until the worker has taken the trigger before it
//     closes. select picks randomly among ready cases, so a Close issued too
//     early can win the race and the flush never happens at all.
//   - Close, not Sync, is the join. Sync calls flush on the caller's goroutine,
//     which races the worker for the same lock: whichever loses returns early,
//     and which one loses varies per run.
func TestFlushWorkerUnderFaultSweep(t *testing.T) {
	if testing.Short() {
		t.Skip("the sweep opens a store per failure point")
	}

	root := t.TempDir()
	const keys = 8

	vfstest.SweepRole(t, roleFlush, 64,
		func() *vfstest.RoleFS { return vfstest.NewRoles(vfs.OS(), "lsm-flush", lsmRoles) },
		func(fs vfs.FileSystem) error {
			stepDir := filepath.Join(root, fmt.Sprintf("step-%d", nextStepID()))
			opts := LSMOptions{
				DataDir: stepDir,
				FS:      fs,
				// Large enough that eight small writes never fill it, so the
				// only flush is the one this scenario asks for.
				MemTableSize:         1 << 20,
				CompactionStrategy:   DefaultLeveledCompaction(),
				EnableAutoCompaction: true, // the shipped workers
			}
			l, err := NewLSMStorage(opts)
			if err != nil {
				return err
			}

			stop := make(chan struct{})
			readerDone := make(chan struct{})
			go func() {
				defer close(readerDone)
				for {
					select {
					case <-stop:
						return
					default:
						l.Get([]byte("k0"))
					}
				}
			}()

			for i := 0; i < keys; i++ {
				if err := l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value")); err != nil {
					close(stop)
					<-readerDone
					_ = l.Close()
					return err
				}
			}

			l.triggerFlush()
			if !waitFor(2*time.Second, func() bool {
				rfs, ok := fs.(*vfstest.RoleFS)
				return ok && rfs.OpsForRole(roleFlush) > 0
			}) {
				close(stop)
				<-readerDone
				_ = l.Close()
				return fmt.Errorf("the flush worker never took the trigger in %s", stepDir)
			}

			close(stop)
			<-readerDone

			closeErr := l.Close()

			violations, invErr := CheckInvariants(l)
			if invErr != nil {
				return fmt.Errorf("CheckInvariants: %w", invErr)
			}
			if len(violations) > 0 {
				return fmt.Errorf("invariants violated in %s: %s", stepDir, strings.Join(violations, "; "))
			}

			// The durability claim, and only a clean Close makes one.
			if closeErr == nil {
				reopened, err := NewLSMStorage(LSMOptions{
					DataDir:              stepDir,
					MemTableSize:         1 << 20,
					CompactionStrategy:   DefaultLeveledCompaction(),
					EnableAutoCompaction: false,
				})
				if err != nil {
					return fmt.Errorf("reopen %s: %w", stepDir, err)
				}
				defer reopened.Close()
				for i := 0; i < keys; i++ {
					key := fmt.Sprintf("k%d", i)
					if _, ok := reopened.Get([]byte(key)); !ok {
						return fmt.Errorf("Close returned nil but %q did not survive the reopen of %s",
							key, stepDir)
					}
				}
			}

			return closeErr
		},
		func(t *testing.T, n int, runErr error) {
			// An injected I/O error is an expected outcome of a step. A lost
			// write, a violated invariant or a lying Close is not.
			if runErr == nil || errorsIsInjected(runErr) {
				return
			}
			t.Errorf("failure point %d: %v", n, runErr)
		},
	)
}

// stepID gives each sweep step its own directory. time.Now would collide when
// two steps land in the same nanosecond, and a step that inherited another
// step's files would compare the wrong thing.
var stepID atomic.Int64

func nextStepID() int64 { return stepID.Add(1) }

// waitFor polls until cond holds or the deadline passes. It returns whether
// cond held, so a caller can fail with a message instead of continuing on a
// state that never arrived.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// A flush that fails partway must not leave its half-written SSTable behind.
//
// NewSSTableWithFS creates the file and then writes to it. On a write, sync or
// close error it returned without removing what it had created, so a partial
// table stayed in the data directory with no level holding it. NewLSMStorage
// rebuilds the levels by reading that directory and returns an error when any
// file fails to open, so one failed flush could stop the store from opening
// again at all.
func TestFailedFlushLeavesNoPartialSSTable(t *testing.T) {
	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "lsm-partial", lsmRoles)
	// The fault must land AFTER the open, or no file is created and there is
	// nothing to leak. A persistent fault fails the open itself, so this is
	// single-shot on the first write: the file exists, the write fails, and
	// the question is what happens to the file.
	fs.FailAtKey(vfstest.Key{Role: roleFlush, Op: vfstest.OpWrite, Nth: 1})

	l, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		FS:                   fs,
		MemTableSize:         1 << 20,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}

	for i := 0; i < 8; i++ {
		if err := l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	l.triggerFlush()
	if !waitFor(2*time.Second, func() bool { return fs.Fired() }) {
		t.Fatal("the fault never fired, so the test proves nothing")
	}
	_ = l.Close()

	// Close retried the flush and wrote a real SSTable, which is the repaired
	// behaviour, so the directory is not empty. What must not be there is the
	// half-written one, and every .sst present must be readable.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sst") {
			continue
		}
		sst, err := OpenSSTable(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Errorf("a failed flush left %s behind, and it cannot be opened: %v", e.Name(), err)
			continue
		}
		_ = sst.Close()
	}

	// And the store must still open. Before the repair it could not: one
	// half-written table made ListSSTables fail, and NewLSMStorage returns
	// that error rather than skipping the file.
	reopened, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		MemTableSize:         1 << 20,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: false,
	})
	if err != nil {
		t.Fatalf("the store could not be reopened after one failed flush: %v", err)
	}
	_ = reopened.Close()
}

// faultedFlushStore opens a store whose flush worker will fail once, at the
// named operation, and returns it with the driver.
func faultedFlushStore(t *testing.T, dir string, key vfstest.Key) (*LSMStorage, *vfstest.RoleFS) {
	t.Helper()
	fs := vfstest.NewRoles(vfs.OS(), "lsm-flush-fault", lsmRoles)
	fs.FailAtKey(key)
	l, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		FS:                   fs,
		MemTableSize:         1 << 20,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}
	return l, fs
}

// A flush that fails must not lose the data it was flushing.
//
// flush moves the memtable aside before it writes. The first repair for that
// cleared the moved-aside table on error, which stopped flushing from seizing
// up and threw away writes that Put had acknowledged. The entries go back into
// the active memtable now, so a later flush retries them.
func TestFailedFlushKeepsTheDataForRetry(t *testing.T) {
	dir := t.TempDir()
	l, fs := faultedFlushStore(t, dir, vfstest.Key{Role: roleFlush, Op: vfstest.OpWrite, Nth: 1})

	for i := 0; i < 8; i++ {
		if err := l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	l.triggerFlush()
	if !waitFor(2*time.Second, func() bool { return fs.Fired() }) {
		t.Fatal("the fault never fired, so the test proves nothing")
	}

	// Still readable: the flush failed, so nothing is durable, but nothing
	// acknowledged has vanished either.
	if !waitFor(2*time.Second, func() bool { _, ok := l.Get([]byte("k0")); return ok }) {
		t.Error("a key vanished when its flush failed")
	}

	// And a later flush persists them, because they are back in the memtable.
	if err := l.Sync(); err != nil {
		t.Fatalf("the retry flush failed: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close after a successful retry: %v", err)
	}

	reopened, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		MemTableSize:         1 << 20,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: false,
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	for i := 0; i < 8; i++ {
		key := fmt.Sprintf("k%d", i)
		if _, ok := reopened.Get([]byte(key)); !ok {
			t.Errorf("%q did not survive the reopen after a retried flush", key)
		}
	}
}

// Close returning nil is a promise that everything before it is on the disk.
//
// A background worker has no caller, so its flush error reached log.Printf and
// nothing else. Sync and Close return it now, until a flush succeeds.
//
// The fault has to persist for this to be observable, which is a finding in
// itself. With a single-shot fault the entries go back into the memtable, the
// next flush retries them and succeeds, and Sync returning nil is then correct
// rather than a lie. The promise is only at risk when the disk stays broken.
//
// This is a characterisation test, not a guard on anything this commit
// changed. It passes through the error Sync and Close already returned from
// the flush they attempt directly. It is here because the promise is worth
// pinning, and because writing it is what showed that the sticky-error field
// this commit first added had no path that could observe it.
func TestCloseReportsAPersistentlyFailingFlush(t *testing.T) {
	dir := t.TempDir()
	fs := vfstest.NewRoles(vfs.OS(), "lsm-flush-fault", lsmRoles)
	fs.FailAllForRole(roleFlush)
	l, err := NewLSMStorage(LSMOptions{
		DataDir:              dir,
		FS:                   fs,
		MemTableSize:         1 << 20,
		CompactionStrategy:   DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	})
	if err != nil {
		t.Fatalf("NewLSMStorage: %v", err)
	}

	for i := 0; i < 8; i++ {
		if err := l.Put([]byte(fmt.Sprintf("k%d", i)), []byte("value")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	l.triggerFlush()
	if !waitFor(2*time.Second, func() bool { return fs.Fired() }) {
		t.Fatal("the fault never fired, so the test proves nothing")
	}

	if err := l.Sync(); err == nil {
		t.Error("Sync returned nil after a background flush had failed: a caller told " +
			"its data was durable would be wrong")
	}
	if err := l.Close(); err == nil {
		t.Error("Close returned nil after a background flush had failed")
	}
}
