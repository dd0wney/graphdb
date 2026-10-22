package query

import (
	"errors"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Close cancels the stream's context and then closes its channel. A consumer
// blocked in Next could wake on the cancel first and report context.Canceled
// for a stream that ended normally, or for one that ended with SendError, so a
// caller could not tell a clean end, a real error and a cancel apart. The race
// is narrow, so each test runs many short streams.
const streamEndRounds = 3000

func TestResultStream_CleanEndIsNotReportedAsAnError(t *testing.T) {
	for i := 0; i < streamEndRounds; i++ {
		s := NewResultStream(10)
		go func() {
			s.Send(&storage.Node{ID: 1})
			s.Close()
		}()
		if n, err := s.Next(); err != nil || n == nil {
			t.Fatalf("round %d: first Next = (%v, %v), want the node", i, n, err)
		}
		if n, err := s.Next(); err != nil || n != nil {
			t.Fatalf("round %d: Next at a clean end = (%v, %v), want (nil, nil)", i, n, err)
		}
	}
}

func TestResultStream_SendErrorIsNotReportedAsACancel(t *testing.T) {
	sentinel := errors.New("producer failed")
	for i := 0; i < streamEndRounds; i++ {
		s := NewResultStream(10)
		go func() {
			s.Send(&storage.Node{ID: 1})
			s.SendError(sentinel)
		}()
		var err error
		for {
			var n *storage.Node
			n, err = s.Next()
			if err != nil || n == nil {
				break
			}
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("round %d: stream ended with %v, want the producer's error", i, err)
		}
	}
}
