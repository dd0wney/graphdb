package storage

import (
	"testing"
)

// Tests for the *ForTenant variants added in audit task A3a (2026-05-06).
//
// In A3a these methods are wrappers — they accept tenantID but do not
// enforce it. Behaviour must match the legacy methods exactly. A3b will
// add the matchesTenant check that returns ErrNodeNotFound on
// cross-tenant access; the tests in this file pin the *current* (A3a)
// behaviour and will need updating in A3b.

func mustNewGraphStorage(t *testing.T) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func TestGetNodeForTenant_WrapsGetNode(t *testing.T) {
	gs := mustNewGraphStorage(t)

	created, err := gs.CreateNode([]string{"Doc"}, map[string]Value{
		"name": StringValue("alice"),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	t.Run("matching tenant", func(t *testing.T) {
		got, err := gs.GetNodeForTenant(created.ID, "")
		if err != nil {
			t.Fatalf("GetNodeForTenant: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID: want %d, got %d", created.ID, got.ID)
		}
	})

	t.Run("non-empty tenantID is currently ignored (A3a)", func(t *testing.T) {
		// In A3a, tenantID is accepted but not enforced — the call returns
		// the node regardless. A3b will change this behaviour to return
		// ErrNodeNotFound for non-matching tenant. This test pins A3a's
		// no-op behaviour so the regression is visible at A3b.
		got, err := gs.GetNodeForTenant(created.ID, "another-tenant")
		if err != nil {
			t.Fatalf("A3a behaviour: should return node ignoring tenantID; got err: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID: want %d, got %d", created.ID, got.ID)
		}
	})

	t.Run("missing node returns ErrNodeNotFound", func(t *testing.T) {
		_, err := gs.GetNodeForTenant(99999, "")
		if err != ErrNodeNotFound {
			t.Errorf("want ErrNodeNotFound, got %v", err)
		}
	})
}

func TestUpdateNodeForTenant_WrapsUpdateNode(t *testing.T) {
	gs := mustNewGraphStorage(t)

	created, err := gs.CreateNode([]string{"Doc"}, map[string]Value{
		"name": StringValue("alice"),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if err := gs.UpdateNodeForTenant(created.ID, map[string]Value{
		"name": StringValue("alice-updated"),
	}, ""); err != nil {
		t.Fatalf("UpdateNodeForTenant: %v", err)
	}

	got, err := gs.GetNode(created.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if name, ok := got.Properties["name"]; !ok || string(name.Data) != "alice-updated" {
		t.Errorf("Properties[name]: want updated, got %v", got.Properties)
	}
}

func TestDeleteNodeForTenant_WrapsDeleteNode(t *testing.T) {
	gs := mustNewGraphStorage(t)

	created, err := gs.CreateNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if err := gs.DeleteNodeForTenant(created.ID, ""); err != nil {
		t.Fatalf("DeleteNodeForTenant: %v", err)
	}

	if _, err := gs.GetNode(created.ID); err != ErrNodeNotFound {
		t.Errorf("after delete: want ErrNodeNotFound, got %v", err)
	}
}

func TestGetEdgeForTenant_WrapsGetEdge(t *testing.T) {
	gs := mustNewGraphStorage(t)

	from, err := gs.CreateNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("CreateNode from: %v", err)
	}
	to, err := gs.CreateNode([]string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("CreateNode to: %v", err)
	}
	edge, err := gs.CreateEdge(from.ID, to.ID, "REL", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	got, err := gs.GetEdgeForTenant(edge.ID, "")
	if err != nil {
		t.Fatalf("GetEdgeForTenant: %v", err)
	}
	if got.ID != edge.ID {
		t.Errorf("ID: want %d, got %d", edge.ID, got.ID)
	}

	if _, err := gs.GetEdgeForTenant(99999, ""); err != ErrEdgeNotFound {
		t.Errorf("missing: want ErrEdgeNotFound, got %v", err)
	}
}

func TestUpdateEdgeForTenant_WrapsUpdateEdge(t *testing.T) {
	gs := mustNewGraphStorage(t)

	from, _ := gs.CreateNode([]string{"Doc"}, nil)
	to, _ := gs.CreateNode([]string{"Doc"}, nil)
	edge, _ := gs.CreateEdge(from.ID, to.ID, "REL", nil, 1.0)

	newWeight := 2.5
	if err := gs.UpdateEdgeForTenant(edge.ID, nil, &newWeight, ""); err != nil {
		t.Fatalf("UpdateEdgeForTenant: %v", err)
	}

	got, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("GetEdge: %v", err)
	}
	if got.Weight != newWeight {
		t.Errorf("Weight: want %v, got %v", newWeight, got.Weight)
	}
}

func TestDeleteEdgeForTenant_WrapsDeleteEdge(t *testing.T) {
	gs := mustNewGraphStorage(t)

	from, _ := gs.CreateNode([]string{"Doc"}, nil)
	to, _ := gs.CreateNode([]string{"Doc"}, nil)
	edge, _ := gs.CreateEdge(from.ID, to.ID, "REL", nil, 1.0)

	if err := gs.DeleteEdgeForTenant(edge.ID, ""); err != nil {
		t.Fatalf("DeleteEdgeForTenant: %v", err)
	}

	if _, err := gs.GetEdge(edge.ID); err != ErrEdgeNotFound {
		t.Errorf("after delete: want ErrEdgeNotFound, got %v", err)
	}
}
