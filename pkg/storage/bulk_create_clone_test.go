package storage

import (
	"context"
	"sync/atomic"
	"testing"
)

// The bulk creates return only IDs, so a clone of each entity is needed only
// for an observer. On an 882k-node, 889k-edge batch the clones held 0.56 GB
// until the notify loop ran, with no observer registered (snare ask 5).
//
// A create always allocates, so neither test can ask for zero. Each compares
// two paths that differ only in whether a clone is owed, and requires the
// difference to be at least one measured clone.

const bulkCloneN = 200

var cloneSink atomic.Pointer[Node]
var edgeCloneSink atomic.Pointer[Edge]

func bulkCloneSpecs() []NodeSpec {
	specs := make([]NodeSpec, bulkCloneN)
	for i := range specs {
		specs[i] = NodeSpec{
			Labels: []string{"Function", "Exported"},
			Properties: map[string]Value{
				"name": StringValue("f"),
				"line": IntValue(int64(i)),
				"file": StringValue("a.go"),
			},
		}
	}
	return specs
}

// countingObserver records how many created nodes it saw, so the
// test can show that skipping the clone for no observer still delivers one to
// an observer.
type countingObserver struct {
	created atomic.Int64
}

func (o *countingObserver) OnNodeCreated(_ context.Context, n *Node) {
	if n != nil {
		o.created.Add(1)
	}
}
func (o *countingObserver) OnNodeUpdated(context.Context, *Node, *Node)   {}
func (o *countingObserver) OnNodeDeleted(context.Context, uint64, string) {}

func perNodeBulkAllocs(t *testing.T, gs *GraphStorage) float64 {
	t.Helper()
	specs := bulkCloneSpecs()
	return testing.AllocsPerRun(10, func() {
		if _, err := gs.CreateNodesWithTenant("t", specs); err != nil {
			t.Fatalf("CreateNodesWithTenant: %v", err)
		}
	}) / bulkCloneN
}

func TestCreateNodesWithTenant_NoObserverSkipsClone(t *testing.T) {
	sample := &Node{ID: 1, Labels: bulkCloneSpecs()[0].Labels, Properties: bulkCloneSpecs()[0].Properties}
	cloneAllocs := testing.AllocsPerRun(100, func() { cloneSink.Store(sample.Clone()) })
	if cloneAllocs < 2 {
		t.Fatalf("Node.Clone measured %v allocs; the comparison below needs a real clone cost", cloneAllocs)
	}

	plain, err := NewGraphStorageWithConfig(ephemeralConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	observed, err := NewGraphStorageWithConfig(ephemeralConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer observed.Close()
	obs := &countingObserver{}
	observed.AddObserver(obs)

	without := perNodeBulkAllocs(t, plain)
	with := perNodeBulkAllocs(t, observed)

	// AllocsPerRun makes one warm-up call plus 10 measured ones.
	if got, want := obs.created.Load(), int64(11*bulkCloneN); got != want {
		t.Errorf("observer saw %d created nodes, want %d", got, want)
	}
	t.Logf("per-node allocs: %.2f with an observer, %.2f without, clone %.0f", with, without, cloneAllocs)
	if with-without < cloneAllocs-0.5 {
		t.Errorf("per-node allocs: %.2f with an observer, %.2f without; difference %.2f, want at least one clone (%.0f)",
			with, without, with-without, cloneAllocs)
	}
}

func TestCreateEdgesWithTenant_SkipsClone(t *testing.T) {
	sample := &Edge{ID: 1, Type: "CALLS", Properties: map[string]Value{"line": IntValue(3)}}
	cloneAllocs := testing.AllocsPerRun(100, func() { edgeCloneSink.Store(sample.Clone()) })
	if cloneAllocs < 2 {
		t.Fatalf("Edge.Clone measured %v allocs; the comparison below needs a real clone cost", cloneAllocs)
	}

	// One store for each path, with the same history: on one shared store the
	// second path would grow adjacency slices that are already longer, and the
	// order alone would move the count.
	newStore := func() (*GraphStorage, []EdgeSpec) {
		gs, err := NewGraphStorageWithConfig(ephemeralConfig(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = gs.Close() })
		ids, err := gs.CreateNodesWithTenant("t", bulkCloneSpecs())
		if err != nil {
			t.Fatal(err)
		}
		specs := make([]EdgeSpec, bulkCloneN)
		for i := range specs {
			specs[i] = EdgeSpec{FromID: ids[i], ToID: ids[(i+1)%len(ids)], Type: "CALLS",
				Properties: map[string]Value{"line": IntValue(int64(i))}, Weight: 1}
		}
		return gs, specs
	}

	bulkStore, specs := newStore()
	bulk := testing.AllocsPerRun(10, func() {
		if _, err := bulkStore.CreateEdgesWithTenant("t", specs); err != nil {
			t.Fatalf("CreateEdgesWithTenant: %v", err)
		}
	}) / bulkCloneN
	// The single create returns the edge to its caller, so it owes a clone.
	singleStore, specs := newStore()
	single := testing.AllocsPerRun(10, func() {
		for _, s := range specs {
			if _, err := singleStore.CreateEdgeWithTenant("t", s.FromID, s.ToID, s.Type, s.Properties, s.Weight); err != nil {
				t.Fatalf("CreateEdgeWithTenant: %v", err)
			}
		}
	}) / bulkCloneN

	t.Logf("per-edge allocs: %.2f single, %.2f bulk, clone %.0f", single, bulk, cloneAllocs)
	if single-bulk < cloneAllocs-0.5 {
		t.Errorf("per-edge allocs: %.2f single, %.2f bulk; difference %.2f, want at least one clone (%.0f)",
			single, bulk, single-bulk, cloneAllocs)
	}
}
