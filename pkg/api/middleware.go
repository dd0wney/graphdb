package api

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
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

// auditMiddleware logs all API requests for security and compliance
func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapper := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Process request
		next.ServeHTTP(wrapper, r)

		// Extract user info from context if authenticated
		userID := ""
		username := ""
		if claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims); ok {
			userID = claims.UserID
			username = claims.Username
		}

		// Determine resource type and action from path and method
		resourceType, action := determineResourceAndAction(r.Method, r.URL.Path)

		// Determine status
		status := audit.StatusSuccess
		if wrapper.statusCode >= 400 {
			status = audit.StatusFailure
		}

		// Log the audit event
		event := &audit.Event{
			UserID:       userID,
			Username:     username,
			Action:       action,
			ResourceType: resourceType,
			Status:       status,
			IPAddress:    getIPAddress(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]interface{}{
				"method":       r.Method,
				"path":         r.URL.Path,
				"status_code":  wrapper.statusCode,
				"duration_ms":  time.Since(start).Milliseconds(),
			},
		}

		s.auditLogger.Log(event)
	})
}

// statusResponseWriter wraps http.ResponseWriter to capture status code
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Helper functions

func determineResourceAndAction(method, path string) (audit.ResourceType, audit.Action) {
	// Determine resource type from path
	resourceType := audit.ResourceQuery // default

	if strings.Contains(path, "/nodes") {
		resourceType = audit.ResourceNode
	} else if strings.Contains(path, "/edges") {
		resourceType = audit.ResourceEdge
	} else if strings.Contains(path, "/auth") {
		resourceType = audit.ResourceAuth
	} else if strings.Contains(path, "/query") || strings.Contains(path, "/graphql") {
		resourceType = audit.ResourceQuery
	}

	// Determine action from HTTP method
	action := audit.ActionRead // default

	switch method {
	case http.MethodPost:
		if resourceType == audit.ResourceAuth {
			action = audit.ActionAuth
		} else {
			action = audit.ActionCreate
		}
	case http.MethodGet:
		action = audit.ActionRead
	case http.MethodPut, http.MethodPatch:
		action = audit.ActionUpdate
	case http.MethodDelete:
		action = audit.ActionDelete
	}

	return resourceType, action
}

func getIPAddress(r *http.Request) string {
	// Try to get real IP from headers (for proxies)
	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		ip = r.Header.Get("X-Forwarded-For")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	return ip
}
