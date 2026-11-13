package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// <> with a null operand is null, and WHERE removes the row. Before this, a
// node with no such property matched `p.nosuch <> 1`.
func TestAPI_Query_NotEqualsMissingPropertyReturnsNoRows(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for i := int64(1); i <= 2; i++ {
		if _, err := server.graph.CreateNodeWithTenant("default", []string{"T"},
			map[string]storage.Value{"id": storage.IntValue(i)}); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query",
		map[string]any{"query": "MATCH (p:T) WHERE p.nosuch <> 1 RETURN p.id"}, "default"))
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
