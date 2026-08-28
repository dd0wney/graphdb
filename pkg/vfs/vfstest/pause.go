package vfstest

import (
	"sync"
	"testing"
	"time"
)

// Pause parks one actor's operation so a test can look at the suspended state
// and then decide what that operation returns.
//
// A sweep asks "what happens if this operation fails?". A barrier asks "what
// does everyone else see while this operation is in flight?". The second
// question is the one a background worker raises, and a single-threaded sweep
// cannot pose it at all.
type Pause struct {
	reached chan struct{}
	release chan error

	arriveOnce  sync.Once
	releaseOnce sync.Once
}

func newPause() *Pause {
	return &Pause{
		reached: make(chan struct{}),
		// Buffered, so Release never blocks even when the actor never arrives.
		release: make(chan error, 1),
	}
}

// arrive is called by the parked actor. It signals the waiting test, then
// blocks until Release.
func (p *Pause) arrive() error {
	p.arriveOnce.Do(func() { close(p.reached) })
	return <-p.release
}

// Wait blocks the test until an actor reaches the parked operation.
//
// It fails the test rather than hanging when no actor arrives. A barrier that
// hangs turns a wrong scenario into a suite that never finishes, and the cause
// is then invisible.
func (p *Pause) Wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-p.reached:
	case <-time.After(timeout):
		t.Fatalf("vfstest: no actor reached the parked operation within %s: either the role "+
			"or the operation number is wrong, or the actor never ran", timeout)
	}
}

// Release resumes the parked actor. Pass an error to make the parked operation
// report it, or nil to let the operation proceed. A second call does nothing.
func (p *Pause) Release(err error) {
	p.releaseOnce.Do(func() { p.release <- err })
}
