// Package faultsim makes unreachable error paths reachable.
//
// Some failures cannot be provoked from outside. A WAL cannot be made to
// exhaust a 64-bit LSN space; a goroutine cannot be made to fail to start; a
// guard written for an invariant that holds cannot be entered at all. The code
// that handles those cases is therefore never executed, never measured, and
// never known to work — and it is exactly the code that runs on the worst day.
//
// SQLite solves this with sqlite3FaultSim(int eCode): a function that returns
// false in production and can be made to return true for a chosen site, with
// the control surface compiled into every build. Its own example is
// pthread_create, where the recovery path is otherwise untestable:
//
//	if( sqlite3FaultSim(200) ){ rc = 1; }
//	else{ rc = pthread_create(&p->tid, 0, xTask, pIn); }
//
// The talk is explicit about why the control surface ships rather than hiding
// behind a build tag: "Included in all builds. Fly what you test."
//
// # Cost
//
// Fail is one atomic load when nothing is armed, which is the production case
// always. Nothing is allocated and no lock is taken on that path.
//
// # Not for production use
//
// Arming a fault in a running server makes it fail on purpose. The API is
// exported because a test in another package must reach it, which is the same
// reason sqlite3_test_control is published and documented as testing-only.
package faultsim

import (
	"sync"
	"sync/atomic"
)

// Site identifies a fault-injection point. Add a constant here when you add a
// call to Fail, and give it a name that says where it is.
type Site int

const (
	// None is the zero value and never fires.
	None Site = iota

	// WALLSNExhausted is the LSN-space overflow guard in wal.Append. Reaching
	// it honestly requires 2^64 appends.
	WALLSNExhausted

	// WALRotateReopen is the recovery branch in wal.Truncate that reopens the
	// old file after a failed rename. It runs only when a rename fails after
	// the replacement file was already created.
	WALRotateReopen

	// numSites bounds the state array. Keep it last.
	numSites
)

func (s Site) String() string {
	switch s {
	case WALLSNExhausted:
		return "WALLSNExhausted"
	case WALRotateReopen:
		return "WALRotateReopen"
	default:
		return "None"
	}
}

type siteState struct {
	// armed means this site should fail. nth, when non-zero, restricts firing
	// to the nth call.
	armed bool
	nth   int
	calls int
	fired int
}

var (
	// anyArmed is the production fast path: one atomic load, no lock.
	anyArmed atomic.Bool

	mu    sync.Mutex
	sites [numSites]siteState
)

// Fail reports whether the given site should fail now.
//
// It returns false in production. A site that is armed with a positive nth
// fires only on that call, counting from 1, which is how a sweep walks the
// failure through a sequence of attempts.
func Fail(s Site) bool {
	if !anyArmed.Load() {
		return false
	}
	if s <= None || s >= numSites {
		return false
	}

	mu.Lock()
	defer mu.Unlock()

	st := &sites[s]
	st.calls++
	if !st.armed {
		return false
	}
	if st.nth > 0 && st.calls != st.nth {
		return false
	}
	st.fired++
	return true
}

// Arm makes a site fail. nth restricts firing to that call number, counting
// from 1; zero means every call.
func Arm(s Site, nth int) {
	if s <= None || s >= numSites {
		return
	}
	mu.Lock()
	sites[s] = siteState{armed: true, nth: nth}
	mu.Unlock()
	anyArmed.Store(true)
}

// Disarm stops a site failing and clears its counters.
func Disarm(s Site) {
	if s <= None || s >= numSites {
		return
	}
	mu.Lock()
	sites[s] = siteState{}
	any := false
	for i := range sites {
		if sites[i].armed {
			any = true
			break
		}
	}
	mu.Unlock()
	anyArmed.Store(any)
}

// Reset disarms every site. A test that arms a fault should defer this, because
// the state is process-wide.
func Reset() {
	mu.Lock()
	for i := range sites {
		sites[i] = siteState{}
	}
	mu.Unlock()
	anyArmed.Store(false)
}

// Calls reports how many times a site was reached SINCE SOMETHING WAS ARMED.
//
// It does not count while nothing is armed. Fail returns on a single atomic
// load in that case and records nothing, which is what keeps the production
// cost at zero; counting unconditionally would put a lock on a path every
// caller pays for and no caller uses. A test therefore arms first and reads
// Calls afterwards, which is the order a test needs anyway.
//
// This is the negative control, and it is not optional. A test that arms a
// fault and sees the expected error has proved nothing unless the site was
// actually executed — the same error can arrive from anywhere. Assert Calls > 0
// alongside the error, or the test passes when the code under test never ran.
func Calls(s Site) int {
	if s <= None || s >= numSites {
		return 0
	}
	mu.Lock()
	defer mu.Unlock()
	return sites[s].calls
}

// Fired reports how many times a site actually failed.
func Fired(s Site) int {
	if s <= None || s >= numSites {
		return 0
	}
	mu.Lock()
	defer mu.Unlock()
	return sites[s].fired
}
