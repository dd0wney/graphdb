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
