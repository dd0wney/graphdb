package graphql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// Audit A6c-graphql-resolvers (2026-05-08): HTTP-level cross-tenant
// isolation gate. Pre-fix, /graphql was completely tenant-blind —
// resolvers ran with context.Background() (because pkg/graphql/http.go
// dropped r.Context() before invoking ExecuteQuery, see PR #23) AND
// called tenant-blind storage methods (this PR). Either layer alone
// being broken meant cross-tenant data was returned. This test pins
// both layers working together.

// graphqlReq builds a /graphql POST request with a tenant context,
// matching what withTenant middleware produces in production.
func graphqlReq(t *testing.T, query string, tenantID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(GraphQLRequest{Query: query})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// TestGraphQL_TenantIsolation_QueryByLabel pins that a tenant-A
// caller asking for `persons` only sees tenant-A's people, never
// tenant-B's. This is the cardinal cross-tenant read leak.
func TestGraphQL_TenantIsolation_QueryByLabel(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	if _, err := gs.CreateNodeWithTenant("tenant-A", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("alice-A"),
	}); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if _, err := gs.CreateNodeWithTenant("tenant-B", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("bob-B"),
	}); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema: %v", err)
	}
	handler := NewGraphQLHandler(schema)

	t.Run("tenant-A sees its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, graphqlReq(t, "{ persons { id } }", "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp GraphQLResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, _ := resp.Data.(map[string]any)
		persons, _ := data["persons"].([]any)
		if len(persons) != 1 {
			t.Errorf("tenant-A persons: want 1, got %d (resp=%v)", len(persons), resp)
		}
	})

	t.Run("tenant-B sees its own", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, graphqlReq(t, "{ persons { id } }", "tenant-B"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp GraphQLResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, _ := resp.Data.(map[string]any)
		persons, _ := data["persons"].([]any)
		if len(persons) != 1 {
			t.Errorf("tenant-B persons: want 1, got %d (resp=%v)", len(persons), resp)
		}
	})

	t.Run("tenant-C (no data) sees nothing", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, graphqlReq(t, "{ persons { id } }", "tenant-C"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp GraphQLResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, _ := resp.Data.(map[string]any)
		persons, _ := data["persons"].([]any)
		if len(persons) != 0 {
			t.Errorf("tenant-C persons: want 0 (no data), got %d (resp=%v)", len(persons), resp)
		}
	})
}

// TestGraphQL_TenantIsolation_QueryByID pins that fetching a specific
// node by ID returns null (not the node) when called from another
// tenant — the existence-leak gate.
func TestGraphQL_TenantIsolation_QueryByID(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	aNode, err := gs.CreateNodeWithTenant("tenant-A", []string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("alice-A"),
	})
	if err != nil {
		t.Fatalf("seed A: %v", err)
	}

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema: %v", err)
	}
	handler := NewGraphQLHandler(schema)

	query := fmt.Sprintf(`{ person(id: "%d") { id labels } }`, aNode.ID)

	t.Run("owner sees its own node", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, graphqlReq(t, query, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp GraphQLResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		data, _ := resp.Data.(map[string]any)
		if data["person"] == nil {
			t.Errorf("tenant-A: expected to see own node, got nil (resp=%v)", resp)
		}
	})

	t.Run("cross-tenant returns nil", func(t *testing.T) {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, graphqlReq(t, query, "tenant-B"))
		// Cross-tenant: GetNodeForTenant returns ErrNodeNotFound;
		// graphql resolver surfaces this as an error in the response.
		// Either way, no node data should appear in resp.Data.person.
		var resp GraphQLResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		data, _ := resp.Data.(map[string]any)
		if data != nil && data["person"] != nil {
			t.Errorf("tenant-B leaked tenant-A's node: %v", data["person"])
		}
	})
}
