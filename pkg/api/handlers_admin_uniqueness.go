package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/dd0wney/graphdb/pkg/audit"
	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/validation"
)

// UniquenessRulesListResponse is the GET /admin/uniqueness-rules body
// (ADR 0001).
type UniquenessRulesListResponse struct {
	Rules []storage.UniquenessRule `json:"rules"`
}

// handleUniquenessRules dispatches GET (list) and POST (register/upsert)
// on /admin/uniqueness-rules. Registered with s.requireAdmin next to
// /admin/backup (server.go) — the mux-level wrapper is what actually
// enforces the admin role; the handler-level requireAdminClaims calls
// below are defense-in-depth, matching handleCreateTenant /
// handleDeleteTenant (handlers_tenant.go).
func (s *Server) handleUniquenessRules(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listUniquenessRules(w, r) }).
		Post(func() { s.registerUniquenessRule(w, r) }).
		NotAllowed()
}

// handleUniquenessRule dispatches DELETE /admin/uniqueness-rules/{name}.
func (s *Server) handleUniquenessRule(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Delete(func() { s.removeUniquenessRule(w, r) }).
		NotAllowed()
}

// listUniquenessRules returns every registered rule. No audit event: a
// read has nothing to record beyond what request logging already
// captures — handleListTenants' GET follows the same convention.
func (s *Server) listUniquenessRules(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, UniquenessRulesListResponse{Rules: s.graph.UniquenessRules()})
}

// registerUniquenessRule handles POST /admin/uniqueness-rules: upsert by
// name (storage.RegisterUniquenessRule is idempotent), 200 with the stored
// rule. Validated with pkg/validation before it reaches storage, so a
// malformed body is a 400 naming the field it failed on, ahead of
// storage's own (differently worded) validation.
func (s *Server) registerUniquenessRule(w http.ResponseWriter, r *http.Request) {
	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	var req storage.UniquenessRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateUniquenessRuleBody(req); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.graph.RegisterUniquenessRule(req); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// R5: audit event on register, following handlers_tenant.go's
	// handleCreateTenant pattern.
	s.logAuditEvent(&audit.Event{
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionCreate,
		ResourceType: "uniqueness_rule",
		ResourceID:   req.Name,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	s.respondJSON(w, http.StatusOK, req)
}

// removeUniquenessRule handles DELETE /admin/uniqueness-rules/{name}. 200
// always — storage.RemoveUniquenessRule does not error on an absent name —
// so this endpoint cannot be used to probe which names are registered.
func (s *Server) removeUniquenessRule(w http.ResponseWriter, r *http.Request) {
	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	name, ok := s.NewPathExtractor(w, r).ExtractString("/admin/uniqueness-rules/")
	if !ok {
		return
	}
	// Same name rule as the POST body's Name field: a malformed path
	// segment is a 400, not a silent no-op 200.
	if err := validation.ValidatePropertyKey(name); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.graph.RemoveUniquenessRule(name); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "remove uniqueness rule"))
		return
	}

	// R5: audit event on remove, following handlers_tenant.go's
	// handleDeleteTenant pattern.
	s.logAuditEvent(&audit.Event{
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionDelete,
		ResourceType: "uniqueness_rule",
		ResourceID:   name,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	s.respondJSON(w, http.StatusOK, map[string]string{"status": "removed", "name": name})
}

// validateUniquenessRuleBody validates a POST /admin/uniqueness-rules body
// with pkg/validation: ValidatePropertyKey for Name and PropertyKey (the
// same identifier shape pkg/storage's own unexported validateUniquenessRule
// enforces — see uniquenessNamePattern's doc comment in
// pkg/storage/uniqueness_rules.go for why that package keeps its own copy
// rather than importing this one), and ValidateLabel for Label.
func validateUniquenessRuleBody(r storage.UniquenessRule) error {
	if err := validation.ValidatePropertyKey(r.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if err := validation.ValidateLabel(r.Label); err != nil {
		return fmt.Errorf("label: %w", err)
	}
	if err := validation.ValidatePropertyKey(r.PropertyKey); err != nil {
		return fmt.Errorf("propertyKey: %w", err)
	}
	return nil
}
