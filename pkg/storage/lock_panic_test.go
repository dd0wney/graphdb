package storage

import (
	"testing"
	"time"
)

// injectedPanic is the value the hook panics with, so the harness can tell its
// own panic from a real one.
type injectedPanic struct{ site string }

// lockReleaseTimeout bounds each call after the injected panic. A lock left
// held makes the call block for ever, so the timeout is the failure signal.
const lockReleaseTimeout = 5 * time.Second

// assertPanicReleasesLock makes the critical section at site panic during op,
// then proves the lock was released: next (a second operation that takes the
// same lock) and Close must both return.
//
// The panic must come from the hook. If op returns without it, the site name is
// wrong or op does not reach the section, and the test would otherwise pass
// without testing anything.
func assertPanicReleasesLock(t *testing.T, gs *GraphStorage, site string, op func(), next func() error) {
	t.Helper()
	lockPanicHook = func(s string) {
		if s == site {
			panic(injectedPanic{site: s})
		}
	}
	defer func() { lockPanicHook = nil }()

	if !panicked(op, site) {
		t.Fatalf("op did not reach panic point %q", site)
	}
	lockPanicHook = nil

	if err := withinTimeout(next); err != nil {
		t.Fatalf("second operation after a panic at %q: %v", site, err)
	}
	if err := withinTimeout(gs.Close); err != nil {
		t.Fatalf("Close after a panic at %q: %v", site, err)
	}
}

// panicked runs op and reports whether it panicked with the injected value for
// site. Any other panic is re-raised: it is a real defect, not the test's.
func panicked(op func(), site string) (hit bool) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if p, ok := r.(injectedPanic); ok && p.site == site {
			hit = true
			return
		}
		panic(r)
	}()
	op()
	return false
}

// errLockHeld is what withinTimeout returns when fn does not return in time.
type errLockHeld struct{}

func (errLockHeld) Error() string {
	return "did not return within " + lockReleaseTimeout.String() + "; the lock is still held"
}

func withinTimeout(fn func() error) error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(lockReleaseTimeout):
		return errLockHeld{}
	}
}

// newPanicTestStore opens a store with one node. The second operation of each
// test writes the same node again, so it needs gs.mu and the node's shard
// lock, which are the locks a panic could leave held.
func newPanicTestStore(t *testing.T) (*GraphStorage, *Node) {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	n, err := gs.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(1)})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	return gs, n
}

// updateAgain is the second operation for a test whose store holds n.
func updateAgain(gs *GraphStorage, n *Node) func() error {
	return func() error { return gs.UpdateNode(n.ID, map[string]Value{"k": IntValue(9)}) }
}

func TestLockPanic_PatchNode(t *testing.T) {
	for _, site := range []string{"patchNode", "materializeNode", "patchNode.shard"} {
		t.Run(site, func(t *testing.T) {
			gs, n := newPanicTestStore(t)
			assertPanicReleasesLock(t, gs, site,
				func() { _ = gs.UpdateNode(n.ID, map[string]Value{"k": IntValue(3)}) },
				updateAgain(gs, n))
		})
	}
}

func TestLockPanic_BatchCommit(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "Batch.Commit",
		func() {
			batch := gs.BeginBatch()
			batch.UpdateNode(n.ID, map[string]Value{"k": IntValue(3)})
			_ = batch.Commit()
		},
		updateAgain(gs, n))
}

func TestLockPanic_TransactionCommit(t *testing.T) {
	for _, site := range []string{"Transaction.Commit", "materializeNode", "Transaction.Commit.shard", "Transaction.Commit.wal"} {
		t.Run(site, func(t *testing.T) {
			gs, n := newPanicTestStore(t)
			assertPanicReleasesLock(t, gs, site,
				func() {
					tx, err := gs.BeginTransaction()
					if err != nil {
						t.Fatalf("BeginTransaction: %v", err)
					}
					if err := tx.UpdateNode(n.ID, map[string]Value{"k": IntValue(3)}); err != nil {
						t.Fatalf("tx.UpdateNode: %v", err)
					}
					_ = tx.Commit()
				},
				updateAgain(gs, n))
		})
	}
}

func TestLockPanic_CreateNode(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "CreateNodeWithTenant",
		func() { _, _ = gs.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(3)}) },
		updateAgain(gs, n))
}

func TestLockPanic_CreateNodesWithTenant(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "CreateNodesWithTenant",
		func() {
			_, _ = gs.CreateNodesWithTenant(DefaultTenantID, []NodeSpec{{Labels: []string{"P"}, Properties: map[string]Value{"k": IntValue(3)}}})
		},
		updateAgain(gs, n))
}

func TestLockPanic_CreateNodeWithUniqueProperty(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "CreateNodeWithUniquePropertyForTenant",
		func() {
			_, _ = gs.CreateNodeWithUniquePropertyForTenant(DefaultTenantID, []string{"P"}, map[string]Value{"k": IntValue(3)}, "P", "k")
		},
		updateAgain(gs, n))
}

func TestLockPanic_DeleteNode(t *testing.T) {
	for _, site := range []string{"DeleteNode", "DeleteNode.shard"} {
		t.Run(site, func(t *testing.T) {
			gs, n := newPanicTestStore(t)
			assertPanicReleasesLock(t, gs, site,
				func() { _ = gs.DeleteNode(n.ID) },
				updateAgain(gs, n))
		})
	}
}

func TestLockPanic_DeleteAllNodes(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "DeleteAllNodes",
		func() { _ = gs.DeleteAllNodes() },
		updateAgain(gs, n))
}

// PatchNodeForTenant and DeleteNodeForTenant share checkNodeTenant, which holds
// a shard read lock. A leaked read lock blocks the shard write in updateAgain.
func TestLockPanic_NodeTenantCheck(t *testing.T) {
	ops := map[string]func(gs *GraphStorage, n *Node){
		"PatchNodeForTenant": func(gs *GraphStorage, n *Node) {
			_ = gs.PatchNodeForTenant(n.ID, map[string]Value{"k": IntValue(3)}, nil, DefaultTenantID)
		},
		"DeleteNodeForTenant": func(gs *GraphStorage, n *Node) { _ = gs.DeleteNodeForTenant(n.ID, DefaultTenantID) },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			gs, n := newPanicTestStore(t)
			assertPanicReleasesLock(t, gs, "checkNodeTenant",
				func() { op(gs, n) },
				updateAgain(gs, n))
		})
	}
}

// newPanicEdgeStore opens a store with two nodes and one edge between them. The
// second operation of an edge test updates that edge, so it needs gs.mu and the
// edge's shard lock.
func newPanicEdgeStore(t *testing.T) (*GraphStorage, *Node, *Node, *Edge) {
	t.Helper()
	gs, a := newPanicTestStore(t)
	b, err := gs.CreateNode([]string{"P"}, map[string]Value{"k": IntValue(2)})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	e, err := gs.CreateEdge(a.ID, b.ID, "R", map[string]Value{"k": IntValue(1)}, 1)
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	return gs, a, b, e
}

// updateEdgeAgain is the second operation for a test whose store holds e.
func updateEdgeAgain(gs *GraphStorage, e *Edge) func() error {
	return func() error { return gs.UpdateEdge(e.ID, map[string]Value{"k": IntValue(9)}, nil) }
}

func TestLockPanic_CreateEdgesWithTenant(t *testing.T) {
	gs, a, b, e := newPanicEdgeStore(t)
	assertPanicReleasesLock(t, gs, "CreateEdgesWithTenant",
		func() {
			_, _ = gs.CreateEdgesWithTenant(DefaultTenantID, []EdgeSpec{{FromID: a.ID, ToID: b.ID, Type: "R2", Weight: 1}})
		},
		updateEdgeAgain(gs, e))
}

// DeleteEdgeForTenant and PatchEdgeForTenant share checkEdgeTenant, which holds
// a shard read lock. A leaked read lock blocks the shard write in UpdateEdge.
func TestLockPanic_EdgeTenantCheck(t *testing.T) {
	ops := map[string]func(gs *GraphStorage, e *Edge){
		"PatchEdgeForTenant": func(gs *GraphStorage, e *Edge) {
			_ = gs.PatchEdgeForTenant(e.ID, map[string]Value{"k": IntValue(3)}, nil, nil, DefaultTenantID)
		},
		"DeleteEdgeForTenant": func(gs *GraphStorage, e *Edge) { _ = gs.DeleteEdgeForTenant(e.ID, DefaultTenantID) },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			gs, _, _, e := newPanicEdgeStore(t)
			assertPanicReleasesLock(t, gs, "checkEdgeTenant",
				func() { op(gs, e) },
				updateEdgeAgain(gs, e))
		})
	}
}

func TestLockPanic_DeleteEdge(t *testing.T) {
	gs, _, _, e := newPanicEdgeStore(t)
	assertPanicReleasesLock(t, gs, "DeleteEdge.shard",
		func() { _ = gs.DeleteEdge(e.ID) },
		updateEdgeAgain(gs, e))
}

func TestLockPanic_UpsertEdge(t *testing.T) {
	gs, a, b, e := newPanicEdgeStore(t)
	assertPanicReleasesLock(t, gs, "UpsertEdge.shard",
		func() { _, _, _ = gs.UpsertEdge(a.ID, b.ID, "R", map[string]Value{"k": IntValue(3)}, 1) },
		updateEdgeAgain(gs, e))
}

func TestLockPanic_DeleteEdgeBetweenAcrossTenants(t *testing.T) {
	for _, site := range []string{"DeleteEdgeBetweenAcrossTenants", "DeleteEdgeBetweenAcrossTenants.shard"} {
		t.Run(site, func(t *testing.T) {
			gs, a, b, e := newPanicEdgeStore(t)
			assertPanicReleasesLock(t, gs, site,
				func() { _, _ = gs.DeleteEdgeBetweenAcrossTenants(a.ID, b.ID, "R") },
				updateEdgeAgain(gs, e))
		})
	}
}

// cascadeDeleteOutgoingEdge and cascadeDeleteIncomingEdge share
// detachCascadedEdge. Deleting the source node reaches the outgoing cascade, and
// deleting the target node reaches the incoming one.
func TestLockPanic_CascadeDeleteEdge(t *testing.T) {
	for _, tc := range []struct {
		name   string
		victim func(a, b *Node) uint64
	}{
		{"outgoing", func(a, _ *Node) uint64 { return a.ID }},
		{"incoming", func(_, b *Node) uint64 { return b.ID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs, a, b, e := newPanicEdgeStore(t)
			assertPanicReleasesLock(t, gs, "cascadeDeleteEdge.shard",
				func() { _ = gs.DeleteNode(tc.victim(a, b)) },
				updateEdgeAgain(gs, e))
		})
	}
}

// newPanicDiskEdgeStore opens a store with UseDiskBackedEdges, the only
// configuration that reaches the EdgeStore.
func newPanicDiskEdgeStore(t *testing.T) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            t.TempDir(),
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	if gs.edgeStore == nil {
		t.Fatal("UseDiskBackedEdges did not create an edge store")
	}
	return gs
}

// The Get* sites hold es.mu.RLock, and a leaked read lock blocks the write in
// the second operation. Node 777 is never cached, so the read reaches the LSM.
// The Store* sites hold es.mu.Lock, and the second operation writes again.
func TestLockPanic_EdgeStore(t *testing.T) {
	const nodeID = 777
	cases := []struct {
		site string
		op   func(es *EdgeStore)
		next func(es *EdgeStore) error
	}{
		{"StoreOutgoingEdges",
			func(es *EdgeStore) { _ = es.StoreOutgoingEdges(nodeID, []uint64{1}) },
			func(es *EdgeStore) error { return es.StoreOutgoingEdges(nodeID, []uint64{2}) }},
		{"StoreIncomingEdges",
			func(es *EdgeStore) { _ = es.StoreIncomingEdges(nodeID, []uint64{1}) },
			func(es *EdgeStore) error { return es.StoreIncomingEdges(nodeID, []uint64{2}) }},
		{"GetOutgoingEdges",
			func(es *EdgeStore) { _, _ = es.GetOutgoingEdges(nodeID) },
			func(es *EdgeStore) error { return es.StoreOutgoingEdges(nodeID, []uint64{2}) }},
		{"GetIncomingEdges",
			func(es *EdgeStore) { _, _ = es.GetIncomingEdges(nodeID) },
			func(es *EdgeStore) error { return es.StoreIncomingEdges(nodeID, []uint64{2}) }},
	}
	for _, tc := range cases {
		t.Run(tc.site, func(t *testing.T) {
			gs := newPanicDiskEdgeStore(t)
			es := gs.edgeStore
			assertPanicReleasesLock(t, gs, tc.site,
				func() { tc.op(es) },
				func() error { return tc.next(es) })
		})
	}
}

// Snapshot compresses the edge lists under gs.mu.Lock when compression is on,
// which is the default.
func TestLockPanic_SnapshotCompress(t *testing.T) {
	gs, n := newPanicTestStore(t)
	assertPanicReleasesLock(t, gs, "snapshotWithBoundary",
		func() { _ = gs.Snapshot() },
		updateAgain(gs, n))
}
