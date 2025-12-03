package middleware

import (
	"net/http"
)

// BodySizeLimit creates middleware that limits the size of incoming request bodies
// to prevent denial-of-service attacks via large payloads.
// The maxBytes parameter specifies the maximum allowed size in bytes.
func BodySizeLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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
}
