package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/dd0wney/graphdb/pkg/audit"
	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TenantCreateRequest represents a request to create a tenant
type TenantCreateRequest struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Quota       *tenant.TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
}

// TenantUpdateRequest represents a request to update a tenant
type TenantUpdateRequest struct {
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Quota       *tenant.TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
}

// TenantResponse represents a tenant in API responses
type TenantResponse struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Status      tenant.TenantStatus `json:"status"`
	Quota       *tenant.TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string   `json:"metadata,omitempty"`
	CreatedAt   int64               `json:"created_at"`
	UpdatedAt   int64               `json:"updated_at"`
}

// TenantListResponse represents the response for listing tenants
type TenantListResponse struct {
	Tenants []TenantResponse `json:"tenants"`
	Count   int              `json:"count"`
}

// TenantUsageResponse represents tenant usage statistics
type TenantUsageResponse struct {
	TenantID     string             `json:"tenant_id"`
	NodeCount    int64              `json:"node_count"`
	EdgeCount    int64              `json:"edge_count"`
	StorageBytes int64              `json:"storage_bytes"`
	QuotaUsage   *tenant.QuotaUsage `json:"quota_usage,omitempty"`
	LastUpdated  int64              `json:"last_updated"`
}

// handleCreateTenant handles POST /tenants (admin only)
func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	var req TenantCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate required fields
	if req.ID == "" {
		s.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}
	if req.Name == "" {
		s.respondError(w, http.StatusBadRequest, "Tenant name is required")
		return
	}

	// Create tenant
	t := &tenant.Tenant{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Status:      tenant.TenantStatusActive,
		Quota:       req.Quota,
		Metadata:    req.Metadata,
	}

	if err := s.tenantStore.Create(t); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			s.respondError(w, http.StatusConflict, err.Error())
			return
		}
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Log audit event
	s.logAuditEvent(&audit.Event{
		TenantID:     t.ID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionCreate,
		ResourceType: "tenant",
		ResourceID:   t.ID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	// Create() populates CreatedAt/UpdatedAt on the input pointer (see
	// pkg/tenant/store.go), so t already has the timestamps for the response.
	s.respondJSON(w, http.StatusCreated, tenantToResponse(t))
}

// handleListTenants handles GET /tenants (admin only)
func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	tenants := s.tenantStore.List()

	response := TenantListResponse{
		Tenants: make([]TenantResponse, 0, len(tenants)),
		Count:   len(tenants),
	}

	for _, t := range tenants {
		response.Tenants = append(response.Tenants, tenantToResponse(t))
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleGetTenant handles GET /tenants/{id}
func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	tenantID := extractPathParam(r.URL.Path, "/api/v1/tenants/")
	if tenantID == "" {
		s.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	// Remove any trailing path segments (for /usage endpoint)
	if idx := strings.Index(tenantID, "/"); idx != -1 {
		tenantID = tenantID[:idx]
	}

	claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	// Non-admins can only view their own tenant
	if claims.Role != auth.RoleAdmin {
		currentTenant := getTenantFromContext(r)
		if tenantID != currentTenant {
			s.respondError(w, http.StatusForbidden, "Cannot access other tenants")
			return
		}
	}

	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}

	s.respondJSON(w, http.StatusOK, tenantToResponse(t))
}

// handleUpdateTenant handles PUT /tenants/{id} (admin only)
func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	tenantID := extractPathParam(r.URL.Path, "/api/v1/tenants/")
	if tenantID == "" {
		s.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	var req TenantUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get existing tenant
	existing, err := s.tenantStore.Get(tenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}

	// Update fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Quota != nil {
		existing.Quota = req.Quota
	}
	if req.Metadata != nil {
		existing.Metadata = req.Metadata
	}

	if err := s.tenantStore.Update(existing); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Log audit event
	s.logAuditEvent(&audit.Event{
		TenantID:     tenantID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionUpdate,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	// Update() refreshes UpdatedAt on the same pointer Get returned (see
	// pkg/tenant/store.go), so existing already reflects the persisted state.
	s.respondJSON(w, http.StatusOK, tenantToResponse(existing))
}

// handleDeleteTenant handles DELETE /tenants/{id} (admin only)
func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	tenantID := extractPathParam(r.URL.Path, "/api/v1/tenants/")
	if tenantID == "" {
		s.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	// Guard BEFORE any cascade: the default tenant is undeletable, and a
	// missing tenant is a 404 — don't cascade graph data for a non-tenant.
	// (Cascade-before-record-delete is retry-safe: the record soft-delete is
	// last, so a mid-cascade failure leaves the tenant re-deletable.)
	if tenantID == tenant.DefaultTenantID {
		s.respondError(w, http.StatusForbidden, "cannot delete default tenant")
		return
	}
	if _, err := s.tenantStore.Get(tenantID); err != nil {
		s.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}

	// 1. Cascade the tenant's graph data — nodes, edges, per-tenant indexes,
	//    and vector-index definitions (#223). Without this the tenant's data
	//    stayed queryable under its ID after the record was deleted.
	//
	//    deleteTenantGraphData loops directly over
	//    DeleteNodeForTenant/DeleteEdgeForTenant rather than delegating to
	//    the storage-level DeleteTenant bulk helper (tenant_operations.go),
	//    which mirrors this same loop but ABORTS on the first error. A WAL
	//    append failure on one entity's delete must not stop the cascade
	//    partway through a tenant offboarding — the delete already applied
	//    in memory — so the loop remembers that (walErr) and continues.
	//    Any other error still aborts, matching DeleteTenant's own
	//    contract.
	nodesDeleted, edgesDeleted, walErr, err := s.deleteTenantGraphData(tenantID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete tenant data"))
		return
	}

	// 2. Drop the tenant's server-owned search indexes. LSA must be unlinked
	//    on disk too (else LoadAll resurrects it on restart); FTS is in-memory.
	//    A failure to remove the on-disk LSA snapshot is NOT swallowed
	//    (security audit M-2): that file holds the deleted tenant's full
	//    indexed content, so leaving it orphaned is a data-remanence /
	//    right-to-erasure gap. DeleteLSASnapshot already no-ops on a missing
	//    file, so a non-nil error is a real FS failure — fail the request so
	//    the operator retries. The delete is re-runnable: DeleteTenant
	//    tolerates already-removed data and lsaIndexes.Delete is idempotent.
	if s.lsaIndexes != nil {
		s.lsaIndexes.Delete(tenantID)
		if rmErr := search.DeleteLSASnapshot(filepath.Join(s.dataDir, "lsa"), tenantID); rmErr != nil {
			log.Printf("tenant delete %q: LSA snapshot cleanup failed: %v", tenantID, rmErr)
			s.respondError(w, http.StatusInternalServerError,
				"tenant graph data deleted, but the on-disk search snapshot could not be removed; retry the delete to complete erasure")
			return
		}
	}
	if s.searchIndexes != nil {
		s.searchIndexes.Delete(tenantID)
	}

	// 3. Purge the tenant's WAL records (security audit M-1 / DR-1): the
	//    cascade above appended OpDelete* entries, but the tenant's
	//    original OpCreate* entries — its full property data — stay in
	//    the WAL until the next snapshot+truncate, which on a long-
	//    running server is hours away. CompactWAL checkpoints (snapshot
	//    + TruncateUpTo the boundary) so erasure is immediate without
	//    losing concurrent writers' entries. Loud-fail like the LSA file
	//    above: the delete is re-runnable, so the operator retries.
	if err := s.graph.CompactWAL(); err != nil {
		log.Printf("tenant delete %q: WAL compaction failed: %v", tenantID, err)
		s.respondError(w, http.StatusInternalServerError,
			"tenant graph data deleted, but the write-ahead log could not be purged; retry the delete to complete erasure")
		return
	}

	// 4. Mark the tenant record deleted (authoritative soft-delete; idempotent).
	if err := s.tenantStore.Delete(tenantID); err != nil {
		// Already guarded for default/missing above, so this is unexpected.
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete tenant record"))
		return
	}

	// Log audit event
	s.logAuditEvent(&audit.Event{
		TenantID:     tenantID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionDelete,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	// walErr is non-nil when at least one node or edge delete's WAL append
	// failed during the cascade above (step 1) — the whole cascade still
	// ran to completion and every later step (search index cleanup, WAL
	// compaction, tenant record delete, audit log) still happened, but the
	// deletes it carries are not all durable yet.
	if walErr != nil {
		s.respondWALWriteFailed(w, walErr, 0)
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"message":       "Tenant deleted successfully",
		"id":            tenantID,
		"nodes_deleted": nodesDeleted,
		"edges_deleted": edgesDeleted,
	})
}

// deleteTenantGraphData mirrors storage.GraphStorage.DeleteTenant's cascade
// (node pass, defensive edge pass, vector-index cleanup) — see
// tenant_operations.go for the original — but continues past a WAL append
// failure on an individual delete instead of aborting there: the delete
// already applied in memory, so stopping would leave the rest of the
// tenant's data behind mid-offboarding. The first such failure is returned
// as walErr (non-nil means "cascade complete, not all of it durable yet");
// any other error still aborts immediately, matching DeleteTenant's own
// contract.
func (s *Server) deleteTenantGraphData(tenantID string) (nodesDeleted, edgesDeleted int, walErr, err error) {
	nodes, nodeEnumErr := s.graph.GetAllNodesForTenant(tenantID)
	for _, n := range nodes {
		if derr := s.graph.DeleteNodeForTenant(n.ID, tenantID); derr != nil {
			if errors.Is(derr, storage.ErrNodeNotFound) {
				continue // already removed via another node's edge cascade
			}
			if errors.Is(derr, storage.ErrWALWriteFailed) {
				if walErr == nil {
					walErr = derr
				}
				continue
			}
			return nodesDeleted, edgesDeleted, walErr, fmt.Errorf("delete node %d: %w", n.ID, derr)
		}
		nodesDeleted++
	}

	// Defensive sweep: any edges the node pass didn't cascade.
	edges, edgeEnumErr := s.graph.GetAllEdgesForTenant(tenantID)
	for _, e := range edges {
		if derr := s.graph.DeleteEdgeForTenant(e.ID, tenantID); derr != nil {
			if errors.Is(derr, storage.ErrEdgeNotFound) {
				continue
			}
			if errors.Is(derr, storage.ErrWALWriteFailed) {
				if walErr == nil {
					walErr = derr
				}
				continue
			}
			return nodesDeleted, edgesDeleted, walErr, fmt.Errorf("delete edge %d: %w", e.ID, derr)
		}
		edgesDeleted++
	}

	// Drop the tenant's vector-index definitions (WAL-durable). Best-effort:
	// a concurrently-dropped index just surfaces as an error we can ignore,
	// and a WAL append failure here does not change this tenant delete's
	// notDurable outcome — the node/edge passes above already decide that.
	for _, prop := range s.graph.ListVectorIndexesForTenant(tenantID) {
		s.graph.DropVectorIndexForTenant(tenantID, prop) //nolint:errcheck // best-effort cleanup, see comment above
	}

	if enumErr := errors.Join(nodeEnumErr, edgeEnumErr); enumErr != nil {
		return nodesDeleted, edgesDeleted, walErr,
			fmt.Errorf("delete tenant is incomplete, records survive that could not be read: %w", enumErr)
	}

	return nodesDeleted, edgesDeleted, walErr, nil
}

// handleGetTenantUsage handles GET /tenants/{id}/usage
func (s *Server) handleGetTenantUsage(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	// Extract tenant ID from path like /api/v1/tenants/{id}/usage
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/api/v1/tenants/")
	if !ok {
		return
	}
	if len(parts) < 2 || parts[1] != "usage" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	// Non-admins can only view their own tenant
	if claims.Role != auth.RoleAdmin {
		currentTenant := getTenantFromContext(r)
		if tenantID != currentTenant {
			s.respondError(w, http.StatusForbidden, "Cannot access other tenants")
			return
		}
	}

	// Get tenant to check it exists and get quota
	t, err := s.tenantStore.Get(tenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Tenant not found")
		return
	}

	// Get usage
	usage, err := s.tenantStore.GetUsage(tenantID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Usage data not found")
		return
	}

	response := TenantUsageResponse{
		TenantID:     tenantID,
		NodeCount:    usage.NodeCount,
		EdgeCount:    usage.EdgeCount,
		StorageBytes: usage.StorageBytes,
		LastUpdated:  usage.LastUpdated,
	}

	if t.Quota != nil {
		response.QuotaUsage = tenant.NewQuotaUsage(t.Quota, usage)
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleSuspendTenant handles POST /tenants/{id}/suspend (admin only)
func (s *Server) handleSuspendTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	// Extract tenant ID
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/api/v1/tenants/")
	if !ok {
		return
	}
	if len(parts) < 2 || parts[1] != "suspend" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	if err := s.tenantStore.Suspend(tenantID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.respondError(w, http.StatusNotFound, "Tenant not found")
			return
		}
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Log audit event
	s.logAuditEvent(&audit.Event{
		TenantID:     tenantID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionUpdate,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
		Metadata:     map[string]any{"action": "suspend"},
	})

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message": "Tenant suspended",
		"id":      tenantID,
	})
}

// handleActivateTenant handles POST /tenants/{id}/activate (admin only)
func (s *Server) handleActivateTenant(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	// Extract tenant ID
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/api/v1/tenants/")
	if !ok {
		return
	}
	if len(parts) < 2 || parts[1] != "activate" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	if err := s.tenantStore.Activate(tenantID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.respondError(w, http.StatusNotFound, "Tenant not found")
			return
		}
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Log audit event
	s.logAuditEvent(&audit.Event{
		TenantID:     tenantID,
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionUpdate,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
		Metadata:     map[string]any{"action": "activate"},
	})

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message": "Tenant activated",
		"id":      tenantID,
	})
}

// tenantToResponse converts a tenant to API response format
func tenantToResponse(t *tenant.Tenant) TenantResponse {
	return TenantResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Status:      t.Status,
		Quota:       t.Quota,
		Metadata:    t.Metadata,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// extractPathParam extracts a path parameter after a prefix
func extractPathParam(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	return strings.TrimPrefix(path, prefix)
}

// handleTenantsEndpoint routes /api/v1/tenants based on HTTP method
func (s *Server) handleTenantsEndpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleTenantEndpoint routes /api/v1/tenants/{id}[/action] based on HTTP method and path
func (s *Server) handleTenantEndpoint(w http.ResponseWriter, r *http.Request) {
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/api/v1/tenants/")
	if !ok {
		return
	}
	if parts[0] == "" {
		// Defensive: catches double-slash URLs like /api/v1/tenants//abc
		// where ExtractParts returns ["", "abc"] but the tenant ID is empty.
		s.respondError(w, http.StatusBadRequest, "Tenant ID is required")
		return
	}

	// Check for sub-resource actions
	if len(parts) >= 2 {
		switch parts[1] {
		case "usage":
			if r.Method == http.MethodGet {
				s.handleGetTenantUsage(w, r)
				return
			}
		case "suspend":
			if r.Method == http.MethodPost {
				if s.requireAdminClaims(w, r) == nil {
					return
				}
				s.handleSuspendTenant(w, r)
				return
			}
		case "activate":
			if r.Method == http.MethodPost {
				if s.requireAdminClaims(w, r) == nil {
					return
				}
				s.handleActivateTenant(w, r)
				return
			}
		}
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Handle base tenant resource
	switch r.Method {
	case http.MethodGet:
		s.handleGetTenant(w, r)
	case http.MethodPut:
		if s.requireAdminClaims(w, r) == nil {
			return
		}
		s.handleUpdateTenant(w, r)
	case http.MethodDelete:
		if s.requireAdminClaims(w, r) == nil {
			return
		}
		s.handleDeleteTenant(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
