package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/api/middleware"
	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// logAuditEvent logs an event to both audit loggers when persistent logging is enabled.
// This ensures security-critical events are both persisted and available via API.
func (s *Server) logAuditEvent(event *audit.Event) {
	// Log to the main audit logger (persistent if enabled)
	if err := s.auditLogger.Log(event); err != nil {
		log.Printf("Failed to write audit log: %v", err)
	}

	// Also log to in-memory logger if persistent logging is enabled
	// This ensures the API can still query recent events
	if s.persistentAudit != nil {
		s.inMemoryAuditLogger.Log(event)
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

		// Get request ID for correlation
		requestID := middleware.GetRequestID(r)

		// Log the audit event
		event := &audit.Event{
			UserID:       userID,
			Username:     username,
			Action:       action,
			ResourceType: resourceType,
			Status:       status,
			IPAddress:    getIPAddress(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status_code": wrapper.statusCode,
				"duration_ms": time.Since(start).Milliseconds(),
				"request_id":  requestID,
			},
		}

		// Log to both audit loggers (persistent + in-memory for API queries)
		s.logAuditEvent(event)
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

// determineResourceAndAction determines resource type and action from HTTP request
func determineResourceAndAction(method, path string) (audit.ResourceType, audit.Action) {
	// Determine resource type from path
	resourceType := audit.ResourceQuery // default

	switch {
	case strings.Contains(path, "/nodes"):
		resourceType = audit.ResourceNode
	case strings.Contains(path, "/edges"):
		resourceType = audit.ResourceEdge
	case strings.Contains(path, "/auth"):
		resourceType = audit.ResourceAuth
	case strings.Contains(path, "/query") || strings.Contains(path, "/graphql"):
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

// getIPAddress extracts the client IP address from a request.
// Delegates to middleware.GetClientIP which handles trusted proxy validation.
func getIPAddress(r *http.Request) string {
	return middleware.GetClientIP(r)
}
