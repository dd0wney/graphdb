package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestCypherOrderBy_NodeWithoutPropertyIsLast pins the null position through
// POST /query. A null sorted by the position of the node in the store before:
// it was "equal" to every other value, so the answer followed the input order.
func TestCypherOrderBy_NodeWithoutPropertyIsLast(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// The node with no age is created first and in the middle, so a result
	// that leaves it where it was cannot pass.
	people := []struct {
		name string
		age  int64 // -1: no age
	}{{"nobody", -1}, {"old", 41}, {"young", 20}}
	for _, p := range people {
		props := map[string]storage.Value{"name": storage.StringValue(p.name)}
		if p.age >= 0 {
			props["age"] = storage.IntValue(p.age)
		}
		if _, err := server.graph.CreateNodeWithTenant("default", []string{"Person"}, props); err != nil {
			t.Fatal(err)
		}
	}

	names := func(order string) string {
		rr := httptest.NewRecorder()
		server.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query", map[string]any{
			"query": "MATCH (p:Person) RETURN p.name AS name ORDER BY p.age" + order,
		}, "default"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Rows []map[string]any `json:"rows"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v body=%s", err, rr.Body.String())
		}
		out := make([]string, len(resp.Rows))
		for i, r := range resp.Rows {
			out[i], _ = r["name"].(string)
		}
		return strings.Join(out, ",")
	}

	if got := names(""); got != "young,old,nobody" {
		t.Errorf("ORDER BY p.age = %s, want young,old,nobody", got)
	}
	if got := names(" DESC"); got != "nobody,old,young" {
		t.Errorf("ORDER BY p.age DESC = %s, want nobody,old,young", got)
	}
}
