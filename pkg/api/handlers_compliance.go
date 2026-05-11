package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/masking"
)

// Compliance API endpoint defaults.
const (
	complianceAuditLogDefaultLimit = 100
	complianceAuditLogMaxLimit     = 1000
)

// handleComplianceAuditLog serves GET /v1/compliance/audit-log — a
// tenant-scoped read of the audit log.
//
// Scope resolution (per docs/F3_COMPLIANCE_API_DESIGN.md §3 Decision 1c):
//   - Non-admin: always own tenant. Header/query overrides ignored.
//   - Admin, no X-Tenant-ID, no ?tenant=*: own tenant.
//   - Admin, X-Tenant-ID: <other>: that tenant's events (withTenant
//     middleware applies the override before this handler runs).
//   - Admin, ?tenant=*: cross-tenant (Filter.TenantID stays empty).
//
// Query params mirror /api/v1/security/audit/logs: user_id, username,
// action, resource_type, status, start_time, end_time. Tenant is sourced
// from context + admin overrides, not from a caller-supplied query.
//
// Pagination: ?limit=N (default 100, capped at 1000), ?offset=N.
// Events come from the in-memory logger in append-only insertion order;
// offset+limit is a stable cursor over that order.
func (s *Server) handleComplianceAuditLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	q := r.URL.Query()

	// Default scope: the tenant withTenant resolved for this request.
	// Admin + ?tenant=* widens to cross-tenant; non-admins cannot
	// widen even by supplying the query (admin gate inside the if).
	filterTenant := getTenantFromContext(r)
	crossTenant := false
	claims, hasClaims := r.Context().Value(claimsContextKey).(*auth.Claims)
	if hasClaims && claims.Role == auth.RoleAdmin && q.Get("tenant") == "*" {
		filterTenant = ""
		crossTenant = true
	}

	filter := &audit.Filter{TenantID: filterTenant}
	if v := q.Get("user_id"); v != "" {
		filter.UserID = v
	}
	if v := q.Get("username"); v != "" {
		filter.Username = v
	}
	if v := q.Get("action"); v != "" {
		filter.Action = audit.Action(v)
	}
	if v := q.Get("resource_type"); v != "" {
		filter.ResourceType = audit.ResourceType(v)
	}
	if v := q.Get("status"); v != "" {
		filter.Status = audit.Status(v)
	}
	if v := q.Get("start_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.StartTime = &t
		}
	}
	if v := q.Get("end_time"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.EndTime = &t
		}
	}

	limit := complianceAuditLogDefaultLimit
	if v := q.Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			if l > complianceAuditLogMaxLimit {
				l = complianceAuditLogMaxLimit
			}
			limit = l
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}

	events := s.inMemoryAuditLogger.GetEvents(filter)
	total := len(events)

	// Slice safely — offset may be past end after concurrent log
	// rotation/filtering. Clamp rather than 4xx; an empty page is a
	// valid pagination terminator.
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := events[start:end]

	response := map[string]any{
		"events":   page,
		"count":    len(page),
		"total":    total,
		"offset":   offset,
		"limit":    limit,
		"has_more": end < total,
	}
	if crossTenant {
		response["cross_tenant"] = true
	} else {
		response["tenant"] = filterTenant
	}

	s.respondJSON(w, http.StatusOK, response)
}

// maskingPolicyRequest is the JSON body for POST
// /v1/compliance/masking-policy. Mirrors masking.Policy but drops
// server-managed fields (TenantID — set from request context;
// UpdatedAt — server-stamped).
type maskingPolicyRequest struct {
	Properties map[string]masking.MaskingStrategy `json:"properties,omitempty"`
	AutoDetect bool                               `json:"auto_detect"`
}

// validMaskingStrategies is the allow-list for incoming policy
// strategies. We validate at the request boundary rather than rely
// on Masker's "unknown → maskPartial" default so operators get a
// clear 400 on typos.
var validMaskingStrategies = map[masking.MaskingStrategy]bool{
	masking.StrategyFull:     true,
	masking.StrategyPartial:  true,
	masking.StrategyHash:     true,
	masking.StrategyRedact:   true,
	masking.StrategyTokenize: true,
	masking.StrategyNone:     true,
}

// handleComplianceMaskingPolicySet serves POST
// /v1/compliance/masking-policy. Sets or replaces the masking policy
// for a tenant. Per design doc §3 Decision 1c / §4 PR-3:
//
//   - Admin-only operation (claims.Role == RoleAdmin).
//   - Target tenant comes from withTenant context resolution
//     (X-Tenant-ID admin-override applies).
//   - Body: maskingPolicyRequest JSON.
//
// Audit: emits AuditActionUpdate / ResourceCompliance event on success.
func (s *Server) handleComplianceMaskingPolicySet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !ok || claims.Role != auth.RoleAdmin {
		s.respondError(w, http.StatusForbidden, "Admin role required")
		return
	}

	tenantID := getTenantFromContext(r)
	if tenantID == "" {
		s.respondError(w, http.StatusBadRequest, "No tenant resolvable from context")
		return
	}

	var req maskingPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	// Validate strategies before we Set, so an operator with a typo
	// gets a clear 400 with the bad name rather than silent fallback.
	for propName, strategy := range req.Properties {
		if !validMaskingStrategies[strategy] {
			s.respondError(w, http.StatusBadRequest,
				"Invalid masking strategy for property "+propName+": "+string(strategy))
			return
		}
	}

	policy := &masking.Policy{
		Properties: req.Properties,
		AutoDetect: req.AutoDetect,
	}
	s.maskingPolicyStore.Set(tenantID, policy)

	// Audit trail: who changed which tenant's masking policy. Read
	// back the stored policy to surface the server-stamped UpdatedAt
	// in the response body. The Get cannot return ErrPolicyNotFound
	// because we just Set it on the line above; ignore the error.
	stored, _ := s.maskingPolicyStore.Get(tenantID) //nolint:errcheck // Set above guarantees presence
	if stored == nil {
		stored = &masking.Policy{TenantID: tenantID}
	}
	s.logAuditEvent(&audit.Event{
		TenantID:     tenantID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionUpdate,
		ResourceType: audit.ResourceCompliance,
		ResourceID:   "masking-policy/" + tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"property_count": len(req.Properties),
			"auto_detect":    req.AutoDetect,
		},
	})

	s.respondJSON(w, http.StatusOK, stored)
}

// handleComplianceMaskingPolicyGet serves GET
// /v1/compliance/masking-policy/{tenant}. Returns the named tenant's
// policy. Per design doc:
//
//   - Admin: any tenant.
//   - Non-admin: only own tenant (403 otherwise — strict, not 404, so
//     non-admins get a clear access-denied rather than masking the
//     existence of another tenant; existence of the *masking policy*
//     is not a side channel because tenants exist independently and
//     admins can already enumerate them).
//   - Missing policy: 404 with a distinct error so operators can
//     differentiate "no policy" from "policy exists but is empty."
func (s *Server) handleComplianceMaskingPolicyGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Path: /v1/compliance/masking-policy/{tenant}
	const prefix = "/v1/compliance/masking-policy/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		s.respondError(w, http.StatusNotFound, "Not found")
		return
	}
	pathTenant := strings.TrimPrefix(r.URL.Path, prefix)
	if pathTenant == "" || strings.Contains(pathTenant, "/") {
		s.respondError(w, http.StatusBadRequest, "Tenant ID required in path")
		return
	}

	claims, hasClaims := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !hasClaims {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	if claims.Role != auth.RoleAdmin {
		callerTenant := getTenantFromContext(r)
		if pathTenant != callerTenant {
			s.respondError(w, http.StatusForbidden,
				"Non-admin callers can only read their own tenant's policy")
			return
		}
	}

	policy, err := s.maskingPolicyStore.Get(pathTenant)
	if err != nil {
		if errors.Is(err, masking.ErrPolicyNotFound) {
			s.respondError(w, http.StatusNotFound, "No masking policy set for this tenant")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "get masking policy"))
		return
	}

	s.respondJSON(w, http.StatusOK, policy)
}

// handleComplianceMaskingPolicy dispatches POST vs GET on the
// /v1/compliance/masking-policy/* route family. GET handler reads
// {tenant} from path; POST handler doesn't (target is withTenant's
// resolved tenant).
func (s *Server) handleComplianceMaskingPolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleComplianceMaskingPolicySet(w, r)
	case http.MethodGet:
		s.handleComplianceMaskingPolicyGet(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
