// Package alloc is graphdb's allocator seam for out-of-memory testing.
//
// SQLite tests its out-of-memory paths by substituting the allocator through
// sqlite3_config(SQLITE_CONFIG_MALLOC, ...) and running two loops:
//
//	Rig the allocator to fail once on the N-th allocation for N=1,2,3,....
//	Repeat until no memory allocations fail.
//
//	Rig the allocator to fail all allocations beginning with the N-th, for
//	N=1,2,3,.... Repeat until no memory allocations fail.
//
// # What this can and cannot claim
//
// Go cannot substitute malloc. A Go program that exhausts memory is killed by
// the runtime or the OOM killer; there is no error to return and no stack to
// unwind. Any package claiming to test "out of memory" in Go is claiming
// something narrower, so this one says exactly what it covers:
//
//   - Covered: graphdb's own large, length-driven buffers — the ones sized
//     from a record header, a snapshot section, or a query result. Those have
//     real failure paths, because the size comes from data and the data can be
//     wrong or simply too big for a budget.
//   - Not covered: every implicit allocation Go makes — slice growth, map
//     rehashing, interface boxing, goroutine stacks. Nothing here touches them.
//
// That is a smaller claim than SQLite's, and it is the honest one. The value is
// the same in kind: an error path that no test can otherwise reach gets
// executed, and a budget becomes enforceable.
//
// # Cost
//
// Bytes is one atomic load and a make when nothing is installed, which is the
// production case always.
package alloc

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// ErrNoMemory is returned when an installed allocator refuses a request. It is
// the error a caller must handle; graphdb treats it like any other failure to
// obtain a resource, not like corruption.
var ErrNoMemory = fmt.Errorf("alloc: allocation refused")

// Allocator supplies byte buffers.
//
// An implementation must be safe for concurrent use.
type Allocator interface {
	// Bytes returns a buffer of exactly n bytes, or an error.
	Bytes(n int) ([]byte, error)
	// Name identifies the allocator for diagnostics.
	Name() string
}

var (
	// installed is the production fast path: one atomic load.
	installed atomic.Bool

	mu     sync.Mutex
	custom Allocator
	// allocs counts requests while an allocator is installed. As with
	// faultsim.Calls, counting is skipped on the production path so callers
	// pay nothing for a feature they do not use.
	allocs  int
	refused int
)

// Bytes returns a buffer of n bytes.
//
// With no allocator installed it is a plain make, and the error is always nil.
// Callers must still handle the error: that is the point, because the handler
// is the code this package exists to execute.
func Bytes(n int) ([]byte, error) {
	if !installed.Load() {
		return make([]byte, n), nil
	}

	mu.Lock()
	a := custom
	allocs++
	mu.Unlock()

	if a == nil {
		return make([]byte, n), nil
	}
	buf, err := a.Bytes(n)
	if err != nil {
		mu.Lock()
		refused++
		mu.Unlock()
		return nil, err
	}
	return buf, nil
}

// Install replaces the allocator. Passing nil restores the default.
//
// The state is process-wide, exactly as sqlite3_config's is. A test that
// installs one must defer Reset, or every later test in the same binary
// inherits it.
func Install(a Allocator) {
	mu.Lock()
	custom = a
	allocs, refused = 0, 0
	mu.Unlock()
	installed.Store(a != nil)
}

// Reset restores the default allocator and clears the counters.
func Reset() { Install(nil) }

// Allocs reports how many requests were made since an allocator was installed.
//
// This is the negative control. A test that installs a failing allocator and
// sees the expected error has proved nothing unless an allocation was actually
// attempted — the same error can arrive from elsewhere. Assert Allocs > 0.
func Allocs() int { mu.Lock(); defer mu.Unlock(); return allocs }

// Refused reports how many requests the installed allocator rejected.
func Refused() int { mu.Lock(); defer mu.Unlock(); return refused }
