package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenant"
)

// postQueryJSON sends raw JSON parameters, so a number reaches the executor
// the way a real client's number does: as float64.
func postQueryJSON(t *testing.T, server *Server, q, rawParams string) *httptest.ResponseRecorder {
	t.Helper()
	var params map[string]any
	if rawParams != "" {
		if err := json.Unmarshal([]byte(rawParams), &params); err != nil {
			t.Fatalf("params: %v", err)
		}
	}
	body, err := json.Marshal(QueryRequest{Query: q, Parameters: params})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), "default"))
	rr := httptest.NewRecorder()
	server.handleQuery(rr, req)
	return rr
}

// OPTIONAL MATCH left its *ParameterRef unresolved, so {"v": 7} bound null
// where the stored integer 7 should have matched.
func TestQuery_OptionalMatchPatternParameterBinds(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if rr := postQueryJSON(t, server, `CREATE (p:Person {id: 7, name: "Gus"})`, ""); rr.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rr.Code, rr.Body.String())
	}

	rr := postQueryJSON(t, server, `OPTIONAL MATCH (p:Person {id: $v}) RETURN p.name`, `{"v": 7}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("rows = %d, want 1: %s", resp.Count, rr.Body.String())
	}
	if got := resp.Rows[0]["p.name"]; got != "Gus" {
		t.Errorf("p.name = %#v, want \"Gus\"", got)
	}
}

// A pattern parameter with no "parameters" field takes the Execute path, which
// used to return no rows with no error.
func TestQuery_PatternParameterWithoutParametersIsRefused(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := postQueryJSON(t, server, `OPTIONAL MATCH (p:Person {id: $v}) RETURN p.name`, "")
	if rr.Code == http.StatusOK {
		t.Fatalf("a query with an unsupplied parameter returned 200: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "$v") {
		t.Errorf("error does not name the parameter: %s", rr.Body.String())
	}
}
