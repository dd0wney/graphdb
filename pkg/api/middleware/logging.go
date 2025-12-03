package middleware

import (
	"log"
	"net/http"
	"time"
)

// Logging creates middleware that logs HTTP requests with timing information.
// It uses the request ID from context if available.
func Logging(getRequestID func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)

			// Include request ID in logs for tracing
			requestID := ""
			if getRequestID != nil {
				requestID = getRequestID(r)
			}

			if requestID != "" {
				log.Printf("[%s] %s %s %v", requestID, r.Method, r.URL.Path, time.Since(start))
			} else {
				log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
			}
		})
	}
}
