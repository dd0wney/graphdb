package storage

import (
	"errors"
	"math"
	"testing"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// walTestNode is big enough that an encode cannot hide in a small-object
// allocation the runtime might elide.
func walTestNode() *Node {
	return &Node{
		ID:     1,
		Labels: []string{"Function"},
		Properties: map[string]Value{
			"name": StringValue("a reasonably long property value for the payload"),
			"path": StringValue("/src/pkg/storage/node_operations.go"),
		},
	}
}

// A store with no WAL sink has nowhere to put an encoded record, so encoding
// one is pure garbage: on a 882k-node bulk create it was 0.66 GB that nothing
// read (snare ask 4). Both write paths must skip the encode, not only the
// append.
func TestWALWrite_NoSinkAllocatesNothing(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(ephemeralConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	node := walTestNode()

	enqueue := testing.AllocsPerRun(100, func() {
		if p := gs.enqueueWAL(wal.OpCreateNode, node); p != nil {
			t.Fatal("enqueueWAL with no sink returned a pending handle")
		}
	})
	if enqueue != 0 {
		t.Errorf("enqueueWAL with no WAL sink: %v allocs per call, want 0", enqueue)
	}

	write := testing.AllocsPerRun(100, func() {
		if err := gs.writeToWALWithError(wal.OpCreateNode, node); err != nil {
			t.Fatalf("writeToWALWithError with no sink: %v", err)
		}
	})
	if write != 0 {
		t.Errorf("writeToWALWithError with no WAL sink: %v allocs per call, want 0", write)
	}
}

// The positive control for the test above: with a WAL, the same call must
// allocate, or a 0 there could mean the measurement never saw the encode.
func TestWALWrite_WithSinkAllocates(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(jsonConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	node := walTestNode()

	allocs := testing.AllocsPerRun(20, func() {
		if err := gs.writeToWALWithError(wal.OpCreateNode, node); err != nil {
			t.Fatalf("writeToWALWithError: %v", err)
		}
	})
	if allocs == 0 {
		t.Fatal("writeToWALWithError with a WAL allocated nothing; the no-sink test cannot be trusted")
	}
}

// Skipping the encode on a WAL-less store also skips the one place a
// non-finite weight used to fail (json.Marshal refuses NaN and Inf). The
// refusal must come from validateEdgeWeight, ahead of the WAL, so that it does
// not depend on whether the store has a WAL.
func TestCreateEdge_NonFiniteWeight_RejectedWithoutWAL(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(ephemeralConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	a, err := gs.CreateNode([]string{"N"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := gs.CreateNode([]string{"N"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := gs.CreateEdge(a.ID, b.ID, "E", nil, w); !errors.Is(err, ErrInvalidEdgeWeight) {
			t.Errorf("CreateEdge(weight=%v) on a WAL-less store = %v, want ErrInvalidEdgeWeight", w, err)
		}
	}
}
