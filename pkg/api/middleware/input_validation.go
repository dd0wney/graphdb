package middleware

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/security"
)

// InputValidationConfig configures input validation middleware
type InputValidationConfig struct {
	SkipPaths      []string // Paths to skip validation for
	MaxBodySize    int      // Maximum body size to validate (default: 10MB)
	ValidateAll    bool     // If true, validate all methods (not just POST/PUT/PATCH)
}

// DefaultInputValidationConfig returns default configuration
func DefaultInputValidationConfig() *InputValidationConfig {
	return &InputValidationConfig{
		SkipPaths: []string{
			"/auth/login",
			"/auth/register",
			"/auth/refresh",
		},
		MaxBodySize: 10 * 1024 * 1024, // 10MB
		ValidateAll: false,
	}
}

// InputValidation creates middleware that validates input for security issues.
// This protects against injection attacks, XSS, path traversal, etc.
func InputValidation(config *InputValidationConfig) func(http.Handler) http.Handler {
	if config == nil {
		config = DefaultInputValidationConfig()
	}

	validator := security.NewInputValidator()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip validation for certain paths
			for _, path := range config.SkipPaths {
				if strings.HasPrefix(r.URL.Path, path) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Only validate POST/PUT/PATCH requests with bodies (unless ValidateAll is true)
			if !config.ValidateAll {
				if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Enforce body size limit BEFORE reading to prevent DoS
			// Use http.MaxBytesReader to limit the amount we read
			r.Body = http.MaxBytesReader(w, r.Body, int64(config.MaxBodySize))

			// Read request body (now limited by MaxBytesReader)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				// MaxBytesReader returns a specific error type for oversized bodies
				if err.Error() == "http: request body too large" {
					http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
					return
				}
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

			// Validate maximum length
			if err := validator.ValidateString(bodyStr, config.MaxBodySize); err != nil {
				log.Printf("Input validation failed: %v", err)
				http.Error(w, "Invalid input: request too large", http.StatusBadRequest)
				return
			}

			// Restore body for next handler
			r.Body = io.NopCloser(strings.NewReader(bodyStr))

			next.ServeHTTP(w, r)
		})
	}
}
