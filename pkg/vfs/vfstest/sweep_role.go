package vfstest

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// SweepRole walks the point of failure through every operation ONE actor
// performs, while the other actors run untouched.
//
// Sweep does the same for a process that has one actor. It cannot be used
// under concurrency: its counter is per process, so the N-th operation is a
// different operation on every run. The walk then visits an arbitrary subset
// of the error paths while appearing to visit all of them, and it still
// terminates and still passes.
//
// SweepRole counts per role, which is stable only if the role's own operation
// sequence is stable. That is an assumption about the scenario, not a fact
// about the driver, so the sweep checks it: on every run it compares the
// role's trace with the previous run's, up to the injection point, and fails
// on a divergence. A sweep that cannot make that comparison is a sweep that
// terminates, passes, and proves nothing.
//
// newFS builds a fresh driver, with its classifier, for each step. Sweep can
// build its own inline because it needs no classifier.
func SweepRole(
	t *testing.T,
	role Role,
	maxOps int,
	newFS func() *RoleFS,
	run func(fs vfs.FileSystem) error,
	check func(t *testing.T, n int, runErr error),
) {
	t.Helper()

	if maxOps <= 0 {
		maxOps = 512
	}

	var prev []string

	for n := 1; ; n++ {
		if n > maxOps {
			t.Fatalf("the sweep of role %q did not terminate within %d operations: the fault "+
				"kept firing, so the scenario performs more I/O each step or the driver is "+
				"not counting", role, maxOps)
			return
		}

		fs := newFS()
		fs.FailNthOpForRole(role, n)

		runErr := run(fs)
		trace := fs.Trace(role)

		if prev != nil {
			limit := min(n-1, min(len(trace), len(prev)))
			for i := 0; i < limit; i++ {
				if trace[i] == prev[i] {
					continue
				}
				t.Fatalf("the sweep of role %q is unsound: at step %d of run %d the role did "+
					"%q, and in the previous run it did %q. The role's operation sequence is "+
					"not stable, so \"the N-th operation\" names a different operation on each "+
					"run and this sweep proves nothing.",
					role, i+1, n, trace[i], prev[i])
				return
			}
		}
		prev = trace

		failedBefore := t.Failed()
		check(t, n, runErr)
		if !failedBefore && t.Failed() {
			// An ordinal only means anything against the exact scenario that
			// produced it. Print the structural key so the case can be pinned.
			if key, ok := fs.FiredKey(); ok {
				t.Logf("failure point %d has structural key %s: pin it as a regression test "+
					"with FailAtKey(vfstest.Key{Role: %q, Op: vfstest.%s, Nth: %d})",
					n, key, key.Role, opConstName(key.Op), key.Nth)
			}
		}

		if !fs.Fired() {
			// N ran off the end of the role's sequence: every one of its
			// operations has now been failed once, in turn.
			if n == 1 {
				t.Fatalf("role %q performed no I/O at all, so the sweep proved nothing", role)
			}
			return
		}
	}
}

// opConstName returns the Go constant name for an Op, so a logged failure can
// be pasted into a test without a lookup.
func opConstName(op Op) string {
	switch op {
	case OpOpen:
		return "OpOpen"
	case OpRemove:
		return "OpRemove"
	case OpRename:
		return "OpRename"
	case OpStat:
		return "OpStat"
	case OpMkdirAll:
		return "OpMkdirAll"
	case OpReadDir:
		return "OpReadDir"
	case OpRead:
		return "OpRead"
	case OpWrite:
		return "OpWrite"
	case OpSync:
		return "OpSync"
	case OpTruncate:
		return "OpTruncate"
	case OpClose:
		return "OpClose"
	}
	return "Op(0)"
}
