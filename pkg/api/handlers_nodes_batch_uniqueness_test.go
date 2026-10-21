package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func postBatchNodes(t *testing.T, server *Server, nodes []NodeRequest) BatchNodeResponse {
	t.Helper()
	buf, err := json.Marshal(BatchNodeRequest{Nodes: nodes})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/nodes/batch", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleBatchNodes(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /nodes/batch = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var resp BatchNodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rr.Body.String())
	}
	return resp
}

// countWidgets counts the stored nodes that carry the given sku, which is the
// ground truth the batch response could misreport.
func countWidgets(t *testing.T, server *Server, sku string) int {
	t.Helper()
	nodes, err := server.graph.FindNodesByLabelAcrossTenants("Widget")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants: %v", err)
	}
	n := 0
	for _, node := range nodes {
		if v, ok := node.Properties["sku"]; ok {
			if s, err := v.AsString(); err == nil && s == sku {
				n++
			}
		}
	}
	return n
}

// ADR 0001 promised that a registered rule holds on every write surface, and
// POST /nodes enforced it, but POST /nodes/batch called CreateNodeWithTenant
// and stored a duplicate for every item that repeated a key. The batch keeps
// its partial-success shape (CC7, CC20): a refused item is an error entry,
// and the other items still apply.
func TestBatchNodes_UniquenessRuleEnforced(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "widget_sku", Label: "Widget", PropertyKey: "sku",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	if rr := postNode(t, server, NodeRequest{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-1"}}); rr.Code != http.StatusCreated {
		t.Fatalf("seed create = %d, body=%s", rr.Code, rr.Body.String())
	}

	resp := postBatchNodes(t, server, []NodeRequest{
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-1"}}, // duplicate of the seed
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-2"}},
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-2"}}, // duplicate inside the batch
		{Labels: []string{"Widget"}, Properties: map[string]any{"name": "no sku"}},
	})

	if resp.Created != 1 || resp.Failed != 3 {
		t.Errorf("created=%d failed=%d, want 1 and 3; errors=%+v", resp.Created, resp.Failed, resp.Errors)
	}
	wantIndex := []int{0, 2, 3}
	if len(resp.Errors) != len(wantIndex) {
		t.Fatalf("errors = %+v, want indexes %v", resp.Errors, wantIndex)
	}
	for i, e := range resp.Errors {
		if e.Index != wantIndex[i] {
			t.Errorf("errors[%d].index = %d, want %d", i, e.Index, wantIndex[i])
		}
	}
	if !strings.Contains(resp.Errors[0].Error, "unique constraint violation") {
		t.Errorf("errors[0] = %q, want a unique constraint violation", resp.Errors[0].Error)
	}
	if !strings.Contains(resp.Errors[2].Error, "requires a \"sku\" property") {
		t.Errorf("errors[2] = %q, want the missing-property refusal", resp.Errors[2].Error)
	}

	for sku, want := range map[string]int{"w-1": 1, "w-2": 1} {
		if got := countWidgets(t, server, sku); got != want {
			t.Errorf("stored Widget nodes with sku %s = %d, want %d", sku, got, want)
		}
	}
}

// A batch item refused for a missing required rule must not name the rule,
// as the single create's fixed 503 does not (ADR 0001, R4).
func TestBatchNodes_RequiredRuleMissingNamesOnlyTheLabel(t *testing.T) {
	server, cleanup := setupTestServerWithRequiredRules(t, []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
	})
	defer cleanup()

	resp := postBatchNodes(t, server, []NodeRequest{
		{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}},
		{Labels: []string{"Other"}, Properties: map[string]any{"x": "y"}},
	})

	if resp.Created != 1 || resp.Failed != 1 || len(resp.Errors) != 1 || resp.Errors[0].Index != 0 {
		t.Fatalf("created=%d failed=%d errors=%+v, want item 0 refused and item 1 created", resp.Created, resp.Failed, resp.Errors)
	}
	want := `a required uniqueness rule for label "Claim" is not registered; contact the administrator`
	if resp.Errors[0].Error != want {
		t.Errorf("errors[0] = %q, want %q", resp.Errors[0].Error, want)
	}
}

// Two rules that both match an item's labels are refused, and the item error
// must not carry the rule names that storage's error text contains.
func TestBatchNodes_MultipleRulesDoesNotNameRules(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, r := range []storage.UniquenessRule{
		{Name: "secret_rule_a", Label: "Widget", PropertyKey: "sku"},
		{Name: "secret_rule_b", Label: "Widget", PropertyKey: "serial"},
	} {
		if err := server.graph.RegisterUniquenessRule(r); err != nil {
			t.Fatalf("RegisterUniquenessRule: %v", err)
		}
	}

	resp := postBatchNodes(t, server, []NodeRequest{
		{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "a", "serial": "b"}},
	})
	if resp.Created != 0 || len(resp.Errors) != 1 {
		t.Fatalf("created=%d errors=%+v, want one refused item", resp.Created, resp.Errors)
	}
	if strings.Contains(resp.Errors[0].Error, "secret_rule") {
		t.Errorf("item error %q names a rule", resp.Errors[0].Error)
	}
}
