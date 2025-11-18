package api

import (
	"log"
	"net/http"
	"time"
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

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
