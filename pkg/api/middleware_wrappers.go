package api

import (
	"net/http"
	"strings"

	"github.com/dd0wney/graphdb/pkg/api/middleware"
)

// Request body caps (security audit M-4). The general cap matches the
// input-validation middleware's existing 10 MB limit; the auth cap is
// tight because /auth/* payloads are small JSON — and those paths are
// the ones inputValidationMiddleware skips, so without this outer layer
// they had no body bound at all (an unbounded pre-auth read).
const (
	maxAuthBodyBytes    = 64 * 1024        // 64 KiB
	maxGeneralBodyBytes = 10 * 1024 * 1024 // 10 MiB
)

// bodyLimitMiddleware caps the request body size for EVERY request,
// including the pre-auth /auth/* paths that inputValidationMiddleware
// skips (security audit M-4). It runs ahead of input validation so an
// oversized body is rejected before anything reads it.
func (s *Server) bodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxGeneralBodyBytes)
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			limit = maxAuthBodyBytes
		}
		if r.ContentLength > limit {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		// Safety net for chunked/absent Content-Length.
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

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
