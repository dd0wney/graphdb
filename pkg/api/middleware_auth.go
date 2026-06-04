package api

import (
	"context"
	"log"
	"net/http"

	"github.com/dd0wney/graphdb/pkg/audit"
	"github.com/dd0wney/graphdb/pkg/auth"
)

// Context key for storing claims
type contextKey string

const claimsContextKey contextKey = "claims"

// requireAdmin middleware validates JWT tokens or API keys and requires admin role
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
		if !ok {
			s.respondError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		if claims.Role != auth.RoleAdmin {
			// Log unauthorized admin access attempt
			s.logAuditEvent(&audit.Event{
				UserID:       claims.UserID,
				Username:     claims.Username,
				Action:       audit.ActionAuth,
				ResourceType: audit.ResourceAuth,
				Status:       audit.StatusFailure,
				IPAddress:    getIPAddress(r),
				UserAgent:    r.UserAgent(),
				Metadata: map[string]any{
					"error":  "admin access required",
					"path":   r.URL.Path,
					"method": r.Method,
				},
			})
			s.respondError(w, http.StatusForbidden, "Admin access required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// requireAdminClaims pulls admin-validated claims from the request context.
// Writes 401 (no claims in context) or 403 (claims present but role != admin)
// directly to w on failure and returns nil; caller pattern is:
//
//	claims := s.requireAdminClaims(w, r)
//	if claims == nil {
//	    return
//	}
//
// Use after a route is wrapped in s.requireAdmin (the typical case — the
// middleware writes claims into context and enforces the role at the
// dispatch layer; this helper pulls them out and re-asserts as
// defense-in-depth at the handler boundary so a future routing change
// that drops the middleware can't silently turn an admin endpoint into
// an unauthenticated one).
func (s *Server) requireAdminClaims(w http.ResponseWriter, r *http.Request) *auth.Claims {
	claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return nil
	}
	if claims.Role != auth.RoleAdmin {
		s.respondError(w, http.StatusForbidden, "Admin access required")
		return nil
	}
	return claims
}

// requireAuth middleware validates JWT tokens or API keys and protects endpoints
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try JWT token first (Authorization: Bearer <token>)
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token := authHeader[7:]

			// Validate token using composite validator (supports JWT and OIDC tokens)
			claims, err := s.tokenValidator.ValidateToken(r.Context(), token)
			if err != nil {
				log.Printf("Token validation failed: %v", err)
				s.respondError(w, http.StatusUnauthorized, "Invalid or expired token")
				return
			}

			// Verify user still exists
			_, err = s.userStore.GetUserByID(claims.UserID)
			if err != nil {
				s.respondError(w, http.StatusUnauthorized, "User not found")
				return
			}

			// Store claims in context for handlers to access. Also write the
			// identity into the audit collector (if installed) so the outer
			// auditMiddleware can include it in emitted events — see
			// middleware_audit_collector.go for why a separate collector is
			// needed instead of context lookups.
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			setAuditUser(ctx, claims.UserID, claims.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Try API key (X-API-Key: <key>)
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			// Validate API key with environment enforcement
			key, err := s.apiKeyStore.ValidateKeyForEnv(apiKey, s.environment)
			if err != nil {
				log.Printf("API key validation failed: %v", err)
				if err == auth.ErrAPIKeyWrongEnv {
					s.respondError(w, http.StatusForbidden, "API key environment mismatch (test key on production or vice versa)")
					return
				}
				s.respondError(w, http.StatusUnauthorized, "Invalid or expired API key")
				return
			}

			// Update last used timestamp — best-effort; auth proceeds
			// even if the timestamp update fails, but we log so operators
			// can diagnose persistent-store issues.
			if err := s.apiKeyStore.UpdateLastUsed(key.ID); err != nil {
				log.Printf("API key UpdateLastUsed failed for key %s: %v", key.ID, err)
			}

			// Get user for the API key
			user, err := s.userStore.GetUserByID(key.UserID)
			if err != nil {
				s.respondError(w, http.StatusUnauthorized, "User not found")
				return
			}

			// Create pseudo-claims from API key for consistent context
			claims := &auth.Claims{
				UserID:   user.ID,
				Username: user.Username,
				Role:     user.Role,
			}

			// Store claims in context for handlers to access. Audit collector
			// write mirrors the JWT path above.
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			setAuditUser(ctx, claims.UserID, claims.Username)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// No valid authentication provided
		s.respondError(w, http.StatusUnauthorized, "Missing authentication (Bearer token or X-API-Key header required)")
	}
}
