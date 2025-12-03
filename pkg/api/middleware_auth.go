package api

import (
	"context"
	"log"
	"net/http"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
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

// requireAuth middleware validates JWT tokens or API keys and protects endpoints
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try JWT token first (Authorization: Bearer <token>)
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token := authHeader[7:]

			// Validate token
			claims, err := s.jwtManager.ValidateToken(token)
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

			// Store claims in context for handlers to access
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
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

			// Update last used timestamp
			s.apiKeyStore.UpdateLastUsed(key.ID)

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

			// Store claims in context for handlers to access
			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// No valid authentication provided
		s.respondError(w, http.StatusUnauthorized, "Missing authentication (Bearer token or X-API-Key header required)")
	}
}
