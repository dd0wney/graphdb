package api

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestAdminPasswordIgnored is a table-driven unit test on the pure helper
// adminPasswordIgnored (#456). It pins the exact truth table the fix
// depends on, independent of the full NewServerWithDataDir bootstrap.
func TestAdminPasswordIgnored(t *testing.T) {
	tests := []struct {
		name              string
		existingUserCount int
		adminPasswordEnv  string
		want              bool
	}{
		{
			name:              "empty store, no ADMIN_PASSWORD",
			existingUserCount: 0,
			adminPasswordEnv:  "",
			want:              false,
		},
		{
			name:              "empty store, ADMIN_PASSWORD set — normal bootstrap, not this fix's case",
			existingUserCount: 0,
			adminPasswordEnv:  "secret",
			want:              false,
		},
		{
			name:              "existing users, no ADMIN_PASSWORD — nothing to warn about",
			existingUserCount: 3,
			adminPasswordEnv:  "",
			want:              false,
		},
		{
			name:              "existing users, ADMIN_PASSWORD set — the #456 case",
			existingUserCount: 3,
			adminPasswordEnv:  "secret",
			want:              true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adminPasswordIgnored(tt.existingUserCount, tt.adminPasswordEnv)
			if got != tt.want {
				t.Errorf("adminPasswordIgnored(%d, %q) = %v, want %v",
					tt.existingUserCount, tt.adminPasswordEnv, got, tt.want)
			}
		})
	}
}

// TestAdminPasswordIgnoredEndToEnd exercises NewServerWithDataDir twice
// against the same dataDir — the restored-backup scenario from #456: a
// first boot bootstraps an admin user via ADMIN_PASSWORD, and a second
// boot against the same (now non-empty) auth store sets a *different*
// ADMIN_PASSWORD expecting it to take effect. Before the fix this was
// silently ignored; the fix logs a warning naming ADMIN_PASSWORD and
// "ignored".
//
// Tests in this package do not use t.Parallel() (confirmed before writing
// this test), so redirecting the shared log package output for the
// duration of the second NewServerWithDataDir call is safe.
func TestAdminPasswordIgnoredEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "graph")
	dataDir := filepath.Join(tmp, "datadir")

	graph, err := storage.NewGraphStorage(graphPath)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	// First boot: empty auth store, ADMIN_PASSWORD set — normal bootstrap.
	t.Setenv("ADMIN_PASSWORD", "first-password")
	srv1, err := NewServerWithDataDir(graph, 0, dataDir)
	if err != nil {
		t.Fatalf("NewServerWithDataDir #1: %v", err)
	}

	users := srv1.userStore.ListUsers()
	if len(users) != 1 {
		t.Fatalf("expected exactly 1 user after first boot, got %d", len(users))
	}
	adminUser := users[0]

	// Second boot: same dataDir, now-non-empty auth store, a *different*
	// ADMIN_PASSWORD. Capture log output to assert the warning fires.
	originalOutput := log.Writer()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(originalOutput)

	t.Setenv("ADMIN_PASSWORD", "second-password")
	srv2, err := NewServerWithDataDir(graph, 0, dataDir)

	// Restore log output immediately after the call under test so any
	// subsequent test failures/logging aren't silently swallowed.
	log.SetOutput(originalOutput)

	if err != nil {
		t.Fatalf("NewServerWithDataDir #2: %v", err)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "ADMIN_PASSWORD") {
		t.Errorf("expected log output to mention ADMIN_PASSWORD, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "ignored") {
		t.Errorf("expected log output to mention the setting was ignored, got: %s", logOutput)
	}

	// Bootstrap must have been correctly skipped: still exactly 1 user.
	usersAfter := srv2.userStore.ListUsers()
	if len(usersAfter) != 1 {
		t.Fatalf("expected user count to remain 1 after second boot, got %d", len(usersAfter))
	}

	// The second ADMIN_PASSWORD genuinely had no effect: the original
	// admin's password (from the first boot) still works...
	if !srv2.userStore.VerifyPassword(adminUser, "first-password") {
		t.Error("first-password no longer verifies against the admin user — auth store was not preserved across the second boot")
	}
	// ...and the second, ignored ADMIN_PASSWORD does not authenticate.
	if srv2.userStore.VerifyPassword(adminUser, "second-password") {
		t.Error("second-password verifies against the admin user — ADMIN_PASSWORD was NOT ignored as expected")
	}
}
