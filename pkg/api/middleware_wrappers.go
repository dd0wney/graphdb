package api

import (
	"net/http"

	"github.com/dd0wney/cluso-graphdb/pkg/api/middleware"
)

// panicRecoveryMiddleware recovers from panics in HTTP handlers
func (s *Server) panicRecoveryMiddleware(next http.Handler) http.Handler {
	return middleware.PanicRecovery()(next)
}

// loggingMiddleware logs HTTP requests with timing information
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return middleware.Logging(middleware.GetRequestID)(next)
}

// corsMiddleware handles Cross-Origin Resource Sharing
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return middleware.CORS(s.corsConfig)(next)
}

// bodySizeLimitMiddleware limits the size of incoming request bodies
func (s *Server) bodySizeLimitMiddleware(next http.Handler, maxBytes int64) http.Handler {
	return middleware.BodySizeLimit(maxBytes)(next)
}

// requestIDMiddleware adds a unique request ID to each request
func (s *Server) requestIDMiddleware(next http.Handler) http.Handler {
	return middleware.RequestID()(next)
}

// securityHeadersMiddleware adds security headers to responses
func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	config := &middleware.SecurityHeadersConfig{
		TLSEnabled: s.tlsConfig != nil && s.tlsConfig.Enabled,
	}
	return middleware.SecurityHeaders(config)(next)
}

// inputValidationMiddleware validates input for security issues
func (s *Server) inputValidationMiddleware(next http.Handler) http.Handler {
	return middleware.InputValidation(nil)(next)
}
