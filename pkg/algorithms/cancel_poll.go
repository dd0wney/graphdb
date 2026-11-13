package algorithms

import "context"

// cancelCheckInterval is how many loop items pass between two ctx.Err() calls.
// It is a power of two so the test is a mask, not a division.
const cancelCheckInterval = 1024

// cancelPoll lets a hot loop honour a context without a context call on every
// item. The call is not free: it is an interface dispatch and an atomic load,
// and it would run once per node or edge in the tightest loops. One check for
// each 1024 items still stops a run in good time, because 1024 items take
// microseconds to milliseconds. The cost here is one increment and one compare
// per item.
type cancelPoll struct {
	ctx context.Context
	n   uint32
}

// tick counts one item and checks the context once per cancelCheckInterval.
func (p *cancelPoll) tick() error {
	p.n++
	if p.n&(cancelCheckInterval-1) != 0 {
		return nil
	}
	return p.ctx.Err()
}
