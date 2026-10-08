package storage

import "testing"

// A node or edge created with nil properties keeps a nil map. The patch
// paths write into that map, so they must create it first instead of
// panicking with the global lock or a shard lock held.
func TestPatch_EntityCreatedWithNilProperties(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()

	a, err := gs.CreateNode([]string{"P"}, nil)
	if err != nil {
		t.Fatalf("CreateNode a: %v", err)
	}
	b, err := gs.CreateNode([]string{"Q"}, nil)
	if err != nil {
		t.Fatalf("CreateNode b: %v", err)
	}
	e, err := gs.CreateEdge(a.ID, b.ID, "R", nil, 1)
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	t.Run("edge", func(t *testing.T) {
		if err := gs.PatchEdgeForTenant(e.ID, map[string]Value{"w": IntValue(5)}, nil, nil, DefaultTenantID); err != nil {
			t.Fatalf("PatchEdgeForTenant: %v", err)
		}
		got, err := gs.GetEdge(e.ID)
		if err != nil {
			t.Fatalf("GetEdge: %v", err)
		}
		if v, err := got.Properties["w"].AsInt(); err != nil || v != 5 {
			t.Fatalf("w = %v (err %v), want 5", got.Properties["w"], err)
		}
	})

	t.Run("node", func(t *testing.T) {
		if err := gs.PatchNodeForTenant(a.ID, map[string]Value{"w": IntValue(5)}, nil, DefaultTenantID); err != nil {
			t.Fatalf("PatchNodeForTenant: %v", err)
		}
		got, err := gs.GetNode(a.ID)
		if err != nil {
			t.Fatalf("GetNode: %v", err)
		}
		if v, err := got.Properties["w"].AsInt(); err != nil || v != 5 {
			t.Fatalf("w = %v (err %v), want 5", got.Properties["w"], err)
		}
	})

	// The store must still serve writes afterwards: a panic under a lock
	// that is not deferred would leave it held.
	if _, err := gs.CreateNode([]string{"After"}, nil); err != nil {
		t.Fatalf("CreateNode after the patches: %v", err)
	}
}

// UpsertEdge's update branch merges into the existing edge's map the same way.
func TestUpsertEdge_ExistingEdgeCreatedWithNilProperties(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()

	a, _ := gs.CreateNode([]string{"P"}, nil)
	b, _ := gs.CreateNode([]string{"Q"}, nil)
	if _, err := gs.CreateEdge(a.ID, b.ID, "R", nil, 1); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	e, created, err := gs.UpsertEdge(a.ID, b.ID, "R", map[string]Value{"w": IntValue(5)}, 1)
	if err != nil || created {
		t.Fatalf("UpsertEdge over the existing edge: created=%v err=%v", created, err)
	}
	if v, err := e.Properties["w"].AsInt(); err != nil || v != 5 {
		t.Fatalf("w = %v (err %v), want 5", e.Properties["w"], err)
	}
}

// Batch.UpdateNode merges into the existing node's map the same way.
func TestBatchUpdateNode_NodeCreatedWithNilProperties(t *testing.T) {
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()

	n, _ := gs.CreateNode([]string{"P"}, nil)
	batch := gs.BeginBatch()
	batch.UpdateNode(n.ID, map[string]Value{"w": IntValue(5)})
	if err := batch.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := gs.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if v, err := got.Properties["w"].AsInt(); err != nil || v != 5 {
		t.Fatalf("w = %v (err %v), want 5", got.Properties["w"], err)
	}
}
