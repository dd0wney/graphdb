package storage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// This file drives the parallel-invariant checker (assertGraphInvariants, see
// invariants_test.go) through the write-path × op MATRIX. graphdb has four write
// paths (live / batch / WAL-replay / transaction) that must each keep every
// derived index in lockstep with the shards; the silent bugs this session
// (#288, #298, #305, #307, #308) were missing cells in exactly this matrix. Each
// test exercises one path through a create→update→remove→delete lifecycle with a
// VECTOR INDEX present (so the vector family is covered too), asserting
// invariants after every mutation. A direct count assertion stays at each step
// as a sanity anchor; the checker is the cross-structure net on top.

// newVectorGraph returns a fresh storage with an "embedding" vector index
// created for each named tenant, so vector inserts fire on every path.
func newVectorGraph(t *testing.T, tenants ...string) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	for _, tn := range tenants {
		if err := gs.CreateVectorIndexForTenant(tn, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant(%s): %v", tn, err)
		}
	}
	return gs
}

func vec3(x, y, z float32) map[string]Value {
	return map[string]Value{"embedding": VectorValue([]float32{x, y, z})}
}

// TestInvariantMatrix_Live drives the full op set on the direct path, with two
// tenants so the count-chain Σ assertions and cross-tenant isolation are
// exercised. Invariants must hold after create, update, remove-prop, and both
// delete kinds.
func TestInvariantMatrix_Live(t *testing.T) {
	gs := newVectorGraph(t, "acme", "globex")
	defer func() { _ = gs.Close() }()

	a, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, vec3(1, 0, 0))
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	assertGraphInvariants(t, gs)

	b, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	// A node in a DIFFERENT tenant — its vector must count only against globex.
	g, err := gs.CreateNodeWithTenant("globex", []string{"Doc"}, vec3(0, 1, 0))
	if err != nil {
		t.Fatalf("create g: %v", err)
	}
	assertGraphInvariants(t, gs)

	e, err := gs.CreateEdgeWithTenant("acme", a.ID, b.ID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Update a's vector (re-index in place).
	if err := gs.UpdateNodeForTenant(a.ID, vec3(0, 0, 1), "acme"); err != nil {
		t.Fatalf("update a: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Remove a's vector property (vector must leave the index).
	if err := gs.RemoveNodePropertiesForTenant(a.ID, []string{"embedding"}, "acme"); err != nil {
		t.Fatalf("remove prop: %v", err)
	}
	assertGraphInvariants(t, gs)

	if err := gs.DeleteEdgeForTenant(e.ID, "acme"); err != nil {
		t.Fatalf("delete edge: %v", err)
	}
	assertGraphInvariants(t, gs)

	if err := gs.DeleteNodeForTenant(a.ID, "acme"); err != nil {
		t.Fatalf("delete a: %v", err)
	}
	if err := gs.DeleteNodeForTenant(b.ID, "acme"); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	if err := gs.DeleteNodeForTenant(g.ID, "globex"); err != nil {
		t.Fatalf("delete g: %v", err)
	}
	// Empty graph: every count chain must bottom out at zero, no residue.
	assertGraphInvariants(t, gs)
	if got := gs.CountNodesForTenant("acme"); got != 0 {
		t.Errorf("acme nodes = %d after deleting all, want 0", got)
	}
}

// TestInvariantMatrix_Batch drives create/update/delete through BeginBatch +
// Commit (batch ops are buffered, so invariants are only meaningful after each
// Commit). Batch has no RemoveNodeProperties — that row is logged as skipped.
func TestInvariantMatrix_Batch(t *testing.T) {
	gs := newVectorGraph(t, DefaultTenantID)
	defer func() { _ = gs.Close() }()

	// Batch create: two nodes + an edge (batch is tenant-blind → default tenant).
	b := gs.BeginBatch()
	aID, err := b.AddNode([]string{"Doc"}, vec3(1, 0, 0))
	if err != nil {
		t.Fatalf("AddNode a: %v", err)
	}
	bID, err := b.AddNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("AddNode b: %v", err)
	}
	eID, err := b.AddEdge(aID, bID, "LINKS", nil, 1.0)
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit(create): %v", err)
	}
	assertGraphInvariants(t, gs)

	// Batch update a's vector.
	u := gs.BeginBatch()
	u.UpdateNode(aID, vec3(0, 0, 1))
	if err := u.Commit(); err != nil {
		t.Fatalf("Commit(update): %v", err)
	}
	assertGraphInvariants(t, gs)

	t.Logf("skip: batch path has no RemoveNodeProperties op (covered by the live matrix)")

	// Batch delete the standalone edge (exercises executeDeleteEdge).
	de := gs.BeginBatch()
	de.DeleteEdge(eID)
	if err := de.Commit(); err != nil {
		t.Fatalf("Commit(delete edge): %v", err)
	}
	assertGraphInvariants(t, gs)

	// A fresh connected pair so the next node delete CASCADES a live edge —
	// executeDeleteNode's cascade path (the #304/E bug class), which a
	// delete-edge-then-delete-node order would never reach.
	cID, err := func() (uint64, error) {
		cb := gs.BeginBatch()
		c, err := cb.AddNode([]string{"Doc"}, vec3(0, 0, 1))
		if err != nil {
			return 0, err
		}
		d, err := cb.AddNode([]string{"Doc"}, nil)
		if err != nil {
			return 0, err
		}
		if _, err := cb.AddEdge(c, d, "LINKS", nil, 1.0); err != nil {
			return 0, err
		}
		return c, cb.Commit()
	}()
	if err != nil {
		t.Fatalf("batch create connected pair: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Batch delete C while its edge is live — the cascade must clean the edge out
	// of every index (global type, tenant edge index, adjacency) AND remove C's
	// vector.
	dc := gs.BeginBatch()
	dc.DeleteNode(cID)
	if err := dc.Commit(); err != nil {
		t.Fatalf("Commit(cascade delete node): %v", err)
	}
	assertGraphInvariants(t, gs)

	// Delete everything else; the graph must bottom out clean.
	dn := gs.BeginBatch()
	for _, n := range gs.GetAllNodesForTenant(DefaultTenantID) {
		dn.DeleteNode(n.ID)
	}
	if err := dn.Commit(); err != nil {
		t.Fatalf("Commit(delete rest): %v", err)
	}
	assertGraphInvariants(t, gs)
	if got := gs.CountNodesForTenant(DefaultTenantID); got != 0 {
		t.Errorf("default-tenant nodes = %d after batch deleting all, want 0", got)
	}
}

// TestInvariantMatrix_Transaction drives create + update through
// BeginTransactionForTenant + Commit. Transactions support neither delete nor
// remove-prop; those rows are logged as skipped (no silent omission).
func TestInvariantMatrix_Transaction(t *testing.T) {
	gs := newVectorGraph(t, "acme")
	defer func() { _ = gs.Close() }()

	tx, err := gs.BeginTransactionForTenant("acme")
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	a, err := tx.CreateNode([]string{"Doc"}, vec3(1, 0, 0))
	if err != nil {
		t.Fatalf("tx create a: %v", err)
	}
	b, err := tx.CreateNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("tx create b: %v", err)
	}
	if _, err := tx.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0); err != nil {
		t.Fatalf("tx create edge: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx Commit(create): %v", err)
	}
	assertGraphInvariants(t, gs)

	tx2, err := gs.BeginTransactionForTenant("acme")
	if err != nil {
		t.Fatalf("BeginTransactionForTenant 2: %v", err)
	}
	if err := tx2.UpdateNode(a.ID, vec3(0, 0, 1)); err != nil {
		t.Fatalf("tx update a: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("tx Commit(update): %v", err)
	}
	assertGraphInvariants(t, gs)

	t.Logf("skip: transaction path supports neither DeleteNode/DeleteEdge nor RemoveNodeProperties (covered by the live matrix)")
}

// TestInvariantMatrix_WALReplay is the crash-recovery cell: a mix of create /
// update / delete ops applied post-snapshot, recovered through the WAL replay
// path. The single post-recovery assertGraphInvariants is the one place the
// checker runs after a reopen (rebuild-on-load is the thing under test) — it
// would have caught #305 and #308's C/D/E.
func TestInvariantMatrix_WALReplay(t *testing.T) {
	dir := t.TempDir()
	const tn = "acme"

	var keepID, victimID, edgeID uint64

	// Phase 1: snapshot baseline — index + two nodes + one edge, clean close.
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("phase1 NewGraphStorage: %v", err)
		}
		if err := gs.CreateVectorIndexForTenant(tn, "embedding", 3, 16, 200, vector.MetricCosine); err != nil {
			t.Fatalf("CreateVectorIndexForTenant: %v", err)
		}
		keep, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, vec3(1, 0, 0))
		if err != nil {
			t.Fatalf("create keep: %v", err)
		}
		victim, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, vec3(0, 1, 0))
		if err != nil {
			t.Fatalf("create victim: %v", err)
		}
		keepID, victimID = keep.ID, victim.ID
		e, err := gs.CreateEdgeWithTenant(tn, keepID, victimID, "LINKS", nil, 1.0)
		if err != nil {
			t.Fatalf("create edge: %v", err)
		}
		edgeID = e.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("phase1 Close: %v", err)
		}
	}

	// Phase 2: reopen, apply post-snapshot ops (recovered only via WAL), crash:
	// update keep's vector, create a new node, delete the victim (cascades the
	// edge), delete that fresh node's nothing — a mix across create/update/delete.
	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.UpdateNodeForTenant(keepID, vec3(0, 0, 1), tn); err != nil {
			t.Fatalf("update keep: %v", err)
		}
		if _, err := gs.CreateNodeWithTenant(tn, []string{"Doc"}, vec3(1, 1, 0)); err != nil {
			t.Fatalf("create fresh: %v", err)
		}
		if err := gs.DeleteNodeForTenant(victimID, tn); err != nil {
			t.Fatalf("delete victim (cascades edge %d): %v", edgeID, err)
		}
		// no Close — simulate crash.
	}

	// Phase 3: recover and assert EVERY invariant holds over the replayed state.
	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recovery NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	assertGraphInvariants(t, gs)
	// Sanity anchors: keep + fresh survive (2), victim gone, edge cascaded.
	if got := gs.CountNodesForTenant(tn); got != 2 {
		t.Errorf("CountNodesForTenant = %d after recovery, want 2 (keep + fresh)", got)
	}
	if got := gs.CountEdgesForTenant(tn); got != 0 {
		t.Errorf("CountEdgesForTenant = %d after recovery, want 0 (edge cascaded with victim)", got)
	}
}
