package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// Audit A6a (2026-05-08): pin the cross-tenant contract for /nodes and
// /edges handlers after migrating to *ForTenant variants. The handlers
// previously called tenant-blind storage methods, so a caller in tenant
// B with a JWT for a known node ID could read or mutate tenant A's
// node. The fix routes through GetNodeForTenant / UpdateNodeForTenant /
// DeleteNodeForTenant / GetEdgeForTenant, which return ErrNodeNotFound
// (or ErrEdgeNotFound) on cross-tenant; handlers map that to 404 to
// avoid an existence-leak side channel.

// reqWithTenant builds a request with Content-Type set and the tenant
// context wired the same way withTenant would after JWT extraction.
func reqWithTenant(t *testing.T, method, path string, body any, tenantID string) *http.Request {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// TestA6a_CreateNode_LandsInCallerTenant verifies a POST /nodes from
// tenant-A creates a node owned by tenant-A, not the default tenant.
// Without the *ForTenant migration this would silently land in
// "default" — an ownership-stamping bug that defeats every read scope.
func TestA6a_CreateNode_LandsInCallerTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	server.createNode(rr, reqWithTenant(t, http.MethodPost, "/nodes", NodeRequest{
		Labels:     []string{"Doc"},
		Properties: map[string]any{"title": "A's secret"},
	}, "tenant-A"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("createNode: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp NodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Read back as tenant-A → should succeed.
	if _, err := server.graph.GetNodeForTenant(resp.ID, "tenant-A"); err != nil {
		t.Errorf("GetNodeForTenant(tenant-A): want ok, got %v — node was not stamped with caller tenant", err)
	}
	// Read back as default → must fail (proves it didn't land in default).
	if _, err := server.graph.GetNodeForTenant(resp.ID, "default"); err == nil {
		t.Errorf("GetNodeForTenant(default): want ErrNodeNotFound, got node — leak into default tenant")
	}
}

// TestA6a_GetNode_CrossTenant_Returns404 is the existence-leak gate:
// tenant-B requesting tenant-A's node must get 404, indistinguishable
// from a genuinely-missing ID.
func TestA6a_GetNode_CrossTenant_Returns404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	node, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
		"title": storage.StringValue("A's secret"),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("owner reads its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleNode(rr, reqWithTenant(t, http.MethodGet, "/nodes/"+strconv.FormatUint(node.ID, 10), nil, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Errorf("owner: want 200, got %d", rr.Code)
		}
	})
	t.Run("cross-tenant returns 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleNode(rr, reqWithTenant(t, http.MethodGet, "/nodes/"+strconv.FormatUint(node.ID, 10), nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant: want 404 (no existence leak), got %d body=%s", rr.Code, rr.Body.String())
		}
	})
	t.Run("missing id returns 404", func(t *testing.T) {
		// 999999 doesn't exist in any tenant — the response must be
		// indistinguishable from the cross-tenant case above.
		rr := httptest.NewRecorder()
		server.handleNode(rr, reqWithTenant(t, http.MethodGet, "/nodes/999999", nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("missing: want 404, got %d", rr.Code)
		}
	})
}

// TestA6a_UpdateNode_CrossTenantIsRefused ensures tenant-B cannot
// silently overwrite tenant-A's properties — and that the API surfaces
// the refusal as 404 (not 500). Critically, it asserts the property
// stayed unchanged: a 404 with side effects would still be a CRIT bug.
func TestA6a_UpdateNode_CrossTenantIsRefused(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	node, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
		"title": storage.StringValue("original"),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodPut, "/nodes/"+strconv.FormatUint(node.ID, 10), NodeRequest{
		Properties: map[string]any{"title": "tampered"},
	}, "tenant-B"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant PUT: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Side-effect check: the original property must be untouched.
	got, err := server.graph.GetNodeForTenant(node.ID, "tenant-A")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	title, err := got.Properties["title"].AsString()
	if err != nil {
		t.Fatalf("AsString: %v", err)
	}
	if title != "original" {
		t.Errorf("cross-tenant update leaked through: title=%q (want %q)", title, "original")
	}
}

// TestA6a_DeleteNode_CrossTenantIsRefused mirrors the update case for
// the destructive path: the node must still be readable by its owner
// after a cross-tenant DELETE attempt.
func TestA6a_DeleteNode_CrossTenantIsRefused(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	node, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodDelete, "/nodes/"+strconv.FormatUint(node.ID, 10), nil, "tenant-B"))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant DELETE: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := server.graph.GetNodeForTenant(node.ID, "tenant-A"); err != nil {
		t.Errorf("node disappeared after refused cross-tenant delete: %v", err)
	}
}

// TestA6a_CreateEdge_LandsInCallerTenant mirrors the createNode test
// for edges: a POST /edges from tenant-A must stamp the resulting edge
// with tenant-A, not the default tenant. Without this, a future revert
// of the handler from CreateEdgeWithTenant back to CreateEdge would
// silently land every edge in default — re-introducing the
// ownership-stamping bug A6a fixes.
func TestA6a_CreateEdge_LandsInCallerTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	from, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed from: %v", err)
	}
	to, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed to: %v", err)
	}

	rr := httptest.NewRecorder()
	server.createEdge(rr, reqWithTenant(t, http.MethodPost, "/edges", EdgeRequest{
		FromNodeID: from.ID,
		ToNodeID:   to.ID,
		Type:       "REFERENCES",
		Weight:     1.0,
	}, "tenant-A"))
	if rr.Code != http.StatusCreated {
		t.Fatalf("createEdge: want 201, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp EdgeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Owner can read it back.
	if _, err := server.graph.GetEdgeForTenant(resp.ID, "tenant-A"); err != nil {
		t.Errorf("GetEdgeForTenant(tenant-A): want ok, got %v — edge was not stamped with caller tenant", err)
	}
	// Default tenant cannot — proves the edge didn't land there.
	if _, err := server.graph.GetEdgeForTenant(resp.ID, "default"); err == nil {
		t.Errorf("GetEdgeForTenant(default): want ErrEdgeNotFound, got edge — leak into default tenant")
	}
}

// TestA6a_GetEdge_CrossTenant_Returns404 is the edge-side existence
// gate. Edges live in their own ID space; the equivalent leak would be
// "tenant-B confirms tenant-A has an edge with id 7" via a 200 vs 404
// distinction. Both must collapse to 404.
func TestA6a_GetEdge_CrossTenant_Returns404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	from, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed from: %v", err)
	}
	to, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed to: %v", err)
	}
	edge, err := server.graph.CreateEdgeWithTenant("tenant-A", from.ID, to.ID, "REFERENCES", nil, 0)
	if err != nil {
		t.Fatalf("seed edge: %v", err)
	}

	t.Run("owner reads its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleEdge(rr, reqWithTenant(t, http.MethodGet, "/edges/"+strconv.FormatUint(edge.ID, 10), nil, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Errorf("owner: want 200, got %d", rr.Code)
		}
	})
	t.Run("cross-tenant returns 404", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleEdge(rr, reqWithTenant(t, http.MethodGet, "/edges/"+strconv.FormatUint(edge.ID, 10), nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant: want 404, got %d", rr.Code)
		}
	})
}
