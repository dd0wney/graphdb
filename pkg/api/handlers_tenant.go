package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// TenantCreateRequest represents a request to create a tenant
type TenantCreateRequest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Quota       *tenant.TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TenantUpdateRequest represents a request to update a tenant
type TenantUpdateRequest struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Quota       *tenant.TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TenantResponse represents a tenant in API responses
type TenantResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Status      tenant.TenantStatus  `json:"status"`
	Quota       *tenant.TenantQuota  `json:"quota,omitempty"`
	Metadata    map[string]string    `json:"metadata,omitempty"`
	CreatedAt   int64                `json:"created_at"`
	UpdatedAt   int64                `json:"updated_at"`
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

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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

	// Get the created tenant to return with timestamps
	created, _ := s.tenantStore.Get(t.ID)
	s.respondJSON(w, http.StatusCreated, tenantToResponse(created))
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

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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

	// Get updated tenant
	updated, _ := s.tenantStore.Get(tenantID)
	s.respondJSON(w, http.StatusOK, tenantToResponse(updated))
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

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

	if err := s.tenantStore.Delete(tenantID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.respondError(w, http.StatusNotFound, "Tenant not found")
			return
		}
		if strings.Contains(err.Error(), "cannot delete") {
			s.respondError(w, http.StatusForbidden, err.Error())
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
		Action:       audit.ActionDelete,
		ResourceType: "tenant",
		ResourceID:   tenantID,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	s.respondJSON(w, http.StatusOK, map[string]string{
		"message": "Tenant deleted successfully",
		"id":      tenantID,
	})
}

// handleGetTenantUsage handles GET /tenants/{id}/usage
func (s *Server) handleGetTenantUsage(w http.ResponseWriter, r *http.Request) {
	if s.tenantStore == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Multi-tenancy is not enabled")
		return
	}

	// Extract tenant ID from path like /api/v1/tenants/{id}/usage
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "usage" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "suspend" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "activate" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return
	}
	tenantID := parts[0]

	claims := r.Context().Value(claimsContextKey).(*auth.Claims)

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
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
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
				// Admin only check
				claims := r.Context().Value(claimsContextKey).(*auth.Claims)
				if claims.Role != auth.RoleAdmin {
					s.respondError(w, http.StatusForbidden, "Admin access required")
					return
				}
				s.handleSuspendTenant(w, r)
				return
			}
		case "activate":
			if r.Method == http.MethodPost {
				// Admin only check
				claims := r.Context().Value(claimsContextKey).(*auth.Claims)
				if claims.Role != auth.RoleAdmin {
					s.respondError(w, http.StatusForbidden, "Admin access required")
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
		// Admin only check
		claims := r.Context().Value(claimsContextKey).(*auth.Claims)
		if claims.Role != auth.RoleAdmin {
			s.respondError(w, http.StatusForbidden, "Admin access required")
			return
		}
		s.handleUpdateTenant(w, r)
	case http.MethodDelete:
		// Admin only check
		claims := r.Context().Value(claimsContextKey).(*auth.Claims)
		if claims.Role != auth.RoleAdmin {
			s.respondError(w, http.StatusForbidden, "Admin access required")
			return
		}
		s.handleDeleteTenant(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
