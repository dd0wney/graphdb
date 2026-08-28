// Package alloctest provides allocators that refuse on purpose, and the two
// loops SQLite runs against them.
//
// It is an ordinary package rather than a set of _test.go helpers, for the same
// reason pkg/vfs/vfstest is: a test in any package installs one through the
// published API a production caller would use, so the code under test is the
// code that ships.
package alloctest

import (
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
)

// Mode selects which of SQLite's two loops an allocator implements.
type Mode int

const (
	// FailOnce refuses the N-th allocation and then behaves normally. It finds
	// handlers that cope with a single failure and then continue.
	FailOnce Mode = iota

	// FailAllFrom refuses the N-th allocation and every one after it. It finds
	// the handlers that only work when a later allocation succeeds — a cleanup
	// path that allocates in order to clean up, for instance.
	FailAllFrom
)

func (m Mode) String() string {
	if m == FailAllFrom {
		return "fail-all-from"
	}
	return "fail-once"
}

// Failing refuses allocations according to a mode and a threshold.
type Failing struct {
	mu   sync.Mutex
	mode Mode
	nth  int
	seen int
}

// New returns an allocator that refuses starting at the nth allocation,
// counting from 1.
func New(mode Mode, nth int) *Failing {
	return &Failing{mode: mode, nth: nth}
}

func (f *Failing) Bytes(n int) ([]byte, error) {
	f.mu.Lock()
	f.seen++
	seen := f.seen
	mode, nth := f.mode, f.nth
	f.mu.Unlock()

	switch mode {
	case FailAllFrom:
		if nth > 0 && seen >= nth {
			return nil, alloc.ErrNoMemory
		}
	default:
		if nth > 0 && seen == nth {
			return nil, alloc.ErrNoMemory
		}
	}
	return make([]byte, n), nil
}

func (f *Failing) Name() string { return "alloctest." + f.mode.String() }

// Seen reports how many requests reached this allocator.
func (f *Failing) Seen() int { f.mu.Lock(); defer f.mu.Unlock(); return f.seen }

// Sweep runs one of SQLite's two out-of-memory loops.
//
//	Rig the allocator to fail once on the N-th allocation for N=1,2,3,....
//	Repeat until no memory allocations fail.
//
// and the same walk for "fail all allocations beginning with the N-th". The
// termination condition is what makes it a proof rather than a sample: when a
// run completes with no allocation refused, N has passed the last allocation
// the scenario makes, and every allocation failure it can experience has been
// tried.
//
// run performs the scenario and returns whatever error it produced; a refused
// allocation is an expected outcome. check runs after every step and holds the
// invariant — typically that the store is still usable, that nothing was
// silently dropped, and that nothing panicked.
func Sweep(t *testing.T, mode Mode, maxAllocs int, run func() error, check func(t *testing.T, n int, runErr error)) {
	t.Helper()

	if maxAllocs <= 0 {
		maxAllocs = 256
	}

	for n := 1; ; n++ {
		if n > maxAllocs {
			t.Fatalf("%s sweep did not terminate within %d allocations", mode, maxAllocs)
		}

		f := New(mode, n)
		alloc.Install(f)
		err := run()
		refused := alloc.Refused()
		alloc.Reset()

		check(t, n, err)

		if refused == 0 {
			if n == 1 {
				t.Fatalf("%s sweep: the scenario made no gated allocation, so it proved nothing", mode)
			}
			return
		}
	}
}
