package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// ORDER BY an expression sorts over HTTP, and a bad one is the caller's
// mistake (400), not a silent no-op.
func TestAPI_Query_OrderByExpression(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, p := range []struct {
		name string
		age  int64
	}{{"dave", 41}, {"Bob", 20}, {"alice", 30}, {"eve", 35}} {
		if _, err := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(p.name),
			"age":  storage.IntValue(p.age),
		}); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}

	names := func(body []byte) string {
		var resp QueryResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		out := make([]string, len(resp.Rows))
		for i, r := range resp.Rows {
			out[i] = fmt.Sprint(r["n"])
		}
		return strings.Join(out, ",")
	}

	rr := makeQueryRequest(t, server, `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age + 1`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	if got, want := names(rr.Body.Bytes()), "Bob,alice,eve,dave"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}

	rr = makeQueryRequest(t, server, `MATCH (p:Person) RETURN p.name AS n ORDER BY p.age / 0`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "division by zero") {
		t.Errorf("body %s lacks the refusal text", rr.Body.String())
	}
}
