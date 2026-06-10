package api

import "testing"

// TestRateLimiter_ActiveByDefault pins security audit finding H-5: after
// server construction both the auth brute-force limiter and the general
// limiter must be active. Before the fix InitRateLimiterFromEnv was never
// called, so both were nil and middleware.RateLimit passed everything
// through — no protection at all.
func TestRateLimiter_ActiveByDefault(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "") // exercise the default (unset) path
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if server.authRateLimiter == nil {
		t.Error("auth rate limiter is nil — brute-force protection inactive (H-5)")
	}
	if server.rateLimiter == nil {
		t.Error("general rate limiter is nil — should be on by default (H-5)")
	}
}

// TestRateLimiter_GeneralDisabledViaEnv pins the opt-out escape hatch: the
// general limiter can be turned off, but auth limiting stays on.
func TestRateLimiter_GeneralDisabledViaEnv(t *testing.T) {
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if server.authRateLimiter == nil {
		t.Error("auth rate limiter must stay on even when general limiting is disabled")
	}
	if server.rateLimiter != nil {
		t.Error("general rate limiter should be nil when RATE_LIMIT_ENABLED=false")
	}
}
