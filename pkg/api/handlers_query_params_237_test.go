package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/tenant"
)

// #237: /query must honor req.Parameters end-to-end. Before the fix, handleQuery
// called ExecuteWithContext and dropped req.Parameters, so a parameterized
// CREATE stored the literal "&{name}" instead of the value.
func TestQuery_ParametersHonoredEndToEnd(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	post := func(q string, params map[string]any) QueryResponse {
		t.Helper()
		body, err := json.Marshal(QueryRequest{Query: q, Parameters: params})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/query", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(tenant.WithTenant(req.Context(), "default"))
		rr := httptest.NewRecorder()
		server.handleQuery(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	post(`CREATE (n:Widget {name: $name})`, map[string]any{"name": "gizmo"})

	resp := post(`MATCH (n:Widget) RETURN n.name`, nil)
	if resp.Count != 1 {
		t.Fatalf("rows = %d, want 1", resp.Count)
	}
	if got := resp.Rows[0]["n.name"]; got != "gizmo" {
		t.Errorf("n.name = %#v, want \"gizmo\" (param not substituted via /query?)", got)
	}
}

// POST /query decodes the JSON body without UseNumber, so {"v": 7} reaches the
// executor as float64(7). MERGE on a stored integer id must still match it,
// not create a second node on each call.
func TestQuery_MergeNumericParamDoesNotDuplicate(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	post := func(q string, rawParams string) QueryResponse {
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
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return resp
	}

	post(`CREATE (p:M {id: 7})`, "")
	for i := 1; i <= 2; i++ {
		post(`MERGE (p:M {id: $v}) RETURN p`, `{"v": 7}`)
		if got := post(`MATCH (p:M) RETURN p`, "").Count; got != 1 {
			t.Fatalf("after MERGE call %d: M nodes = %d, want 1", i, got)
		}
	}
}
