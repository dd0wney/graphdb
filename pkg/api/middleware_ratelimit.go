package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/dd0wney/graphdb/pkg/api/middleware"
	"github.com/dd0wney/graphdb/pkg/audit"
	"github.com/dd0wney/graphdb/pkg/auth"
)

// rateLimitMiddleware applies rate limiting per client IP
func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	getClientID := func(r *http.Request) string {
		// Get client identifier (IP address, or user ID if authenticated)
		clientID := getIPAddress(r)

		// If authenticated, use user ID for more granular limiting
		if claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims); ok {
			clientID = "user:" + claims.UserID
		}
		return clientID
	}

	onLimited := func(w http.ResponseWriter, r *http.Request, clientID string) {
		// Record in audit log
		s.logAuditEvent(&audit.Event{
			Action:       audit.ActionRead,
			ResourceType: audit.ResourceQuery,
			Status:       audit.StatusFailure,
			IPAddress:    getIPAddress(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"error":     "rate_limit_exceeded",
				"client_id": clientID,
				"path":      r.URL.Path,
				"method":    r.Method,
			},
		})
	}

	return middleware.RateLimit(s.rateLimiter, getClientID, onLimited)(next)
}

// InitRateLimiterFromEnv initializes rate limiter from environment variables
func (s *Server) InitRateLimiterFromEnv() {
	// Always initialize auth rate limiter for brute-force protection
	// This is a security-critical feature that should not be disabled
	s.initAuthRateLimiter()

	// General API rate limiting is ON by default (security audit H-5):
	// previously it was opt-in AND this function was never invoked, so
	// there was no flood protection at all. Operators can disable it with
	// RATE_LIMIT_ENABLED=false (the auth brute-force limiter stays on
	// regardless). The default limits (100 req/s sustained, burst 200,
	// per authenticated user or per IP) are generous enough not to throttle
	// normal use; tune via RATE_LIMIT_RPS / RATE_LIMIT_BURST.
	enabled := os.Getenv("RATE_LIMIT_ENABLED")
	if enabled == "false" || enabled == "0" {
		log.Printf("General API rate limiting DISABLED via RATE_LIMIT_ENABLED=%s (auth rate limiting stays on)", enabled)
		return
	}

	config := middleware.DefaultRateLimitConfig()

	// Parse optional configuration from environment
	if rps := os.Getenv("RATE_LIMIT_RPS"); rps != "" {
		if val, err := strconv.ParseFloat(rps, 64); err == nil && val > 0 {
			config.RequestsPerSecond = val
		}
	}

	if burst := os.Getenv("RATE_LIMIT_BURST"); burst != "" {
		if val, err := strconv.Atoi(burst); err == nil && val > 0 {
			config.BurstSize = val
		}
	}

	s.rateLimiter = middleware.NewRateLimiter(config)
	log.Printf("General API rate limiting enabled: %.0f req/s, burst size %d", config.RequestsPerSecond, config.BurstSize)
}

// initAuthRateLimiter initializes the auth-specific rate limiter with stricter limits.
// This helps prevent brute-force password attacks on login/register endpoints.
func (s *Server) initAuthRateLimiter() {
	config := &middleware.RateLimitConfig{
		RequestsPerSecond: 5,  // Much stricter than general API
		BurstSize:         10, // Allow small bursts
		CleanupInterval:   5 * time.Minute,
		ClientExpiration:  30 * time.Minute, // Longer expiration for auth tracking
		MaxClients:        50000,            // Lower than general limiter
	}

	// Allow environment variable overrides
	if rps := os.Getenv("AUTH_RATE_LIMIT_RPS"); rps != "" {
		if val, err := strconv.ParseFloat(rps, 64); err == nil && val > 0 {
			config.RequestsPerSecond = val
		}
	}

	if burst := os.Getenv("AUTH_RATE_LIMIT_BURST"); burst != "" {
		if val, err := strconv.Atoi(burst); err == nil && val > 0 {
			config.BurstSize = val
		}
	}

	s.authRateLimiter = middleware.NewRateLimiter(config)
	log.Printf("Auth rate limiting enabled: %.0f req/s, burst size %d", config.RequestsPerSecond, config.BurstSize)
}

// authRateLimitMiddleware applies stricter rate limiting for authentication endpoints.
// Uses IP-based limiting only (not user ID) since these are pre-auth requests.
func (s *Server) authRateLimitMiddleware(next http.Handler) http.Handler {
	// For auth endpoints, always use IP-based limiting.
	// This prevents attackers from bypassing limits by trying different usernames.
	getClientID := getIPAddress

	onLimited := func(w http.ResponseWriter, r *http.Request, clientID string) {
		// Record in audit log with auth-specific context
		s.logAuditEvent(&audit.Event{
			Action:       audit.ActionAuth,
			ResourceType: audit.ResourceAuth,
			Status:       audit.StatusFailure,
			IPAddress:    getIPAddress(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"error":     "auth_rate_limit_exceeded",
				"client_ip": clientID,
				"path":      r.URL.Path,
				"method":    r.Method,
				"reason":    "brute_force_protection",
			},
		})
		log.Printf("AUTH RATE LIMIT: IP %s exceeded auth rate limit on %s", clientID, r.URL.Path)
	}

	return middleware.RateLimit(s.authRateLimiter, getClientID, onLimited)(next)
}
