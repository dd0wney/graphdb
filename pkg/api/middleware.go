package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/security"
)

// panicRecoveryMiddleware recovers from panics in HTTP handlers
// This prevents server crashes and returns a proper error response
func (s *Server) panicRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with stack trace
				stack := debug.Stack()
				log.Printf("PANIC in HTTP handler [%s %s]: %v\n%s",
					r.Method, r.URL.Path, err, stack)

				// Return Internal Server Error
				http.Error(w,
					fmt.Sprintf("Internal server error: %v", err),
					http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

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

// metricsMiddleware tracks HTTP request metrics
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track in-flight requests
		s.metricsRegistry.HTTPRequestsInFlight.Inc()
		defer s.metricsRegistry.HTTPRequestsInFlight.Dec()

		// Create a response writer wrapper to capture status code and size
		wrapper := &metricsResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
			bytesWritten:   0,
		}

		// Process request
		next.ServeHTTP(wrapper, r)

		// Record metrics
		duration := time.Since(start)
		statusStr := strconv.Itoa(wrapper.statusCode)

		s.metricsRegistry.RecordHTTPRequest(r.Method, r.URL.Path, statusStr, duration)
		s.metricsRegistry.HTTPResponseSizeBytes.WithLabelValues(r.Method, r.URL.Path).Observe(float64(wrapper.bytesWritten))
	})
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code and bytes written
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// updateMetricsPeriodically updates system metrics every 10 seconds
func (s *Server) updateMetricsPeriodically() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update uptime
		s.metricsRegistry.UptimeSeconds.Set(time.Since(s.startTime).Seconds())

		// Update Go runtime metrics
		s.metricsRegistry.GoRoutines.Set(float64(runtime.NumGoroutine()))

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		s.metricsRegistry.MemoryAllocBytes.Set(float64(m.Alloc))
		s.metricsRegistry.MemorySysBytes.Set(float64(m.Sys))

		// Update storage metrics
		stats := s.graph.GetStatistics()
		s.metricsRegistry.StorageNodesTotal.Set(float64(stats.NodeCount))
		s.metricsRegistry.StorageEdgesTotal.Set(float64(stats.EdgeCount))
	}
}

// inputValidationMiddleware validates input for security issues
// This protects against injection attacks, XSS, path traversal, etc.
func (s *Server) inputValidationMiddleware(next http.Handler) http.Handler {
	validator := security.NewInputValidator()

	// Paths that should skip strict validation
	skipPaths := []string{
		"/auth/login",
		"/auth/register",
		"/auth/refresh",
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip validation for certain paths
		for _, path := range skipPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Only validate POST/PUT/PATCH requests with bodies
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
			next.ServeHTTP(w, r)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Convert to string for validation
		bodyStr := string(body)

		// Skip validation for empty bodies
		if len(bodyStr) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Validate for path traversal (most dangerous)
		if err := validator.ValidateNoPathTraversal(bodyStr); err != nil {
			log.Printf("Path traversal attempt detected: %v", err)
			http.Error(w, "Invalid input: potential security threat detected", http.StatusBadRequest)
			return
		}

		// Validate maximum length (10MB)
		if err := validator.ValidateString(bodyStr, 10*1024*1024); err != nil {
			log.Printf("Input validation failed: %v", err)
			http.Error(w, "Invalid input: request too large", http.StatusBadRequest)
			return
		}

		// Restore body for next handler
		r.Body = io.NopCloser(strings.NewReader(bodyStr))

		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware adds security headers to responses
func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Enable XSS protection
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Enforce HTTPS (if TLS is enabled)
		if s.tlsConfig != nil && s.tlsConfig.Enabled {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Content Security Policy
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'")

		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Permissions policy
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}
