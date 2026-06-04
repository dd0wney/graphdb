package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// edgeFixture creates two nodes + an edge in the given tenant and returns its ID.
func edgeFixture(t *testing.T, s *Server, tenantID string, weight float64) uint64 {
	t.Helper()
	a, err := s.graph.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create node a: %v", err)
	}
	b, err := s.graph.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create node b: %v", err)
	}
	e, err := s.graph.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "LINK",
		map[string]storage.Value{"k": storage.StringValue("v1")}, weight)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
	return e.ID
}

func ptrFloat(f float64) *float64 { return &f }

func TestUpdateEdge_PropertiesAndWeight(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tenantID = "acme"
	id := edgeFixture(t, server, tenantID, 1.0)

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", id),
		EdgeUpdateRequest{Properties: map[string]any{"k": "v2"}, Weight: ptrFloat(2.0)}, tenantID))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /edges/%d: want 200, got %d: %s", id, rr.Code, rr.Body.String())
	}

	e, err := server.graph.GetEdgeForTenant(id, tenantID)
	if err != nil {
		t.Fatalf("get edge: %v", err)
	}
	if got, _ := e.Properties["k"].AsString(); got != "v2" {
		t.Errorf("property k = %q, want v2", got)
	}
	if e.Weight != 2.0 {
		t.Errorf("weight = %v, want 2.0", e.Weight)
	}
}

// TestUpdateEdge_OmittedWeightPreservesWeight is the pointer-weight teeth: a
// properties-only update must NOT zero the weight. A bare float64 weight field
// would decode an absent weight as 0.0 and clobber it.
func TestUpdateEdge_OmittedWeightPreservesWeight(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tenantID = "acme"
	id := edgeFixture(t, server, tenantID, 5.0)

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", id),
		EdgeUpdateRequest{Properties: map[string]any{"k": "v2"}}, tenantID)) // no Weight
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	e, err := server.graph.GetEdgeForTenant(id, tenantID)
	if err != nil {
		t.Fatalf("get edge: %v", err)
	}
	if e.Weight != 5.0 {
		t.Errorf("weight = %v after properties-only update, want 5.0 preserved (omitted weight must not zero it)", e.Weight)
	}
	if got, _ := e.Properties["k"].AsString(); got != "v2" {
		t.Errorf("property k = %q, want v2", got)
	}
}

// TestUpdateEdge_NonFiniteWeightRejected: a JSON overflow literal (1e400)
// decodes to +Inf, which the WAL can't marshal (#328) — must be a 400.
func TestUpdateEdge_NonFiniteWeightRejected(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tenantID = "acme"
	id := edgeFixture(t, server, tenantID, 1.0)

	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/edges/%d", id),
		bytes.NewReader([]byte(`{"weight": 1e400}`)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))

	rr := httptest.NewRecorder()
	server.handleEdge(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("PUT non-finite weight: want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdateEdge_CrossTenantNotFound(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	id := edgeFixture(t, server, "acme", 1.0)

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", id),
		EdgeUpdateRequest{Properties: map[string]any{"k": "v2"}}, "other"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant PUT: want 404, got %d", rr.Code)
	}
}

func TestDeleteEdge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tenantID = "acme"
	id := edgeFixture(t, server, tenantID, 1.0)

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodDelete, fmt.Sprintf("/edges/%d", id), nil, tenantID))
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := server.graph.GetEdgeForTenant(id, tenantID); !errors.Is(err, storage.ErrEdgeNotFound) {
		t.Errorf("after delete, GetEdgeForTenant err = %v, want ErrEdgeNotFound", err)
	}
}

func TestDeleteEdge_CrossTenantNotFound(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	id := edgeFixture(t, server, "acme", 1.0)

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodDelete, fmt.Sprintf("/edges/%d", id), nil, "other"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant DELETE: want 404, got %d", rr.Code)
	}
	if _, err := server.graph.GetEdgeForTenant(id, "acme"); err != nil {
		t.Errorf("edge should survive cross-tenant delete attempt: %v", err)
	}
}
