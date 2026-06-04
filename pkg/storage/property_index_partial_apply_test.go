package storage

import "testing"

// TestCreateNode_TypeMismatchedIndexedProperty_NoPartialApply pins that
// creating a node whose value for an indexed property does NOT match the
// index's declared type does not leave the node half-committed.
//
// The bug: persistNodeLocked publishes the node into the shard map, the global
// label index, the per-tenant index, and bumps NodeCount BEFORE calling
// insertNodeIntoPropertyIndexes. That helper used to fail on a type mismatch
// (PropertyIndex.Insert rejects a value whose Type != indexType), and
// createNodeLocked returned the error WITHOUT rolling back the visible
// mutations — so the caller saw a failed create while the node was live in
// every index and the count. graphdb is schemaless (property types are
// per-node), so a wrong-typed value for an indexed key is reachable from the
// REST/GraphQL surface with no prior validation.
//
// The fix mirrors the BUILD path (CreatePropertyIndex / replayCreatePropertyIndex,
// which skip values where prop.Type != valueType): the per-write insert skips
// type-mismatched values too. The create succeeds, the mismatched value is
// simply not indexed (it cannot be — the index holds one type), and there is no
// partial apply. This also makes the live write path agree with what a snapshot
// rebuild / WAL replay would produce for the same node.
func TestCreateNode_TypeMismatchedIndexedProperty_NoPartialApply(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}

	const tenant = "acme"
	// "age" is declared TypeInt in the index, but this node supplies a string —
	// a type mismatch the index cannot hold.
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
		"age":  StringValue("not-an-int"),
	})
	if err != nil {
		// Pre-fix path: the create reported failure. The teeth — prove the
		// partial apply by reporting the leaked state alongside the error.
		t.Fatalf("CreateNodeWithTenant errored on a type-mismatched indexed property: %v "+
			"(pre-fix this also leaves a partial apply: CountNodesForTenant=%d)",
			err, gs.CountNodesForTenant(tenant))
	}
	if node == nil {
		t.Fatal("CreateNodeWithTenant returned nil node with nil error")
	}

	// The node is fully and consistently committed: visible, counted once.
	if got := gs.CountNodesForTenant(tenant); got != 1 {
		t.Errorf("CountNodesForTenant = %d, want 1", got)
	}
	if _, err := gs.GetNodeForTenant(node.ID, tenant); err != nil {
		t.Errorf("GetNodeForTenant(%d): %v — node not retrievable after create", node.ID, err)
	}
	byLabel := gs.GetNodesByLabelForTenant(tenant, "Person")
	if len(byLabel) != 1 {
		t.Errorf("GetNodesByLabelForTenant(Person) = %d, want 1", len(byLabel))
	}

	// Every derived index agrees with the authoritative shards. The mismatched
	// "age" value is correctly absent from the property index (it can't be held),
	// which is exactly what the checker expects (it filters ground truth by
	// indexType). No drift.
	assertGraphInvariants(t, gs)
}

// TestCreateNode_MatchingIndexedProperty_StillIndexed guards against the fix
// over-reaching: a value whose type DOES match the index must still be indexed.
func TestCreateNode_MatchingIndexedProperty_StillIndexed(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}

	const tenant = "acme"
	if _, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{
		"age": IntValue(30),
	}); err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	found, err := gs.FindNodesByPropertyIndexedForTenant("age", IntValue(30), tenant)
	if err != nil {
		t.Fatalf("FindNodesByPropertyIndexedForTenant: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("indexed lookup age=30 = %d nodes, want 1 — a type-MATCHING value must still be indexed", len(found))
	}
	assertGraphInvariants(t, gs)
}

// --- Update / delete cells (follow-up to the create-cell fix above) ---
//
// The create cell skipped type-mismatched inserts. The update and delete cells
// had the symmetric problem at idx.Remove: PropertyIndex.Remove errors
// "node not found" when the (value, node) pair isn't in the index — which is
// exactly the case for a type-mismatched value (never inserted). So updating or
// deleting a node carrying a mismatched indexed property hit a spurious error +
// partial apply. The fix gates BOTH Remove and Insert on value.Type ==
// idx.indexType across updatePropertyIndexes (live/transaction/replay),
// removeNodeFromPropertyIndexes (delete), and the batch executor's inline copies.

// TestUpdateNode_IndexedPropertyToMismatchedType_NoPartialApply: a node with a
// type-MATCHING indexed value, updated to a MISMATCHED type. Pre-fix:
// updatePropertyIndexes removed the old (matching) value then failed to insert
// the mismatched one — leaving the index missing a value the node still carried
// (checker-catchable drift) and returning an error. Post-fix: old value removed,
// new value skipped (not indexable), update succeeds, no drift.
func TestUpdateNode_IndexedPropertyToMismatchedType_NoPartialApply(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	const tenant = "acme"
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{"age": IntValue(25)})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	// age 25 (int, indexed) -> "later" (string, mismatch).
	if err := gs.UpdateNode(node.ID, map[string]Value{"age": StringValue("later")}); err != nil {
		t.Fatalf("UpdateNode errored converting an indexed property to a mismatched type: %v "+
			"(pre-fix: Remove(old) succeeded, Insert(new) failed → index lost a value the node still had)", err)
	}
	// The old indexed value must be gone (the node no longer holds age=25).
	if found, _ := gs.FindNodesByPropertyIndexedForTenant("age", IntValue(25), tenant); len(found) != 0 {
		t.Errorf("indexed lookup age=25 = %d after update, want 0 (old value not removed)", len(found))
	}
	assertGraphInvariants(t, gs)
}

// TestUpdateNode_PreviouslyMismatchedProperty_NoRemoveError: a node whose
// indexed property was ALWAYS a mismatched type (so never indexed), updated
// again. Pre-fix: updatePropertyIndexes tried to Remove the never-indexed old
// value → "not found" error → the update failed spuriously. Teeth is err==nil
// (the checker passes either way — a mismatched value is never expected in the
// index).
func TestUpdateNode_PreviouslyMismatchedProperty_NoRemoveError(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	const tenant = "acme"
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{"age": StringValue("foo")})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	if err := gs.UpdateNode(node.ID, map[string]Value{"age": StringValue("bar")}); err != nil {
		t.Errorf("UpdateNode errored removing a never-indexed (type-mismatched) old value: %v", err)
	}
	assertGraphInvariants(t, gs)
}

// TestDeleteNode_TypeMismatchedIndexedProperty_NoRemoveError: deleting a node
// that carries a type-mismatched indexed property. Pre-fix:
// removeNodeFromPropertyIndexes tried to Remove the never-indexed value →
// "not found" → DeleteNode errored, leaving the node behind. Teeth is err==nil
// + the node actually gone.
func TestDeleteNode_TypeMismatchedIndexedProperty_NoRemoveError(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	const tenant = "acme"
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{"age": StringValue("foo")})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	if err := gs.DeleteNode(node.ID); err != nil {
		t.Fatalf("DeleteNode errored removing a node with a type-mismatched indexed property: %v "+
			"(pre-fix: removeNodeFromPropertyIndexes Remove → not found)", err)
	}
	if got := gs.CountNodesForTenant(tenant); got != 0 {
		t.Errorf("CountNodesForTenant = %d after delete, want 0", got)
	}
	assertGraphInvariants(t, gs)
}

// TestBatchUpdateNode_TypeMismatchedIndexedProperty_NoPartialApply pins the
// batch executor's inline update copy (separate from updatePropertyIndexes).
func TestBatchUpdateNode_TypeMismatchedIndexedProperty_NoPartialApply(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	const tenant = "acme"
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{"age": IntValue(25)})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	b := gs.BeginBatch()
	b.UpdateNode(node.ID, map[string]Value{"age": StringValue("later")})
	if err := b.Commit(); err != nil {
		t.Fatalf("batch Commit errored on a type-mismatched indexed update: %v", err)
	}
	if found, _ := gs.FindNodesByPropertyIndexedForTenant("age", IntValue(25), tenant); len(found) != 0 {
		t.Errorf("indexed lookup age=25 = %d after batch update, want 0", len(found))
	}
	assertGraphInvariants(t, gs)
}

// TestBatchDeleteNode_TypeMismatchedIndexedProperty_NoRemoveError pins the
// batch executor's inline delete copy.
func TestBatchDeleteNode_TypeMismatchedIndexedProperty_NoRemoveError(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if err := gs.CreatePropertyIndex("age", TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	const tenant = "acme"
	node, err := gs.CreateNodeWithTenant(tenant, []string{"Person"}, map[string]Value{"age": StringValue("foo")})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}

	b := gs.BeginBatch()
	b.DeleteNode(node.ID)
	if err := b.Commit(); err != nil {
		t.Fatalf("batch Commit errored deleting a node with a type-mismatched indexed property: %v", err)
	}
	if got := gs.CountNodesForTenant(tenant); got != 0 {
		t.Errorf("CountNodesForTenant = %d after batch delete, want 0", got)
	}
	assertGraphInvariants(t, gs)
}
