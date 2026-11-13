package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// With a property index on pid, `MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p`
// deleted a node whose k is 1. The optimizer ran the DELETE on the row the index
// returned and the WHERE after it. The index is an access path, so the node
// must stay and a later count must still see it.
func TestQuery_IndexedDeleteHonoursWhere(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	node, err := server.graph.CreateNodeWithTenant("default", []string{"T"}, map[string]storage.Value{
		"pid": storage.IntValue(1),
		"k":   storage.IntValue(1),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	runCypher(t, server, "MATCH (p:T) WHERE p.pid = 1 AND p.k > 5 DELETE p", nil)

	if _, err := server.graph.GetNodeForTenant(node.ID, "default"); err != nil {
		t.Fatalf("the node is gone after a DELETE whose WHERE is false: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
		"query": "MATCH (p:T) RETURN count(p) AS c",
	}, "default"))
	if rr.Code != http.StatusOK {
		t.Fatalf("count: %d %s", rr.Code, rr.Body.String())
	}
	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 || resp.Rows[0]["c"] != float64(1) {
		t.Errorf("count rows = %v, want one row with c = 1", resp.Rows)
	}
}
