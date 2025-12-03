package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ContextKey is a type for context keys to avoid collisions
type ContextKey string

// RequestIDContextKey is the context key for storing request IDs
const RequestIDContextKey ContextKey = "request_id"

// RequestIDHeader is the header name for request IDs
const RequestIDHeader = "X-Request-ID"

// generateRequestID creates a unique request ID
// Uses timestamp + random suffix for uniqueness without external dependencies
func generateRequestID() string {
	// Format: timestamp_randomhex (e.g., 1699876543_a1b2c3d4)
	timestamp := time.Now().UnixNano()
	// Use simple counter + timestamp for uniqueness
	return fmt.Sprintf("%d_%08x", timestamp/1000000, timestamp%0xFFFFFFFF)
}

// GetRequestID extracts request ID from request context
func GetRequestID(r *http.Request) string {
	if id, ok := r.Context().Value(RequestIDContextKey).(string); ok {
		return id
	}
	return ""
}

// sanitizeRequestID removes potentially dangerous characters from request IDs
func sanitizeRequestID(id string) string {
	var result strings.Builder
	result.Grow(len(id))

	for _, c := range id {
		// Allow alphanumeric, dash, underscore, and dot
		if (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' {
			result.WriteRune(c)
		}
	}

	return result.String()
}

// RequestID creates middleware that adds a unique request ID to each request.
// If the client provides X-Request-ID header, it will be used (after sanitization).
// Otherwise, a new ID is generated.
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check for existing request ID from client
			requestID := r.Header.Get(RequestIDHeader)

			// Validate and sanitize incoming request ID
			if requestID != "" {
				// Limit length to prevent abuse
				if len(requestID) > 64 {
					requestID = requestID[:64]
				}
				// Remove any potentially problematic characters
				requestID = sanitizeRequestID(requestID)
			}

			// Generate new ID if none provided or invalid
			if requestID == "" {
				requestID = generateRequestID()
			}

			// Add to response header for client correlation
			w.Header().Set(RequestIDHeader, requestID)

			// Add to context for downstream handlers
			ctx := context.WithValue(r.Context(), RequestIDContextKey, requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
