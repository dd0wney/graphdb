package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
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
