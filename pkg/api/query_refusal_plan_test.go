package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// With no index, a row that one conjunct excludes must not stop the query
// because of an error in another conjunct. Node 2 holds a string in q, and
// p.id = 1 excludes it, so the query answers with node 1 only.
func TestAPI_Query_ExcludedRowDoesNotRefuseWithoutIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for _, props := range []map[string]storage.Value{
		{"id": storage.IntValue(1), "q": storage.BoolValue(true)},
		{"id": storage.IntValue(2), "q": storage.StringValue("str")},
	} {
		if _, err := server.graph.CreateNodeWithTenant("default", []string{"T"}, props); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query",
		map[string]any{"query": "MATCH (p:T) WHERE p.q AND p.id = 1 RETURN p.id AS id"}, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 || resp.Count != 1 {
		t.Errorf("rows = %v, count = %d, want one row", resp.Rows, resp.Count)
	}
}
