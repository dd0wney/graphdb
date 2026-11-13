package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// NOT (x IN [2, null]) is null for a node whose property is not 2, because
// the null element might equal it. WHERE removes a null row, so the answer has
// no rows. Before this, the null element was skipped and the node matched.
func TestAPI_Query_NotInListWithNullReturnsNoRows(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for i := int64(1); i <= 2; i++ {
		if _, err := server.graph.CreateNodeWithTenant("default", []string{"T"},
			map[string]storage.Value{"id": storage.IntValue(i), "a": storage.IntValue(i)}); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query",
		map[string]any{"query": "MATCH (p:T) WHERE NOT (p.a IN [2, null]) RETURN p.id"}, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 0 || resp.Count != 0 {
		t.Errorf("rows = %v, count = %d, want none", resp.Rows, resp.Count)
	}
}
