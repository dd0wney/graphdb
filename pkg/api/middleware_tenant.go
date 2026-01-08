package api

import (
	"log"
	"net/http"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

const (
	// TenantIDHeader is the header admins can use to override tenant context
	TenantIDHeader = "X-Tenant-ID"
)

// withTenant is middleware that extracts tenant context from claims or headers.
// Tenant is determined in the following priority:
// 1. X-Tenant-ID header (admin only)
// 2. tenant_id claim from JWT
// 3. Default tenant ("default")
//
// This middleware must be applied AFTER requireAuth since it relies on claims being in context.
func (s *Server) withTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
		if !ok {
			// No claims in context - should not happen if requireAuth ran first
			s.respondError(w, http.StatusUnauthorized, "Authentication required for tenant context")
			return
		}

		// Determine tenant ID
		tenantID := s.resolveTenantID(r, claims)

		// Validate tenant exists (if tenant store is configured)
		if s.tenantStore != nil {
			_, err := s.tenantStore.Get(tenantID)
			if err != nil {
				// For default tenant, auto-create if it doesn't exist
				if tenantID == tenant.DefaultTenantID {
					defaultTenant := &tenant.Tenant{
						ID:     tenant.DefaultTenantID,
						Name:   "Default Tenant",
						Status: tenant.TenantStatusActive,
					}
					if createErr := s.tenantStore.Create(defaultTenant); createErr != nil {
						log.Printf("Failed to auto-create default tenant: %v", createErr)
					}
				} else {
					s.respondError(w, http.StatusForbidden, "Tenant not found or inactive")
					return
				}
			}
		}

		// Inject tenant into context
		ctx := tenant.WithTenant(r.Context(), tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// resolveTenantID determines the tenant ID based on headers and claims.
// Priority: X-Tenant-ID header (admin only) > JWT claim > default
func (s *Server) resolveTenantID(r *http.Request, claims *auth.Claims) string {
	// Check for admin override via header
	headerTenantID := r.Header.Get(TenantIDHeader)
	if headerTenantID != "" {
		// Only admins can override tenant
		if claims.Role == auth.RoleAdmin {
			log.Printf("Admin %s overriding tenant to: %s", claims.Username, headerTenantID)
			return headerTenantID
		}
		// Non-admin trying to override - log and ignore
		log.Printf("Non-admin user %s attempted tenant override to %s (ignored)", claims.Username, headerTenantID)
	}

	// Use tenant from JWT claims if present
	if claims.TenantID != "" {
		return claims.TenantID
	}

	// Default tenant for backward compatibility
	return tenant.DefaultTenantID
}

// getTenantFromContext extracts the tenant ID from request context.
// Returns the default tenant if not found.
// This is a convenience helper for handlers.
func getTenantFromContext(r *http.Request) string {
	tenantID, ok := tenant.FromContext(r.Context())
	if !ok {
		return tenant.DefaultTenantID
	}
	return tenantID
}

// requireTenant middleware ensures a valid tenant is in context.
// This is a stricter version that fails if no tenant is set.
func (s *Server) requireTenant(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, ok := tenant.FromContext(r.Context())
		if !ok || tenantID == "" {
			s.respondError(w, http.StatusBadRequest, "Tenant context required")
			return
		}

		// Verify tenant is active if store is configured
		if s.tenantStore != nil {
			t, err := s.tenantStore.Get(tenantID)
			if err != nil {
				s.respondError(w, http.StatusForbidden, "Tenant not found")
				return
			}
			if t.Status != tenant.TenantStatusActive {
				s.respondError(w, http.StatusForbidden, "Tenant is suspended or deleted")
				return
			}
		}

		next.ServeHTTP(w, r)
	}
}
