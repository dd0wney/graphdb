package vfstest

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Sweep walks the point of failure through every I/O operation a scenario
// performs.
//
// SQLite states the loop plainly: "Rig the alternative interface to give an I/O
// error on the N-th system call, for N=1,2,3,.... Repeat until no I/O errors
// occur." The termination condition carries the meaning — when a run completes
// without the fault firing, N has passed the end of the sequence and every
// error path the scenario can reach has been visited.
//
// A single fault test picks one point in that sequence, and picks it by luck.
// The interesting points are the ones inside a rename, between a write and its
// fsync, or on the close after a failed sync, and no one guesses those.
//
// run performs the scenario against the supplied filesystem and returns
// whatever error it produced; an injected fault is an expected outcome, not a
// test failure. check runs after every step and is where the invariant lives:
// typically that a store reopens, that no committed record was lost, and that
// nothing panicked.
//
// maxOps bounds the walk so a scenario that grows its own work per step cannot
// loop forever. Exceeding it fails the test rather than passing quietly,
// because a sweep that never reaches its end has not proved what it claims.
func Sweep(t *testing.T, maxOps int, run func(fs vfs.FileSystem) error, check func(t *testing.T, n int, runErr error)) {
	t.Helper()

	if maxOps <= 0 {
		maxOps = 512
	}

	for n := 1; ; n++ {
		if n > maxOps {
			t.Fatalf("sweep did not terminate within %d operations: the fault kept firing, "+
				"so the scenario performs more I/O each step or the driver is not counting", maxOps)
		}

		fs := NewFaults(vfs.OS(), "sweep")
		fs.FailNthOp(n)

		err := run(fs)
		check(t, n, err)

		if !fs.Fired() {
			// N ran off the end of the sequence: every operation has now been
			// failed once, in turn.
			if n == 1 {
				t.Fatal("the scenario performed no I/O at all, so the sweep proved nothing")
			}
			return
		}
	}
}

// SweepCount reports how many I/O operations a scenario performs, without
// injecting anything. Useful for asserting that a scenario is doing the amount
// of work a test believes it is — a sweep over three operations is not the
// coverage a reader assumes from the word "sweep".
func SweepCount(run func(fs vfs.FileSystem) error) (ops int, err error) {
	fs := NewFaults(vfs.OS(), "count")
	err = run(fs)
	return fs.Ops(), err
}
