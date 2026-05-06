package api

import (
	"os"
	"testing"
)

// TestMain sets JWT_SECRET for the test binary if not already set.
//
// As of the 2026-05-06 audit fix, NewServer fails-closed when JWT_SECRET
// is unset in any environment (the previous random-secret path was a
// security finding). Tests run without env vars by default, so we set a
// fixed dev-only value here. The value never leaves the test binary; it
// must NOT match any production secret.
//
// If JWT_SECRET is already set (e.g., CI passes it in), we leave it alone.
func TestMain(m *testing.M) {
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-only-fixed-secret-pkg-api-do-not-deploy")
	}
	os.Exit(m.Run())
}
