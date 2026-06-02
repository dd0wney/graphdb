package storage

import (
	"fmt"
	"testing"
	"time"
)

// Track P item (1) — the batched WAL must release gs.mu during the durability
// wait so concurrent writers can fill one batch (group commit).
//
// Before the fix, a write method holds gs.mu for its whole critical section,
// including BatchedWAL.Append parking on its done-channel. A second writer
// therefore cannot acquire gs.mu to enqueue, the batch never reaches batchSize,
// and both writes serialize behind the flush ticker.
//
// These are deterministic STRUCTURAL tests (the functional WAL behaviour is
// already correct). batchSize=2 with a long flushInterval: two concurrent
// writes can only complete quickly if the batch fills — which can only happen
// if each writer releases gs.mu before waiting on durability. On the pre-fix
// code the batch cannot fill and the writes block until the 10s ticker fires,
// so the 3s deadline trips.

// newBatchedGS builds a batched-WAL storage with the given batch size and flush
// interval. A long flushInterval makes the ticker, not group commit, the slow
// path — so a fast completion proves the batch filled.
func newBatchedGS(t *testing.T, batchSize int, flush time.Duration) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:        t.TempDir(),
		EnableBatching: true,
		BatchSize:      batchSize,
		FlushInterval:  flush,
	})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	return gs
}

// createNodesConcurrently creates n nodes using n concurrent writers so the
// batch fills and flushes immediately (CreateNode is group-commit converted).
// Used to seed prerequisites for the update/delete/edge tests without waiting
// on the ticker. n >= batchSize so the batch fills.
func createNodesConcurrently(t *testing.T, gs *GraphStorage, n int) []uint64 {
	t.Helper()
	type res struct {
		id  uint64
		err error
	}
	ch := make(chan res, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			node, err := gs.CreateNodeWithTenant("t0", []string{"Doc"},
				map[string]Value{"name": StringValue(fmt.Sprintf("n%d", i))})
			if err != nil {
				ch <- res{0, err}
				return
			}
			ch <- res{node.ID, nil}
		}(i)
	}
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("createNodesConcurrently: %v", r.err)
		}
		ids = append(ids, r.id)
	}
	return ids
}

// createEdgesConcurrently creates n edges between the given endpoints using n
// concurrent writers so the batch fills immediately (CreateEdge is group-commit
// converted). Returns the new edge IDs.
func createEdgesConcurrently(t *testing.T, gs *GraphStorage, from, to uint64, n int) []uint64 {
	t.Helper()
	type res struct {
		id  uint64
		err error
	}
	ch := make(chan res, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			edge, err := gs.CreateEdgeWithTenant("t0", from, to, "LINKS",
				map[string]Value{"i": StringValue(fmt.Sprintf("e%d", i))}, 1.0)
			if err != nil {
				ch <- res{0, err}
				return
			}
			ch <- res{edge.ID, nil}
		}(i)
	}
	ids := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		r := <-ch
		if r.err != nil {
			t.Fatalf("createEdgesConcurrently: %v", r.err)
		}
		ids = append(ids, r.id)
	}
	return ids
}

// awaitN waits for n results on done, failing if fewer than n arrive within the
// deadline (the signature of writers serialized under gs.mu).
func awaitN(t *testing.T, done <-chan error, n int, within time.Duration, failMsg string) {
	t.Helper()
	deadline := time.After(within)
	for got := 0; got < n; got++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("op error: %v", err)
			}
		case <-deadline:
			t.Fatalf("only %d/%d concurrent ops completed in %s — %s", got, n, within, failMsg)
		}
	}
}

// TestBatchedWAL_ConcurrentCreatesFillBatchWithoutHoldingGlobalLock covers the
// create path (the first path converted).
func TestBatchedWAL_ConcurrentCreatesFillBatchWithoutHoldingGlobalLock(t *testing.T) {
	gs := newBatchedGS(t, 2, 10*time.Second)
	defer gs.Close()

	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(n int) {
			_, err := gs.CreateNodeWithTenant("t0", []string{"Doc"},
				map[string]Value{"name": StringValue(fmt.Sprintf("n%d", n))})
			done <- err
		}(i)
	}
	awaitN(t, done, 2, 3*time.Second,
		"concurrent CreateNode writers are serialized under gs.mu (the batch cannot fill)")
}

// TestBatchedWAL_GroupCommit_NodeUpdateAndDelete covers UpdateNode + DeleteNode.
// Concurrent update(id0) + delete(id1) fill batchSize=2 only if BOTH release
// gs.mu during the durability wait.
func TestBatchedWAL_GroupCommit_NodeUpdateAndDelete(t *testing.T) {
	gs := newBatchedGS(t, 2, 10*time.Second)
	defer gs.Close()

	ids := createNodesConcurrently(t, gs, 2)

	done := make(chan error, 2)
	go func() { done <- gs.UpdateNode(ids[0], map[string]Value{"k": StringValue("v")}) }()
	go func() { done <- gs.DeleteNode(ids[1]) }()

	awaitN(t, done, 2, 3*time.Second,
		"concurrent UpdateNode/DeleteNode are serialized under gs.mu (the batch cannot fill)")
}

// TestBatchedWAL_GroupCommit_EdgeCreate covers CreateEdge.
func TestBatchedWAL_GroupCommit_EdgeCreate(t *testing.T) {
	gs := newBatchedGS(t, 2, 10*time.Second)
	defer gs.Close()

	nodes := createNodesConcurrently(t, gs, 2)

	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(n int) {
			_, err := gs.CreateEdgeWithTenant("t0", nodes[0], nodes[1], "LINKS",
				map[string]Value{"i": StringValue(fmt.Sprintf("e%d", n))}, 1.0)
			done <- err
		}(i)
	}
	awaitN(t, done, 2, 3*time.Second,
		"concurrent CreateEdge writers are serialized under gs.mu (the batch cannot fill)")
}

// TestBatchedWAL_GroupCommit_EdgeUpdateAndDelete covers UpdateEdge + DeleteEdge.
func TestBatchedWAL_GroupCommit_EdgeUpdateAndDelete(t *testing.T) {
	gs := newBatchedGS(t, 2, 10*time.Second)
	defer gs.Close()

	nodes := createNodesConcurrently(t, gs, 2)
	edges := createEdgesConcurrently(t, gs, nodes[0], nodes[1], 2)

	w := 2.0
	done := make(chan error, 2)
	go func() { done <- gs.UpdateEdge(edges[0], map[string]Value{"k": StringValue("v")}, &w) }()
	go func() { done <- gs.DeleteEdge(edges[1]) }()

	awaitN(t, done, 2, 3*time.Second,
		"concurrent UpdateEdge/DeleteEdge are serialized under gs.mu (the batch cannot fill)")
}
