package storage

import "testing"

func TestDeleteAllNodesForTenant_Isolation(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()

	seed := func(tenant string) {
		var ids []uint64
		for i := 0; i < 10; i++ {
			n, err := gs.CreateNodeWithTenant(tenant, []string{"Person"},
				map[string]Value{"name": StringValue(tenant)})
			if err != nil {
				t.Fatalf("create %s: %v", tenant, err)
			}
			ids = append(ids, n.ID)
		}
		for i := 0; i+1 < len(ids); i++ {
			if _, err := gs.CreateEdgeWithTenant(tenant, ids[i], ids[i+1], "NEXT", nil, 1); err != nil {
				t.Fatalf("edge %s: %v", tenant, err)
			}
		}
	}
	seed("tenant-a")
	seed("tenant-b")

	if err := gs.DeleteAllNodesForTenant("tenant-a"); err != nil {
		t.Fatalf("DeleteAllNodesForTenant: %v", err)
	}

	// tenant-a fully cleared.
	if c := gs.CountNodesForTenant("tenant-a"); c != 0 {
		t.Errorf("tenant-a nodes = %d, want 0", c)
	}
	if c := gs.CountEdgesForTenant("tenant-a"); c != 0 {
		t.Errorf("tenant-a edges = %d, want 0", c)
	}
	// tenant-b untouched.
	if c := gs.CountNodesForTenant("tenant-b"); c != 10 {
		t.Errorf("tenant-b nodes = %d, want 10", c)
	}
	if c := gs.CountEdgesForTenant("tenant-b"); c != 9 {
		t.Errorf("tenant-b edges = %d, want 9", c)
	}
}
