package storage

import "testing"

// A node created with nil properties has no map. Transaction.Commit wrote into
// it with no guard, unlike patchNode, patchEdge and Batch, so the commit
// panicked while it held gs.mu and the node's shard lock.
func TestTransactionCommit_UpdateOfNodeWithNilProperties(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	n, err := gs.CreateNode([]string{"P"}, nil)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if err := tx.UpdateNode(n.ID, map[string]Value{"k": IntValue(7)}); err != nil {
		t.Fatalf("tx.UpdateNode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := gs.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	v, ok := got.Properties["k"]
	if !ok {
		t.Fatalf("properties after commit = %v, want k=7", got.Properties)
	}
	if k, err := v.AsInt(); err != nil || k != 7 {
		t.Fatalf("k after commit = %v (%v), want 7", k, err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
