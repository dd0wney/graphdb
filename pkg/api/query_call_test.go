package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestAPI_Query_CallProcedureReturnsRows pins the REST surface of a bare CALL.
// Before the CallStep fix every CALL answered {"affected": 1} and never ran the
// procedure, so a client could not tell a missing procedure from a real one.
func TestAPI_Query_CallProcedureReturnsRows(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	var ids []uint64
	for _, name := range []string{"a", "b", "c"} {
		n, err := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		ids = append(ids, n.ID)
	}
	if _, err := server.graph.CreateEdge(ids[0], ids[1], "KNOWS", nil, 1.0); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}

	rr := makeQueryRequest(t, server, "CALL algo.pageRank()")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("rows = %d, want 1: %+v", len(resp.Rows), resp.Rows)
	}
	scores, ok := resp.Rows[0]["scores"].(map[string]any)
	if !ok {
		t.Fatalf("scores = %#v (%T), want a JSON object", resp.Rows[0]["scores"], resp.Rows[0]["scores"])
	}
	if len(scores) != len(ids) {
		t.Errorf("scores has %d entries, want %d", len(scores), len(ids))
	}
}
