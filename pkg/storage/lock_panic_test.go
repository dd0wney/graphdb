package storage

import (
	"testing"
	"time"
)

// injectedPanic is the value the hook panics with, so the harness can tell its
// own panic from a real one.
type injectedPanic struct{ site string }

// lockReleaseTimeout bounds each call after the injected panic. A lock left
// held makes the call block for ever, so the timeout is the failure signal.
const lockReleaseTimeout = 5 * time.Second

// assertPanicReleasesLock makes the critical section at site panic during op,
// then proves the lock was released: next (a second operation that takes the
// same lock) and Close must both return.
//
// The panic must come from the hook. If op returns without it, the site name is
// wrong or op does not reach the section, and the test would otherwise pass
// without testing anything.
func assertPanicReleasesLock(t *testing.T, gs *GraphStorage, site string, op func(), next func() error) {
	t.Helper()
	lockPanicHook = func(s string) {
		if s == site {
			panic(injectedPanic{site: s})
		}
	}
	defer func() { lockPanicHook = nil }()

	if !panicked(op, site) {
		t.Fatalf("op did not reach panic point %q", site)
	}
	lockPanicHook = nil

	if err := withinTimeout(next); err != nil {
		t.Fatalf("second operation after a panic at %q: %v", site, err)
	}
	if err := withinTimeout(gs.Close); err != nil {
		t.Fatalf("Close after a panic at %q: %v", site, err)
	}
}

// panicked runs op and reports whether it panicked with the injected value for
// site. Any other panic is re-raised: it is a real defect, not the test's.
func panicked(op func(), site string) (hit bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if p, ok := r.(injectedPanic); ok && p.site == site {
			hit = true
			return
		}
		panic(r)
	}()
	op()
	return false
}

// errLockHeld is what withinTimeout returns when fn does not return in time.
type errLockHeld struct{}

func (errLockHeld) Error() string {
	return "did not return within " + lockReleaseTimeout.String() + "; the lock is still held"
}

func withinTimeout(fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(lockReleaseTimeout):
		return errLockHeld{}
	}
}

// newPanicTestStore opens a store with one node. The second operation of each
// test writes the same node again, so it needs gs.mu and the node's shard
// lock, which are the locks a panic could leave held.
func newPanicTestStore(t *testing.T) (*GraphStorage, *Node) {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	n, err := gs.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(1)})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	return gs, n
}

// updateAgain is the second operation for a test whose store holds n.
func updateAgain(gs *GraphStorage, n *Node) func() error {
	return func() error { return gs.UpdateNode(n.ID, map[string]Value{"k": IntValue(9)}) }
}

func TestLockPanic_PatchNode(t *testing.T) {
	for _, site := range []string{"patchNode", "materializeNode", "patchNode.shard"} {
		t.Run(site, func(t *testing.T) {
			gs, n := newPanicTestStore(t)
			assertPanicReleasesLock(t, gs, site,
				func() { _ = gs.UpdateNode(n.ID, map[string]Value{"k": IntValue(3)}) },
				updateAgain(gs, n))
		})
	}
}

func TestLockPanic_BatchCommit(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "Batch.Commit",
		func() {
			batch := gs.BeginBatch()
			batch.UpdateNode(n.ID, map[string]Value{"k": IntValue(3)})
			_ = batch.Commit()
		},
		updateAgain(gs, n))
}
