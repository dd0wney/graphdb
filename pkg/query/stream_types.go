package query

import (
	"context"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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
}

// NewResultStream creates a new result stream.
// Enforces a minimum buffer size to prevent deadlocks with unbuffered channels.
func NewResultStream(bufferSize int) *ResultStream {
	// Enforce minimum buffer size to prevent deadlocks
	if bufferSize < MinStreamBufferSize {
		bufferSize = DefaultStreamBufferSize
	}

	ctx, cancel := context.WithCancel(context.Background())

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
			return nil, rs.ctx.Err()
		}
	}
}

// Send sends a result to the stream
func (rs *ResultStream) Send(node *storage.Node) bool {
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
		close(rs.ch)
		rs.cancel()
	})
}
