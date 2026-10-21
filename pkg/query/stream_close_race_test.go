package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A producer that sends and then closes, as StreamNodes does, used to lose a
// buffered node now and then: Close cancelled the context as well as closing
// the channel, and Next's blocking select picked ctx.Done at random over a
// ready receive. TestStreamingQuery_StreamNodes saw "Expected 2 nodes, got 1"
// in CI. Many short streams make the window near certain to be hit.
func TestResultStream_CloseAfterSendDropsNothing(t *testing.T) {
	const rounds, perRound = 20000, 2
	for r := 0; r < rounds; r++ {
		rs := NewResultStream(100)
		go func() {
			defer rs.Close()
			for i := 0; i < perRound; i++ {
				if !rs.Send(&storage.Node{ID: uint64(i + 1)}) {
					return
				}
			}
		}()
		got := 0
		for {
			n, _ := rs.Next()
			if n == nil {
				break
			}
			got++
		}
		if got != perRound {
			t.Fatalf("round %d: received %d nodes, want %d", r, got, perRound)
		}
	}
}

// A consumer that stops early calls Close while the producer may be inside
// Send. Close used to close the channel under the producer, and a send on a
// closed channel panics in the producer's goroutine, which no caller can
// recover: the process dies.
func TestResultStream_ConsumerCloseDuringSendDoesNotPanic(t *testing.T) {
	const rounds = 5000
	for r := 0; r < rounds; r++ {
		rs := NewResultStream(MinStreamBufferSize)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := uint64(1); ; i++ {
				if !rs.Send(&storage.Node{ID: i}) {
					return
				}
			}
		}()
		if n, _ := rs.Next(); n == nil {
			t.Fatalf("round %d: first Next gave no node", r)
		}
		rs.Close()
		<-done
	}
}
