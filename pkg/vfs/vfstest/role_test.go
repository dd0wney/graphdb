package vfstest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

const (
	roleA Role = "a"
	roleB Role = "b"
)

// bySuffix names a role from the file extension, which is enough to test the
// driver without bringing pkg/lsm's rules in here.
func bySuffix(op Op, name string, flag int) Role {
	switch filepath.Ext(name) {
	case ".a":
		return roleA
	case ".b":
		return roleB
	}
	return RoleUnknown
}

func writeThrough(t *testing.T, fs vfs.FileSystem, path string) error {
	t.Helper()
	f, err := fs.Open(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write([]byte("x")); err != nil {
		return err
	}
	return f.Sync()
}

// A fault armed for one role must not fire on another role's operations.
func TestFailNthOpForRoleIsolatesRoles(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	fs.FailNthOpForRole(roleB, 1)

	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("role a was faulted although only role b was armed: %v", err)
	}
	if fs.Fired() {
		t.Fatal("the fault fired on role a")
	}

	err := writeThrough(t, fs, filepath.Join(dir, "f.b"))
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("role b: got %v, want ErrInjected", err)
	}
	if !fs.Fired() {
		t.Fatal("Fired reported false after the fault fired")
	}
}

// The N-th operation of a role means the N-th of that role, not the N-th
// overall. This is the whole reason RoleFS exists beside FaultFS.
func TestFailNthOpForRoleCountsPerRole(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	fs.FailNthOpForRole(roleB, 2) // open is 1, write is 2

	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("role a: %v", err)
	}
	err := writeThrough(t, fs, filepath.Join(dir, "f.b"))
	if !errors.Is(err, ErrInjected) {
		t.Fatalf("role b: got %v, want ErrInjected on the write", err)
	}
	// open, write, sync, and the deferred close: the close counts, which is
	// the point of counting at the driver rather than at the call site.
	if got := fs.OpsForRole(roleA); got != 4 {
		t.Errorf("role a op count = %d, want 4 (open, write, sync, close)", got)
	}
}

// The trace must name the operations in order, because SweepRole compares it
// between runs to decide whether the sweep means anything.
func TestTraceRecordsOperationsInOrder(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := strings.Join(fs.Trace(roleA), ",")
	if !strings.HasPrefix(got, "open,write,sync") {
		t.Errorf("trace = %q, want it to start with open,write,sync", got)
	}
}

// An unclassified path must not silently join a real role. RoleUnknown is a
// role with its own counter for exactly this reason.
func TestUnclassifiedPathTakesRoleUnknown(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	if err := writeThrough(t, fs, filepath.Join(dir, "f.zzz")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := fs.OpsForRole(RoleUnknown); got == 0 {
		t.Error("an unclassified path recorded no operations under RoleUnknown")
	}
	if got := fs.OpsForRole(roleA); got != 0 {
		t.Errorf("role a saw %d operations from an unclassified path", got)
	}
}

// A structural key must name the same operation after the scenario grows an
// operation before it. An ordinal does not, which is why a failure is recorded
// by key rather than by the ordinal the sweep walked.
func TestFailAtKeyIsStableAcrossAnExtraEarlierWrite(t *testing.T) {
	for _, extra := range []int{0, 1, 2} {
		dir := t.TempDir()
		fs := NewRoles(vfs.OS(), "roles", bySuffix)
		fs.FailAtKey(Key{Role: roleA, Op: OpSync, Nth: 1})

		f, err := fs.Open(filepath.Join(dir, "f.a"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatalf("extra=%d open: %v", extra, err)
		}
		for i := 0; i < 1+extra; i++ {
			if _, err := f.Write([]byte("x")); err != nil {
				t.Fatalf("extra=%d write %d: %v", extra, i, err)
			}
		}
		err = f.Sync()
		_ = f.Close()

		if !errors.Is(err, ErrInjected) {
			t.Errorf("extra=%d: the first sync was not faulted: %v", extra, err)
		}
	}
}

// FiredKey must name what actually fired, so a sweep can hand back a case that
// no longer depends on the ordinal it walked.
func TestFiredKeyNamesTheOperation(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	fs.FailNthOpForRole(roleA, 2) // open is 1, write is 2

	_ = writeThrough(t, fs, filepath.Join(dir, "f.a"))

	key, ok := fs.FiredKey()
	if !ok {
		t.Fatal("FiredKey reported nothing after a fault fired")
	}
	if key.Role != roleA || key.Op != OpWrite || key.Nth != 1 {
		t.Errorf("FiredKey = %s, want a/write#1", key)
	}
}

// The barrier must really block. Proved by a state change the test observes
// while the actor is parked, not by a sleep.
func TestPauseBlocksTheActor(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	p := fs.PauseBeforeNthOpForRole(t, roleA, 2) // park on the write

	var wrote atomic.Bool
	done := make(chan error, 1)
	go func() {
		err := writeThrough(t, fs, filepath.Join(dir, "f.a"))
		wrote.Store(true)
		done <- err
	}()

	p.Wait(t, 5*time.Second)
	if wrote.Load() {
		t.Fatal("the actor finished before the barrier released it")
	}

	p.Release(nil)
	if err := <-done; err != nil {
		t.Fatalf("the actor failed after a clean release: %v", err)
	}
}

// Release may choose the error the parked operation reports.
func TestPauseReleasesWithAnInjectedError(t *testing.T) {
	dir := t.TempDir()
	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	p := fs.PauseBeforeNthOpForRole(t, roleA, 2)

	done := make(chan error, 1)
	go func() { done <- writeThrough(t, fs, filepath.Join(dir, "f.a")) }()

	p.Wait(t, 5*time.Second)
	p.Release(ErrInjected)

	if err := <-done; !errors.Is(err, ErrInjected) {
		t.Fatalf("got %v, want ErrInjected", err)
	}
}

// The sweep must visit every operation of the role and then stop.
func TestSweepRoleVisitsEveryOperation(t *testing.T) {
	var seen int
	SweepRole(t, roleA, 32,
		func() *RoleFS { return NewRoles(vfs.OS(), "sweep", bySuffix) },
		func(fs vfs.FileSystem) error {
			return writeThrough(t, fs, filepath.Join(t.TempDir(), "f.a"))
		},
		func(t *testing.T, n int, runErr error) { seen = n },
	)
	// open, write, sync, close is four; the fifth run fires nothing and ends it.
	if seen != 5 {
		t.Errorf("the sweep ended at n=%d, want 5 for a four-operation scenario", seen)
	}
}

// A scenario whose operation sequence is not stable must fail the sweep rather
// than pass it. Without this check the sweep is unsound under concurrency and
// says nothing, which is worse than not running at all.
func TestSweepRoleRejectsANondeterministicScenario(t *testing.T) {
	fake := &testing.T{}
	var round int
	var lastChecked int

	// t.Fatalf calls runtime.Goexit, which would terminate this test's own
	// goroutine. Run the sweep in its own so only that one ends.
	done := make(chan struct{})
	go func() {
		defer close(done)
		SweepRole(fake, roleA, 32,
			func() *RoleFS { return NewRoles(vfs.OS(), "sweep", bySuffix) },
			func(fs vfs.FileSystem) error {
				round++
				dir := t.TempDir()
				path := filepath.Join(dir, "f.a")
				if round%2 == 0 {
					// An extra write on every second run moves every later
					// operation, so run N is not run N-1 plus one failure.
					f, err := fs.Open(path, os.O_RDWR|os.O_CREATE, 0o600)
					if err != nil {
						return err
					}
					_, _ = f.Write([]byte("y"))
					_ = f.Close()
				}
				return writeThrough(t, fs, path)
			},
			func(t *testing.T, n int, runErr error) { lastChecked = n },
		)
	}()
	<-done

	if !fake.Failed() {
		t.Fatal("the sweep accepted a scenario whose operation sequence changes between runs")
	}

	// "The sweep failed" is not the same claim as "the divergence guard fired".
	// The traces first differ at index 2, where an odd run syncs and an even
	// run closes, and the comparison only reaches index 2 at n=4. So check runs
	// for n=1,2,3 and the sweep stops before checking n=4. A different number
	// here means a different guard fired and this test is proving something
	// other than what it says.
	if lastChecked != 3 {
		t.Errorf("check last ran at n=%d, want 3: the sweep stopped for some reason "+
			"other than the trace divergence at n=4", lastChecked)
	}
}

// A scenario that performs no I/O at all must fail the sweep. A sweep that
// terminates immediately looks exactly like one that finished its walk.
func TestSweepRoleRejectsAScenarioThatDoesNothing(t *testing.T) {
	fake := &testing.T{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		SweepRole(fake, roleA, 32,
			func() *RoleFS { return NewRoles(vfs.OS(), "sweep", bySuffix) },
			func(fs vfs.FileSystem) error { return nil },
			func(t *testing.T, n int, runErr error) {},
		)
	}()
	<-done

	if !fake.Failed() {
		t.Error("the sweep accepted a scenario that performed no I/O")
	}
}

// Explore must run every seed and hand back a key that can be pinned. A
// discovery mode that finds something it cannot return as a scripted test
// costs more than it returns.
func TestExploreReturnsAPinnableKey(t *testing.T) {
	dir := t.TempDir()
	fake := &testing.T{}

	var keys []Key
	done := make(chan struct{})
	go func() {
		defer close(done)
		keys = Explore(fake, 4,
			func(seed int64) *RoleFS {
				fs := NewRoles(vfs.OS(), "explore", bySuffix)
				fs.Jitter(seed)
				fs.FailRandomly(seed, roleA, 1.0) // always fault something
				return fs
			},
			func(fs vfs.FileSystem) error {
				return writeThrough(t, fs, filepath.Join(dir, "f.a"))
			},
			func(t *testing.T, seed int64, runErr error) {
				if runErr != nil {
					t.Errorf("seed %d failed", seed)
				}
			},
		)
	}()
	<-done

	if len(keys) == 0 {
		t.Fatal("Explore returned no keys although every seed was armed to fail")
	}
	for _, k := range keys {
		if k.Role != roleA || k.Nth < 1 {
			t.Errorf("key %s is not usable as a FailAtKey regression test", k)
		}
	}

	// The key must actually reproduce the failure on its own, with no seed and
	// no jitter. That is the whole claim the discovery mode makes.
	fs := NewRoles(vfs.OS(), "pinned", bySuffix)
	fs.FailAtKey(keys[0])
	if err := writeThrough(t, fs, filepath.Join(dir, "f.a")); err == nil {
		t.Errorf("the key %s did not reproduce the failure without its seed", keys[0])
	}
}

// Jitter must change when things happen, not what happens.
func TestJitterDoesNotChangeTheTrace(t *testing.T) {
	var first string
	for seed := int64(0); seed < 4; seed++ {
		fs := NewRoles(vfs.OS(), "jitter", bySuffix)
		fs.Jitter(seed)
		if err := writeThrough(t, fs, filepath.Join(t.TempDir(), "f.a")); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		trace := strings.Join(fs.Trace(roleA), ",")
		if first == "" {
			first = trace
			continue
		}
		if trace != first {
			t.Errorf("seed %d produced trace %q, seed 0 produced %q", seed, trace, first)
		}
	}
}

// A directory listing takes a role and can be faulted like any other
// operation. pkg/lsm enumerates its SSTables this way, so its reopen path
// depends on it.
func TestReadDirIsClassifiedAndFaultable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "listing.a") // bySuffix puts this in roleA
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	fs := NewRoles(vfs.OS(), "roles", bySuffix)
	if _, err := fs.ReadDir(dir); err != nil {
		t.Fatalf("clean ReadDir: %v", err)
	}
	if got := fs.OpsForRole(roleA); got != 1 {
		t.Errorf("ReadDir recorded %d operations for role a, want 1", got)
	}

	fs2 := NewRoles(vfs.OS(), "roles", bySuffix)
	fs2.FailAtKey(Key{Role: roleA, Op: OpReadDir, Nth: 1})
	if _, err := fs2.ReadDir(dir); !errors.Is(err, ErrInjected) {
		t.Errorf("faulted ReadDir gave %v, want ErrInjected", err)
	}
}
