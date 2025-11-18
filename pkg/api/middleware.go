package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// bodySizeLimitMiddleware limits the size of incoming request bodies
// to prevent denial-of-service attacks via large payloads.
// The maxBytes parameter specifies the maximum allowed size in bytes.
func (s *Server) bodySizeLimitMiddleware(next http.Handler, maxBytes int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Content-Length header if present
		// This allows us to reject large requests before reading the body
		if r.ContentLength > maxBytes {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		// Also set MaxBytesReader as a safety net in case Content-Length is not set
		// or is incorrect (this handles chunked transfer encoding)
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

		next.ServeHTTP(w, r)
	})
}

// Context key for storing claims
type contextKey string

const claimsContextKey contextKey = "claims"

// requireAuth middleware validates JWT tokens or API keys and protects endpoints
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try JWT token first (Authorization: Bearer <token>)
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			// Extract token (format: "Bearer <token>")
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 {
				token := parts[1]

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
		}

		// Try API key (X-API-Key: <key>)
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			// Validate API key
			key, err := s.apiKeyStore.ValidateKey(apiKey)
			if err != nil {
				log.Printf("API key validation failed: %v", err)
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
