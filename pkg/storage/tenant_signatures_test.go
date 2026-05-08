package storage

import (
	"testing"
)

// Tests for the *ForTenant variants. The signatures were added in A3a
// (2026-05-06) as wrappers; A3b (2026-05-08) added the matchesTenant
// enforcement these tests now pin.
//
// The cross-tenant test below was deliberately *renamed* (not deleted)
// from its A3a form so the git blame trail to "this used to assert
// no-op; A3b flipped it" stays useful for future readers.

func mustNewGraphStorage(t *testing.T) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func TestGetNodeForTenant(t *testing.T) {
	gs := mustNewGraphStorage(t)

	// Created with no explicit tenant → goes to default tenant per
	// effectiveTenantID's empty-string handling.
	created, err := gs.CreateNode([]string{"Doc"}, map[string]Value{
		"name": StringValue("alice"),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	t.Run("empty tenantID matches default tenant", func(t *testing.T) {
		// Empty tenantID normalizes to "default" via effectiveTenantID.
		// CreateNode also uses default. They must match.
		got, err := gs.GetNodeForTenant(created.ID, "")
		if err != nil {
			t.Fatalf("GetNodeForTenant(\"\"): %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID: want %d, got %d", created.ID, got.ID)
		}
	})

	t.Run("explicit \"default\" matches", func(t *testing.T) {
		// The advisor's empty-vs-\"default\" pin: both must hit the
		// same code path. Asymmetry here means the comparison is wrong
		// and would break setupTestServer-style fixtures.
		got, err := gs.GetNodeForTenant(created.ID, "default")
		if err != nil {
			t.Fatalf("GetNodeForTenant(\"default\"): %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID: want %d, got %d", created.ID, got.ID)
		}
	})

	t.Run("cross-tenant returns ErrNodeNotFound (no existence leak)", func(t *testing.T) {
		// This sub-test was renamed from "non-empty tenantID is
		// currently ignored (A3a)" when A3b added enforcement.
		// The unified ErrNodeNotFound (vs a distinct ErrCrossTenant)
		// is intentional — distinguishing would let an attacker
		// enumerate "this ID exists in *some* tenant" by response shape.
		_, err := gs.GetNodeForTenant(created.ID, "another-tenant")
		if err != ErrNodeNotFound {
			t.Errorf("cross-tenant: want ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("missing node returns ErrNodeNotFound", func(t *testing.T) {
		_, err := gs.GetNodeForTenant(99999, "")
		if err != ErrNodeNotFound {
			t.Errorf("want ErrNodeNotFound, got %v", err)
		}
	})

	t.Run("explicit tenant matches node created with same explicit tenant", func(t *testing.T) {
		acmeNode, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
		if err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}

		// Same tenant: succeeds.
		got, err := gs.GetNodeForTenant(acmeNode.ID, "acme")
		if err != nil {
			t.Fatalf("acme→acme: %v", err)
		}
		if got.ID != acmeNode.ID {
			t.Errorf("ID mismatch")
		}

		// Default tenant trying to access acme node: blocked.
		if _, err := gs.GetNodeForTenant(acmeNode.ID, ""); err != ErrNodeNotFound {
			t.Errorf("default→acme: want ErrNodeNotFound, got %v", err)
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

// TestForTenant_CrossTenantWriteIsRefused pins the security contract for
// the write paths: tenant A cannot Update or Delete tenant B's resources.
// Without this guard the audit's CRIT #1 (cross-tenant write) reopens.
func TestForTenant_CrossTenantWriteIsRefused(t *testing.T) {
	gs := mustNewGraphStorage(t)

	bNode, err := gs.CreateNodeWithTenant("tenant-B", []string{"Doc"}, map[string]Value{
		"name": StringValue("alice"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant: %v", err)
	}
	bFrom, _ := gs.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	bTo, _ := gs.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	bEdge, err := gs.CreateEdgeWithTenant("tenant-B", bFrom.ID, bTo.ID, "REL", nil, 1.0)
	if err != nil {
		t.Fatalf("CreateEdgeWithTenant: %v", err)
	}

	t.Run("UpdateNodeForTenant cross-tenant", func(t *testing.T) {
		err := gs.UpdateNodeForTenant(bNode.ID, map[string]Value{
			"name": StringValue("attacker"),
		}, "tenant-A")
		if err != ErrNodeNotFound {
			t.Errorf("want ErrNodeNotFound (no leak), got %v", err)
		}
		// Confirm the property was NOT actually updated.
		got, _ := gs.GetNode(bNode.ID)
		if name := string(got.Properties["name"].Data); name != "alice" {
			t.Errorf("cross-tenant update leaked through: name=%q", name)
		}
	})

	t.Run("DeleteNodeForTenant cross-tenant", func(t *testing.T) {
		err := gs.DeleteNodeForTenant(bNode.ID, "tenant-A")
		if err != ErrNodeNotFound {
			t.Errorf("want ErrNodeNotFound, got %v", err)
		}
		// Confirm the node still exists.
		if _, err := gs.GetNode(bNode.ID); err != nil {
			t.Errorf("cross-tenant delete leaked through: %v", err)
		}
	})

	t.Run("UpdateEdgeForTenant cross-tenant", func(t *testing.T) {
		newWeight := 999.0
		err := gs.UpdateEdgeForTenant(bEdge.ID, nil, &newWeight, "tenant-A")
		if err != ErrEdgeNotFound {
			t.Errorf("want ErrEdgeNotFound, got %v", err)
		}
		got, _ := gs.GetEdge(bEdge.ID)
		if got.Weight != 1.0 {
			t.Errorf("cross-tenant edge update leaked through: weight=%v", got.Weight)
		}
	})

	t.Run("DeleteEdgeForTenant cross-tenant", func(t *testing.T) {
		err := gs.DeleteEdgeForTenant(bEdge.ID, "tenant-A")
		if err != ErrEdgeNotFound {
			t.Errorf("want ErrEdgeNotFound, got %v", err)
		}
		if _, err := gs.GetEdge(bEdge.ID); err != nil {
			t.Errorf("cross-tenant edge delete leaked through: %v", err)
		}
	})
}
