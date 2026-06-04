package storage

import "testing"

// This file extends the parallel-invariant coverage (invariants_test.go +
// invariant_matrix_test.go) to the GLOBAL property index — a structure the
// #310/#311 harness consciously excluded. propertyIndexes is maintained by the
// same four write paths, so each must keep it in lockstep with the shards; a
// path that forgets is exactly the silent-drift class this guards.
//
// Each test creates a property index on "kind" (a scalar string), drives
// create/update/delete through one write path on nodes carrying that property,
// and asserts invariants after every mutation — the checker's property-index
// assertion is the cross-structure net (a direct lookup stays as a sanity
// anchor where cheap).

const propKey = "kind"

// newPropGraph returns a fresh storage with a string property index on "kind".
func newPropGraph(t *testing.T) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	if err := gs.CreatePropertyIndex(propKey, TypeString); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	return gs
}

func kind(v string) map[string]Value { return map[string]Value{propKey: StringValue(v)} }

// TestPropertyIndexMatrix_Live drives create / update / remove-prop / delete on
// the direct path, asserting the property index tracks each mutation.
func TestPropertyIndexMatrix_Live(t *testing.T) {
	gs := newPropGraph(t)
	defer func() { _ = gs.Close() }()

	a, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, kind("alpha"))
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, kind("beta"))
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Update a's indexed value: the old bucket must drop a, the new one gain it.
	if err := gs.UpdateNodeForTenant(a.ID, kind("gamma"), DefaultTenantID); err != nil {
		t.Fatalf("update a: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Remove a's indexed property entirely: a must leave the index.
	if err := gs.RemoveNodePropertiesForTenant(a.ID, []string{propKey}, DefaultTenantID); err != nil {
		t.Fatalf("remove prop: %v", err)
	}
	assertGraphInvariants(t, gs)

	// Delete b (still indexed under "beta"): its bucket must be GC'd to empty.
	if err := gs.DeleteNodeForTenant(b.ID, DefaultTenantID); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	assertGraphInvariants(t, gs)
}

// TestPropertyIndexMatrix_Batch drives create / update / delete through
// BeginBatch + Commit (batch is tenant-blind → default tenant).
func TestPropertyIndexMatrix_Batch(t *testing.T) {
	gs := newPropGraph(t)
	defer func() { _ = gs.Close() }()

	b := gs.BeginBatch()
	aID, err := b.AddNode([]string{"Doc"}, kind("alpha"))
	if err != nil {
		t.Fatalf("AddNode a: %v", err)
	}
	if _, err := b.AddNode([]string{"Doc"}, kind("beta")); err != nil {
		t.Fatalf("AddNode b: %v", err)
	}
	if err := b.Commit(); err != nil {
		t.Fatalf("Commit(create): %v", err)
	}
	assertGraphInvariants(t, gs)

	u := gs.BeginBatch()
	u.UpdateNode(aID, kind("gamma"))
	if err := u.Commit(); err != nil {
		t.Fatalf("Commit(update): %v", err)
	}
	assertGraphInvariants(t, gs)

	d := gs.BeginBatch()
	d.DeleteNode(aID)
	if err := d.Commit(); err != nil {
		t.Fatalf("Commit(delete): %v", err)
	}
	assertGraphInvariants(t, gs)
}

// TestPropertyIndexMatrix_WALReplay creates the index + nodes post-snapshot, so
// every mutation is recovered through WAL replay; the single post-recovery
// assertion is the one cell that checks after a reopen (rebuild-on-load is the
// thing under test). CreatePropertyIndex is itself WAL-logged, so the index def
// survives the crash without a snapshot.
func TestPropertyIndexMatrix_WALReplay(t *testing.T) {
	dir := t.TempDir()
	var aID, bID uint64

	{
		gs := testCrashableStorage(t, dir, crashRecoveryConfig(dir))
		if err := gs.CreatePropertyIndex(propKey, TypeString); err != nil {
			t.Fatalf("CreatePropertyIndex: %v", err)
		}
		a, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, kind("alpha"))
		if err != nil {
			t.Fatalf("create a: %v", err)
		}
		b, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, kind("beta"))
		if err != nil {
			t.Fatalf("create b: %v", err)
		}
		aID, bID = a.ID, b.ID
		if err := gs.UpdateNodeForTenant(aID, kind("gamma"), DefaultTenantID); err != nil {
			t.Fatalf("update a: %v", err)
		}
		if err := gs.DeleteNodeForTenant(bID, DefaultTenantID); err != nil {
			t.Fatalf("delete b: %v", err)
		}
		// no Close — simulate crash.
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("recovery NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	assertGraphInvariants(t, gs)
	// Sanity: a survives indexed under "gamma", b is gone.
	got, err := gs.FindNodesByPropertyIndexed(propKey, StringValue("gamma"))
	if err != nil {
		t.Fatalf("FindNodesByPropertyIndexed: %v", err)
	}
	if len(got) != 1 || got[0].ID != aID {
		t.Errorf("FindNodesByPropertyIndexed(gamma) = %v after recovery, want node %d", got, aID)
	}
}

// TestPropertyIndexMatrix_TransactionCreate drives create through
// BeginTransactionForTenant + Commit. Transaction create goes through the shared
// persistNodeLocked, which maintains the property index.
func TestPropertyIndexMatrix_TransactionCreate(t *testing.T) {
	gs := newPropGraph(t)
	defer func() { _ = gs.Close() }()

	tx, err := gs.BeginTransactionForTenant(DefaultTenantID)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if _, err := tx.CreateNode([]string{"Doc"}, kind("alpha")); err != nil {
		t.Fatalf("tx create: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx commit: %v", err)
	}
	assertGraphInvariants(t, gs)

	t.Logf("skip: transaction has no DeleteNode/RemoveNodeProperties op (covered by the live matrix)")
}

// TestPropertyIndexMatrix_TransactionUpdateExisting is the regression guard for
// the transaction property-index fix. Transaction.Commit's existing-node update
// step (transaction_commit.go step 2c) re-indexed vectors but originally skipped
// updatePropertyIndexes, leaving the property index stale after a transaction
// update of an existing node — found by this very checker extension. The fix
// mirrors the direct UpdateNode path; this pins it so a regression re-fires the
// "lists id N not backed by a live node" / "missing from bucket" violations.
func TestPropertyIndexMatrix_TransactionUpdateExisting(t *testing.T) {
	gs := newPropGraph(t)
	defer func() { _ = gs.Close() }()

	a, err := gs.CreateNodeWithTenant(DefaultTenantID, []string{"Doc"}, kind("alpha"))
	if err != nil {
		t.Fatalf("create a: %v", err)
	}

	tx, err := gs.BeginTransactionForTenant(DefaultTenantID)
	if err != nil {
		t.Fatalf("BeginTransactionForTenant: %v", err)
	}
	if err := tx.UpdateNode(a.ID, kind("gamma")); err != nil {
		t.Fatalf("tx update existing: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx commit: %v", err)
	}
	assertGraphInvariants(t, gs)
}
