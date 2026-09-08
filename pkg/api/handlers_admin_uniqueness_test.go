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

// uniquenessRuleReq issues a request at /admin/uniqueness-rules(/{name})
// through the SAME requireAdmin wrapper server.go registers the route
// with, so a non-admin caller actually hits the 403 the middleware sends —
// calling the handler function directly would bypass that check entirely.
func uniquenessRuleReq(t *testing.T, server *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()

	var chain http.HandlerFunc
	if strings.HasPrefix(path, "/admin/uniqueness-rules/") {
		chain = server.requireAdmin(server.handleUniquenessRule)
	} else {
		chain = server.requireAdmin(server.handleUniquenessRules)
	}
	chain(rr, req)
	return rr
}

// TestAdminUniquenessRules_RegisterIsIdempotentUpsert pins that POSTing the
// same rule twice upserts rather than erroring, per
// storage.RegisterUniquenessRule's own contract, and that the registry
// still holds exactly one entry for that name.
func TestAdminUniquenessRules_RegisterIsIdempotentUpsert(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-upsert", "")

	rule := storage.UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}

	first := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule)
	if first.Code != http.StatusOK {
		t.Fatalf("first register: status = %d, want 200, body=%s", first.Code, first.Body.String())
	}
	// Fix round 1 (Minor): the response body must be what the registry
	// holds, not an echo of the decoded request.
	var firstBody storage.UniquenessRule
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first response: %v; body=%s", err, first.Body.String())
	}
	if firstBody != rule {
		t.Errorf("first response body = %+v, want %+v", firstBody, rule)
	}

	second := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule)
	if second.Code != http.StatusOK {
		t.Fatalf("second register (upsert): status = %d, want 200, body=%s", second.Code, second.Body.String())
	}

	rules := server.graph.UniquenessRules()
	if len(rules) != 1 {
		t.Fatalf("registry holds %d rules after upserting the same name twice, want 1", len(rules))
	}
	if rules[0] != rule {
		t.Errorf("stored rule = %+v, want %+v", rules[0], rule)
	}

	list := uniquenessRuleReq(t, server, http.MethodGet, "/admin/uniqueness-rules", token, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200, body=%s", list.Code, list.Body.String())
	}
	var listBody UniquenessRulesListResponse
	if err := json.Unmarshal(list.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v; body=%s", err, list.Body.String())
	}
	if len(listBody.Rules) != 1 {
		t.Errorf("GET list returned %d rules, want 1", len(listBody.Rules))
	}
}

// TestAdminUniquenessRules_AuditEventsOnRegisterAndRemove pins R5: both
// register and remove write an audit event with ActionCreate/ActionDelete,
// ResourceType "uniqueness_rule", and the rule's name as ResourceID.
func TestAdminUniquenessRules_AuditEventsOnRegisterAndRemove(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-audit", "")

	rule := storage.UniquenessRule{Name: "audit_rule", Label: "Audited", PropertyKey: "sku"}

	if rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule); rr.Code != http.StatusOK {
		t.Fatalf("register: status = %d, body=%s", rr.Code, rr.Body.String())
	}

	registerEvents := server.inMemoryAuditLogger.GetEvents(nil)
	foundCreate := false
	for _, e := range registerEvents {
		if e.ResourceType == "uniqueness_rule" && e.ResourceID == rule.Name && e.Action == "create" {
			foundCreate = true
		}
	}
	if !foundCreate {
		t.Errorf("no create audit event found for uniqueness_rule %q; events=%+v", rule.Name, registerEvents)
	}

	if rr := uniquenessRuleReq(t, server, http.MethodDelete, "/admin/uniqueness-rules/"+rule.Name, token, nil); rr.Code != http.StatusOK {
		t.Fatalf("remove: status = %d, body=%s", rr.Code, rr.Body.String())
	}

	removeEvents := server.inMemoryAuditLogger.GetEvents(nil)
	foundDelete := false
	for _, e := range removeEvents {
		if e.ResourceType == "uniqueness_rule" && e.ResourceID == rule.Name && e.Action == "delete" {
			foundDelete = true
		}
	}
	if !foundDelete {
		t.Errorf("no delete audit event found for uniqueness_rule %q; events=%+v", rule.Name, removeEvents)
	}
}

// TestAdminUniquenessRules_ValidationRejectsBadInput covers R5's validation
// requirement: a control character or an overlong value in any of Name,
// Label, or PropertyKey is a 400 that never reaches the registry (fix
// round 1, Missing item: the original table covered Name only).
func TestAdminUniquenessRules_ValidationRejectsBadInput(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-validate", "")

	tests := []struct {
		name string
		rule storage.UniquenessRule
	}{
		{
			name: "control character in name",
			rule: storage.UniquenessRule{Name: "bad\x00name", Label: "Widget", PropertyKey: "sku"},
		},
		{
			name: "overlong name",
			rule: storage.UniquenessRule{Name: strings.Repeat("a", 101), Label: "Widget", PropertyKey: "sku"},
		},
		{
			name: "control character in label",
			rule: storage.UniquenessRule{Name: "good_name", Label: "bad\x00label", PropertyKey: "sku"},
		},
		{
			name: "overlong propertyKey",
			rule: storage.UniquenessRule{Name: "good_name", Label: "Widget", PropertyKey: strings.Repeat("a", 101)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, tc.rule)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", rr.Code, rr.Body.String())
			}
			if len(server.graph.UniquenessRules()) != 0 {
				t.Errorf("invalid rule reached the registry: %+v", server.graph.UniquenessRules())
			}
		})
	}
}

// TestAdminUniquenessRules_NonAdminGets403 pins that the admin routes are
// gated the same way every other /admin/* endpoint is.
func TestAdminUniquenessRules_NonAdminGets403(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "viewer", "uniq-viewer", "")

	rule := storage.UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}

	if rr := uniquenessRuleReq(t, server, http.MethodGet, "/admin/uniqueness-rules", token, nil); rr.Code != http.StatusForbidden {
		t.Errorf("GET list: status = %d, want 403, body=%s", rr.Code, rr.Body.String())
	}
	if rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule); rr.Code != http.StatusForbidden {
		t.Errorf("POST register: status = %d, want 403, body=%s", rr.Code, rr.Body.String())
	}
	if rr := uniquenessRuleReq(t, server, http.MethodDelete, "/admin/uniqueness-rules/claim_for_task", token, nil); rr.Code != http.StatusForbidden {
		t.Errorf("DELETE remove: status = %d, want 403, body=%s", rr.Code, rr.Body.String())
	}
}

// TestAdminUniquenessRules_DeleteIsAlways200 pins that removing an absent
// name is not an error (storage.RemoveUniquenessRule's own contract), so a
// caller cannot use this endpoint to probe which names exist.
func TestAdminUniquenessRules_DeleteIsAlways200(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-delete", "")

	rr := uniquenessRuleReq(t, server, http.MethodDelete, "/admin/uniqueness-rules/never_registered", token, nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}
