package integration

import (
	"os"
	"testing"
)

// TestMain sets JWT_SECRET for the integration test binary if not
// already set. See pkg/api/test_main_test.go for the full rationale —
// NewServer fails-closed when JWT_SECRET is unset (audit fix
// 2026-05-06), so test packages that call api.NewServer must provide
// a value here.
func TestMain(m *testing.M) {
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-only-fixed-secret-pkg-integration-do-not-deploy")
	}
	os.Exit(m.Run())
}
