package query

import (
	"context"
	"sync"

	"github.com/dd0wney/graphdb/pkg/storage"
)

const (
	// MinStreamBufferSize is the minimum buffer size to prevent deadlocks
	MinStreamBufferSize = 10
	// DefaultStreamBufferSize is used when no size is specified
	DefaultStreamBufferSize = 100
)

// ResultStream provides streaming query results
type ResultStream struct {
	ch     chan *storage.Node
	errCh  chan error
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	// sendMu orders Send against the close of ch. Producer and consumer both
	// call Close, so ch can close while a Send is in flight, and a send on a
	// closed channel panics in the producer's goroutine, where no caller can
	// recover it. Send holds the read side; Close takes the write side only
	// after cancelling ctx, which makes a blocked Send return first.
	sendMu sync.RWMutex
	closed bool
}

// NewResultStream creates a new result stream.
// Enforces a minimum buffer size to prevent deadlocks with unbuffered channels.
func NewResultStream(bufferSize int) *ResultStream {
	// Enforce minimum buffer size to prevent deadlocks
	if bufferSize < MinStreamBufferSize {
		bufferSize = DefaultStreamBufferSize
	}

	// gosec G118 flags this as cancel-not-called, but cancel is stored on
	// the ResultStream struct (line below) and invoked via Close() →
	// rs.once.Do(rs.cancel). gosec's static analysis can't follow through
	// the struct field + sync.Once indirection.
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel called in Close() via sync.Once

	return &ResultStream{
		ch:     make(chan *storage.Node, bufferSize),
		errCh:  make(chan error, 1),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Next returns the next result or error
func (rs *ResultStream) Next() (*storage.Node, error) {
	// First, try to read from channel without blocking
	// This prioritizes draining buffered data before checking context cancellation
	select {
	case node, ok := <-rs.ch:
		if !ok {
			// Check for error
			select {
			case err := <-rs.errCh:
				return nil, err
			default:
				return nil, nil // End of stream
			}
		}
		return node, nil

	case err := <-rs.errCh:
		return nil, err

	default:
		// Nothing immediately available, now block on all cases including context
		select {
		case node, ok := <-rs.ch:
			if !ok {
				select {
				case err := <-rs.errCh:
					return nil, err
				default:
					return nil, nil
				}
			}
			return node, nil

		case err := <-rs.errCh:
			return nil, err

		case <-rs.ctx.Done():
			// Close cancels ctx and closes ch, so both cases can be ready at
			// once and select picks one at random. A producer that sent and
			// then closed must not lose its last nodes to that coin, so take
			// a node that is already buffered before reporting the cancel.
			select {
			case node, ok := <-rs.ch:
				if ok {
					return node, nil
				}
			default:
			}
			return nil, rs.ctx.Err()
		}
	}
}

// Send sends a result to the stream
func (rs *ResultStream) Send(node *storage.Node) bool {
	rs.sendMu.RLock()
	defer rs.sendMu.RUnlock()
	if rs.closed {
		return false
	}
	select {
	case rs.ch <- node:
		return true
	case <-rs.ctx.Done():
		return false
	}
}

// SendError sends an error and closes the stream
func (rs *ResultStream) SendError(err error) {
	select {
	case rs.errCh <- err:
	default:
	}
	rs.Close()
}

// Close closes the result stream
func (rs *ResultStream) Close() {
	rs.once.Do(func() {
		rs.cancel()
		rs.sendMu.Lock()
		rs.closed = true
		close(rs.ch)
		rs.sendMu.Unlock()
	})
}
