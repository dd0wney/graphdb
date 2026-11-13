package algorithms

import "context"

// cancelCheckInterval is how many loop items pass between two ctx.Err() calls.
// It is a power of two so the test is a mask, not a division.
const cancelCheckInterval = 1024

// cancelPoll lets a hot loop honour a context without paying for ctx.Err() on
// every item. ctx.Err() takes a mutex on a cancellable context, so calling it
// per node or per edge would show in a PageRank profile. The cost here is one
// increment and one compare per item.
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
