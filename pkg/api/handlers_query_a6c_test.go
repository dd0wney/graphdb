package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// Audit A6c-query (2026-05-08): HTTP-level cross-tenant isolation
// gate for /query (Cypher-ish). Pre-fix, ExecutionContext carried a
// context.Context but the executor never read tenant from it; every
// graph read used tenant-blind methods. This test pins the security
// contract end-to-end: tenant-A's MATCH must not surface tenant-B's
// nodes, and CREATE must stamp the caller's tenant.

// queryReqWithTenant builds a /query POST with the tenant context
// wired the same way withTenant middleware does in production.
func queryReqWithTenant(t *testing.T, queryStr string, tenantID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(QueryRequest{Query: queryStr})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// TestA6c_Query_MatchIsolation pins that MATCH in /query only sees
// the caller's tenant data — the cardinal cross-tenant read leak.
func TestA6c_Query_MatchIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("alice-A"),
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("bob-B"),
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	t.Run("tenant-A only sees its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleQuery(rr, queryReqWithTenant(t, "MATCH (p:Person) RETURN p.name", "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Count != 1 {
			t.Errorf("tenant-A: want 1 match, got %d (rows=%v)", resp.Count, resp.Rows)
		}
	})

	t.Run("tenant-B only sees its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleQuery(rr, queryReqWithTenant(t, "MATCH (p:Person) RETURN p.name", "tenant-B"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Count != 1 {
			t.Errorf("tenant-B: want 1 match, got %d (rows=%v)", resp.Count, resp.Rows)
		}
	})

	t.Run("tenant-C (no data) sees nothing", func(t *testing.T) {
		rr := httptest.NewRecorder()
		server.handleQuery(rr, queryReqWithTenant(t, "MATCH (p:Person) RETURN p.name", "tenant-C"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Count != 0 {
			t.Errorf("tenant-C: want 0 (no data), got %d (rows=%v)", resp.Count, resp.Rows)
		}
	})
}

// TestA6c_Query_CreateLandsInCallerTenant pins that CREATE through
// /query stamps the resulting node with the caller's tenant — not
// the default. Ownership-stamping check, mirror of A6a's
// TestA6a_CreateNode_LandsInCallerTenant.
func TestA6c_Query_CreateLandsInCallerTenant(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	server.handleQuery(rr, queryReqWithTenant(t, `CREATE (p:User {name: "tenant-A user"}) RETURN p`, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}

	// The CREATE'd node must be visible to tenant-A (and only A).
	aNodes := server.graph.GetNodesByLabelForTenant("tenant-A", "User")
	if len(aNodes) != 1 {
		t.Errorf("tenant-A User count: want 1 (the just-created), got %d", len(aNodes))
	}
	defaultNodes := server.graph.GetNodesByLabelForTenant("default", "User")
	if len(defaultNodes) != 0 {
		t.Errorf("default User count: want 0 (must not leak), got %d — CREATE landed in default tenant", len(defaultNodes))
	}
}
