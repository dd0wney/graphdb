package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/tenant"
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

		// Determine tenant ID. A malformed admin override (M-5) is a
		// client error, distinct from a well-formed-but-unknown tenant.
		tenantID, err := s.resolveTenantID(r, claims)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, "Invalid tenant identifier")
			return
		}

		// Validate tenant exists AND is active (if tenant store is
		// configured). GetActive — not Get — so a suspended or deleted
		// tenant is rejected here rather than retaining access until its
		// JWT expires (security audit H-1).
		if s.tenantStore != nil {
			_, err := s.tenantStore.GetActive(tenantID)
			if err != nil {
				// The default tenant is auto-created on first use, but
				// only when it is genuinely absent — never resurrect a
				// suspended/deleted default tenant by recreating it.
				if tenantID == tenant.DefaultTenantID && errors.Is(err, tenant.ErrTenantNotFound) {
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

		// Inject tenant into context. Also write into the audit collector
		// (if installed) so auditMiddleware can include TenantID in emitted
		// events — see middleware_audit_collector.go for the rationale.
		ctx := tenant.WithTenant(r.Context(), tenantID)
		setAuditTenant(ctx, tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// resolveTenantID determines the tenant ID based on headers and claims.
// Priority: X-Tenant-ID header (admin only) > JWT claim > default.
//
// A non-nil error means an admin supplied a malformed X-Tenant-ID header
// (security audit M-5); the caller maps it to 400. The raw header value
// is never logged before validation passes, so a value containing CR/LF
// cannot forge log entries (the M-5 log-injection vector).
func (s *Server) resolveTenantID(r *http.Request, claims *auth.Claims) (string, error) {
	// Check for admin override via header
	headerTenantID := r.Header.Get(TenantIDHeader)
	if headerTenantID != "" {
		// Only admins can override tenant
		if claims.Role == auth.RoleAdmin {
			if err := tenant.ValidateTenantID(headerTenantID); err != nil {
				return "", err
			}
			// Safe to log: a validated ID has no control characters.
			log.Printf("Admin %s overriding tenant to: %s", claims.Username, headerTenantID)
			return headerTenantID, nil
		}
		// Non-admin trying to override — ignore. Do NOT log the raw
		// attacker-controlled value (M-5); record only that it happened.
		log.Printf("Non-admin user %s attempted tenant override (ignored)", claims.Username)
	}

	// Use tenant from JWT claims if present
	if claims.TenantID != "" {
		return claims.TenantID, nil
	}

	// Default tenant for backward compatibility
	return tenant.DefaultTenantID, nil
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
// This is a stricter version that fails if no tenant is set. Kept as
// part of the middleware API surface (symmetric with withTenant) for
// routes that need hard failure on missing tenant context; not yet
// applied to any route.
//
//nolint:unused // middleware API surface reserved for strict-tenant routes
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
