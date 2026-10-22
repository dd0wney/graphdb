package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
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

// A second rule name on a key another rule covers is a conflict with the
// registry's state, not a malformed request: 409, and the registry keeps one
// rule.
func TestAdminUniquenessRules_SecondNameOnAHeldKeyIs409(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-key-taken", "")

	first := storage.UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}
	if rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, first); rr.Code != http.StatusOK {
		t.Fatalf("first register: status = %d, body=%s", rr.Code, rr.Body.String())
	}
	second := storage.UniquenessRule{Name: "claim_for_task_2", Label: "Claim", PropertyKey: "for_task"}
	rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, second)
	if rr.Code != http.StatusConflict {
		t.Fatalf("second name on Claim.for_task: status = %d, want 409, body=%s", rr.Code, rr.Body.String())
	}
	if got := len(server.graph.UniquenessRules()); got != 1 {
		t.Errorf("registry holds %d rules after a refused register, want 1", got)
	}
}

// A registry that cannot reach the disk is the server's failure, not the
// caller's: 500, a body that names no file, and no rule left behind. Until
// this test the handler answered 400 with the storage error verbatim, which
// told an admin their request was malformed and printed the data-directory
// path of rules.json.new.
func TestAdminUniquenessRules_PersistFailureIs500(t *testing.T) {
	server, faults, cleanup := setupTestServerWithFaultFS(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-persist-fail", "")

	faults.FailOpen(vfstest.Always)
	rule := storage.UniquenessRule{Name: "claim_for_task", Label: "Claim", PropertyKey: "for_task"}
	rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule)
	// Read Fired before Clear: Clear resets it.
	fired := faults.Fired()
	faults.Clear()

	if !fired {
		t.Fatalf("the open fault never fired, so this test exercised no disk failure")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("register with the disk failing: status = %d, want 500, body=%s", rr.Code, rr.Body.String())
	}
	for _, leak := range []string{"rules.json", vfstest.ErrInjected.Error()} {
		if strings.Contains(rr.Body.String(), leak) {
			t.Errorf("response body leaks %q: %s", leak, rr.Body.String())
		}
	}
	if got := len(server.graph.UniquenessRules()); got != 0 {
		t.Errorf("registry holds %d rules after a failed persist, want 0", got)
	}
}

// The rule cap is a limit the caller ran into, so it stays a 4xx. It needs
// its own test because the handler now answers 500 for any error it cannot
// name as the caller's fault.
func TestAdminUniquenessRules_RuleCapIs400(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	token := mintTestToken(t, server, "admin", "uniq-admin-cap", "")

	// Fill the registry through storage until it refuses, so the test does
	// not copy the cap's value from an unexported constant.
	filled := false
	for i := 0; i < 10000; i++ {
		err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
			Name: fmt.Sprintf("rule_%05d", i), Label: "L", PropertyKey: fmt.Sprintf("k%05d", i),
		})
		if errors.Is(err, storage.ErrUniquenessRuleLimit) {
			filled = true
			break
		}
		if err != nil {
			t.Fatalf("fill rule %d: %v", i, err)
		}
	}
	if !filled {
		t.Fatalf("registry never reached its cap")
	}

	rule := storage.UniquenessRule{Name: "one_too_many", Label: "L", PropertyKey: "one_too_many"}
	rr := uniquenessRuleReq(t, server, http.MethodPost, "/admin/uniqueness-rules", token, rule)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("register past the cap: status = %d, want 400, body=%s", rr.Code, rr.Body.String())
	}
}
