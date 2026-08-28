package vfstest

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Explore runs a scenario under a range of seeds, each with its own schedule
// perturbation and its own choice of failure point. It returns the structural
// key of every seed that failed.
//
// This is the other half of SweepRole. The sweep is exhaustive over one
// actor's operations and it is deterministic, which makes it a regression test
// and caps it at the interleavings the scenario happens to produce. Explore is
// neither exhaustive nor reproducible on its own, and it reaches states no
// scripted schedule was written for.
//
// The two are only useful together, and the join is the Key. When a seed
// fails, Explore reports the key of the operation that fired, and that key
// becomes a FailAtKey regression test which depends on neither the seed, nor
// the scheduler, nor the jitter. The fuzzer roams; the interesting cases get
// pinned.
//
// The returned keys are what a caller acts on. The log lines are for a human
// reading a CI failure, and a test that wants to assert on the outcome should
// use the return value rather than the log.
func Explore(
	t *testing.T,
	seeds int,
	newFS func(seed int64) *RoleFS,
	run func(fs vfs.FileSystem) error,
	check func(t *testing.T, seed int64, runErr error),
) []Key {
	t.Helper()

	var found []Key

	for seed := int64(0); seed < int64(seeds); seed++ {
		fs := newFS(seed)

		failedBefore := t.Failed()
		runErr := run(fs)
		check(t, seed, runErr)

		if failedBefore || !t.Failed() {
			continue
		}

		key, ok := fs.FiredKey()
		if !ok {
			// Nothing was armed, so the defect is in the schedule rather than
			// in an error path. That is a different finding and it needs a
			// different tool; say so instead of reporting a key that is not
			// there.
			t.Logf("seed %d failed with no fault armed, so the defect is in the schedule "+
				"rather than in an error path: re-run with -race", seed)
			continue
		}

		found = append(found, key)
		t.Logf("seed %d failed at %s. Pin it as a deterministic regression test:\n"+
			"    fs.FailAtKey(vfstest.Key{Role: %q, Op: vfstest.%s, Nth: %d})",
			seed, key, key.Role, opConstName(key.Op), key.Nth)
	}

	return found
}
