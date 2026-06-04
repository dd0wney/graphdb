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
