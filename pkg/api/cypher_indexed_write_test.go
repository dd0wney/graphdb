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

// A parameter in the second condition of an indexed query was lost: the lookup
// dropped the "$one" binding, the comparison was unknown, a read returned no row
// and a write changed nothing, with a 200.
func TestQuery_IndexedParameterReachesWhere(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreatePropertyIndex("pid", storage.TypeInt); err != nil {
		t.Fatalf("CreatePropertyIndex: %v", err)
	}
	node, err := server.graph.CreateNodeWithTenant("default", []string{"T"}, map[string]storage.Value{
		"pid": storage.IntValue(1),
		"k":   storage.IntValue(3),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}

	post := func(query string, params map[string]any) QueryResponse {
		t.Helper()
		rr := httptest.NewRecorder()
		server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
			"query": query, "parameters": params,
		}, "default"))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", query, rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	read := post("MATCH (p:T) WHERE p.pid = 1 AND p.k > $one RETURN p.pid", map[string]any{"one": 0})
	if len(read.Rows) != 1 {
		t.Errorf("read rows = %v, want one row for k = 3 > 0", read.Rows)
	}

	post("MATCH (p:T) WHERE p.pid = 1 AND p.k > $one SET p.x = 1", map[string]any{"one": 0})
	if v, ok := storedProperty(t, server, node.ID, "x"); !ok || v.Type != storage.TypeInt {
		t.Errorf("x = %+v (present %v) after a SET whose WHERE is true, want it written", v, ok)
	}

	post("MATCH (p:T) WHERE p.pid = 1 AND p.k > $one DELETE p", map[string]any{"one": 5})
	if _, err := server.graph.GetNodeForTenant(node.ID, "default"); err != nil {
		t.Errorf("the node is gone after a DELETE whose WHERE is false: %v", err)
	}
}
