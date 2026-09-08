package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupTestServerWithRequiredRules builds a test server whose storage
// config carries the given required-uniqueness-rule list (ADR 0001,
// StorageConfig.RequiredUniquenessRules). Mirrors setupTestServer
// (server_test.go), which has no way to reach that one field.
func setupTestServerWithRequiredRules(t *testing.T, required []storage.RequiredUniquenessRule) (*Server, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "api-test-uniqueness-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cfg := storage.DefaultStorageConfig(tmpDir)
	cfg.RequiredUniquenessRules = required
	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}

	server, err := NewServerWithDataDir(gs, 8080, tmpDir)
	if err != nil {
		_ = gs.Close()
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("NewServerWithDataDir: %v", err)
	}

	cleanup := func() {
		_ = gs.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return server, cleanup
}

// postNode issues a POST /nodes directly at the handler, bypassing the
// requireAuth/withTenant chain — the same pattern
// handlers_nodes_claim_uniqueness_test.go already uses.
func postNode(t *testing.T, server *Server, body NodeRequest) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleNodes(rr, req)
	return rr
}

// TestCreateNode_GenericLabelUniquenessEnforced is the REST half of the ADR
// 0001 parity test (see the GraphQL twin,
// TestCreateNodeMutation_GenericLabelUniquenessEnforced): a registered rule
// must be enforced whatever the rule's label is called.
//
// Before stage 2 wired CreateNodeWithUniquenessRulesForTenant into
// createNode, a non-Claim label like "Widget" saw no enforcement on this
// surface either — the handler only ever checked the literal string
// "Claim".
func TestCreateNode_GenericLabelUniquenessEnforced(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "widget_sku", Label: "Widget", PropertyKey: "sku",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	first := postNode(t, server, NodeRequest{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-1"}})
	if first.Code != http.StatusCreated {
		t.Fatalf("first Widget create should succeed, got %d body=%s", first.Code, first.Body.String())
	}

	second := postNode(t, server, NodeRequest{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-1"}})
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate Widget sku should be 409, got %d body=%s", second.Code, second.Body.String())
	}

	third := postNode(t, server, NodeRequest{Labels: []string{"Widget"}, Properties: map[string]any{"sku": "w-2"}})
	if third.Code != http.StatusCreated {
		t.Errorf("distinct sku should succeed, got %d body=%s", third.Code, third.Body.String())
	}
}

// TestCreateNode_RequiredRuleMissingIs503 pins R4: a StorageConfig
// required-rule pair covering the write's label, with no matching rule
// registered, refuses the write with a fixed 503 body — distinct from the
// 409 a registered rule's own conflict returns, and distinct from the 201
// an uncovered label still gets.
func TestCreateNode_RequiredRuleMissingIs503(t *testing.T) {
	server, cleanup := setupTestServerWithRequiredRules(t, []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
	})
	defer cleanup()

	rr := postNode(t, server, NodeRequest{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing required rule should be 503, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, rr.Body.String())
	}
	if body.Error != "required uniqueness rule missing" {
		t.Errorf(`Error = %q, want "required uniqueness rule missing"`, body.Error)
	}
	if body.Code != http.StatusServiceUnavailable {
		t.Errorf("Code = %d, want %d", body.Code, http.StatusServiceUnavailable)
	}
	want := `a required uniqueness rule for label "Claim" is not registered; contact the administrator`
	if body.Message != want {
		t.Errorf("Message = %q, want %q", body.Message, want)
	}

	// An uncovered label is unaffected — the required pair only names Claim.
	other := postNode(t, server, NodeRequest{Labels: []string{"Other"}, Properties: map[string]any{"x": "y"}})
	if other.Code != http.StatusCreated {
		t.Errorf("uncovered label should succeed, got %d body=%s", other.Code, other.Body.String())
	}

	// Registering the rule turns the SAME write from 503 into 201, and a
	// repeat becomes 409 — three distinguishable states on one label.
	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	created := postNode(t, server, NodeRequest{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}})
	if created.Code != http.StatusCreated {
		t.Fatalf("write should succeed once the rule is registered, got %d body=%s", created.Code, created.Body.String())
	}
	dup := postNode(t, server, NodeRequest{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}})
	if dup.Code != http.StatusConflict {
		t.Errorf("duplicate should now be 409, not %d body=%s", dup.Code, dup.Body.String())
	}
}

// TestCreateNode_RequiredRuleMissingBodyNamesNoRuleOrList pins R4's no-leak
// requirement directly: the 503 body must name only the caller's own
// label, never a rule Name, and never another entry in
// StorageConfig.RequiredUniquenessRules the caller's request never asked
// about.
func TestCreateNode_RequiredRuleMissingBodyNamesNoRuleOrList(t *testing.T) {
	server, cleanup := setupTestServerWithRequiredRules(t, []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
		{Name: "invoice_number", Label: "Invoice"},
	})
	defer cleanup()

	rr := postNode(t, server, NodeRequest{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing required rule should be 503, got %d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, leak := range []string{"claim_for_task", "invoice_number", "Invoice"} {
		if strings.Contains(body, leak) {
			t.Errorf("503 body leaks %q, which the caller's own request never named: %s", leak, body)
		}
	}
	var decoded ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, body)
	}
	if !strings.Contains(decoded.Message, "Claim") {
		t.Errorf("503 body should still name the caller's own label %q: %s", "Claim", body)
	}
}

// TestCreateNode_AdminAuthenticatedWriteStillEnforced pins that the
// required-rule exemption is by ROUTE (the admin registry endpoints,
// commit 5) and not by caller ROLE: an admin-authenticated /nodes write
// covered by a missing required rule is refused exactly like an
// unauthenticated one.
func TestCreateNode_AdminAuthenticatedWriteStillEnforced(t *testing.T) {
	server, cleanup := setupTestServerWithRequiredRules(t, []storage.RequiredUniquenessRule{
		{Name: "claim_for_task", Label: "Claim"},
	})
	defer cleanup()

	token := mintTestToken(t, server, "admin", "uniq-admin", "")

	buf, err := json.Marshal(NodeRequest{Labels: []string{"Claim"}, Properties: map[string]any{"for_task": "t1"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	chain := server.requireAuth(server.withTenant(server.handleNodes))
	chain(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin-authenticated write covered by a missing required rule should still be 503, got %d body=%s",
			rr.Code, rr.Body.String())
	}
}
