package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A PUT /nodes/{id} that gives a node a value another node holds under a
// registered rule is a 409 with the create's body, not a 500.
func TestPutNode_UniquenessConflictNamesHolder(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const secretRule = "secret_rule_name"
	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: secretRule, Label: "Widget", PropertyKey: "sku",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	const tid = "put-uniq-tenant"
	a, err := server.graph.CreateNodeWithTenant(tid, []string{"Widget"}, map[string]storage.Value{"sku": storage.StringValue("w-1")})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := server.graph.CreateNodeWithTenant(tid, []string{"Widget"}, map[string]storage.Value{"sku": storage.StringValue("w-2")})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/nodes/%d", b.ID),
		map[string]any{"properties": map[string]any{"sku": "w-1"}}, tid))
	if rr.Code != http.StatusConflict {
		t.Fatalf("PUT status = %d, want 409, body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	got, ok := body["conflicting_node_id"].(float64)
	if !ok || uint64(got) != a.ID {
		t.Errorf(`"conflicting_node_id" = %v, want %d; body=%s`, body["conflicting_node_id"], a.ID, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), secretRule) {
		t.Errorf("409 body names the rule: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), tid) {
		t.Errorf("409 body names the tenant: %s", rr.Body.String())
	}
}
