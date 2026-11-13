package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// A bare unaliased RETURN n names its column "n", and each row carries the
// same key. Before the fix both said "n." (variable, dot, empty property).
func TestAPI_Query_BareVariableColumnName(t *testing.T) {
	server, cleanup, _ := setupTestServerWithLabelNode(t)
	defer cleanup()

	rr := makeQueryRequest(t, server, "MATCH (n:Label) RETURN n")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Columns) != 1 || resp.Columns[0] != "n" {
		t.Fatalf("columns = %q, want [n]", resp.Columns)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %+v", len(resp.Rows), resp.Rows)
	}
	if _, ok := resp.Rows[0]["n"].(map[string]any); !ok {
		t.Errorf(`row key "n" = %v (%T), want the decoded node; row %v`,
			resp.Rows[0]["n"], resp.Rows[0]["n"], resp.Rows[0])
	}
}
