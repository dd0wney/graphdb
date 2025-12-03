package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins   []string // List of allowed origins, or ["*"] for all (not recommended for production)
	AllowedMethods   []string // HTTP methods allowed
	AllowedHeaders   []string // Headers allowed in requests
	AllowCredentials bool     // Whether credentials (cookies, auth headers) are allowed
	MaxAge           int      // Preflight cache duration in seconds
}

// DefaultCORSConfig returns secure default CORS configuration
func DefaultCORSConfig() *CORSConfig {
	return &CORSConfig{
		AllowedOrigins:   []string{}, // Empty = no CORS (most secure default)
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-API-Key", "X-Request-ID"},
		AllowCredentials: false,
		MaxAge:           86400, // 24 hours
	}
}

// CORS creates middleware that handles Cross-Origin Resource Sharing.
func CORS(config *CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			allowed := false
			allowedOrigin := ""

			if config != nil && len(config.AllowedOrigins) > 0 {
				for _, o := range config.AllowedOrigins {
					if o == "*" {
						// Wildcard - allow all origins (warn in logs for production)
						allowed = true
						allowedOrigin = origin
						break
					}
					if o == origin {
						allowed = true
						allowedOrigin = origin
						break
					}
				}
			}

			// Set CORS headers only if origin is allowed
			if allowed && origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
				w.Header().Set("Vary", "Origin") // Important for caching

				methods := "GET, POST, PUT, DELETE, OPTIONS"
				headers := "Content-Type, Authorization, X-API-Key, X-Request-ID"
				if config != nil {
					if len(config.AllowedMethods) > 0 {
						methods = strings.Join(config.AllowedMethods, ", ")
					}
					if len(config.AllowedHeaders) > 0 {
						headers = strings.Join(config.AllowedHeaders, ", ")
					}
					if config.AllowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
					if config.MaxAge > 0 {
						w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))
					}
				}
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}

			// Handle preflight OPTIONS request
			if r.Method == "OPTIONS" {
				if allowed {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusForbidden)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
