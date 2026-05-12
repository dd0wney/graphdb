package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name            string
		current, latest string
		want            bool
	}{
		// The case the original spike got wrong: "v0.10.0" vs "v0.9.0".
		// String compare returns false (because '0' < '9' lexicographically).
		// Semver compare returns true (because 10 > 9 numerically).
		{"double-digit minor newer than single-digit", "v0.9.0", "v0.10.0", true},
		{"double-digit patch newer than single-digit", "v1.0.9", "v1.0.10", true},

		// Standard cases.
		{"minor bump", "v1.0.0", "v1.1.0", true},
		{"patch bump", "v1.2.3", "v1.2.4", true},
		{"major bump", "v1.9.9", "v2.0.0", true},
		{"older latest", "v2.0.0", "v1.9.9", false},
		{"equal", "v1.1.0", "v1.1.0", false},

		// Special current values.
		{"dev always upgradable", "dev", "v1.0.0", true},
		{"empty always upgradable", "", "v0.1.0", true},

		// Invalid semver returns false (defensive — don't upgrade on garbage).
		{"invalid current", "garbage", "v1.0.0", false},
		{"invalid latest", "v1.0.0", "not-a-version", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNewer(tt.current, tt.latest); got != tt.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v",
					tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestPickLatest(t *testing.T) {
	releases := []Release{
		{Version: "v1.0.0", Channel: "stable"},
		{Version: "v1.1.0", Channel: "stable"},
		{Version: "v1.2.0-beta", Channel: "beta"},
		{Version: "v1.2.0", Channel: "stable"},
		{Version: "not-semver", Channel: "stable"}, // skipped
		{Version: "v0.10.0", Channel: "stable"},    // older than 1.0
	}

	t.Run("picks highest in channel", func(t *testing.T) {
		got := pickLatest(releases, "stable")
		if got == nil || got.Version != "v1.2.0" {
			t.Errorf("got %v, want v1.2.0", got)
		}
	})

	t.Run("empty channel matches any", func(t *testing.T) {
		got := pickLatest(releases, "")
		if got == nil || got.Version != "v1.2.0" {
			t.Errorf("got %v, want v1.2.0 (or v1.2.0-beta if pre-release ordering differs)", got)
		}
	})

	t.Run("no matching channel returns nil", func(t *testing.T) {
		if got := pickLatest(releases, "nightly"); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("empty releases returns nil", func(t *testing.T) {
		if got := pickLatest(nil, "stable"); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestCheckForUpdates_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		releases := []Release{
			{Version: "v1.0.0", Channel: "stable", ReleaseDate: time.Now()},
			{Version: "v1.2.0", Channel: "stable", ReleaseDate: time.Now()},
		}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	origVersion := Version
	Version = "v1.0.0"
	defer func() { Version = origVersion }()

	status, err := CheckForUpdates(context.Background(), server.URL, "stable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.LatestVersion != "v1.2.0" {
		t.Errorf("LatestVersion = %q, want v1.2.0", status.LatestVersion)
	}
	if !status.UpdateAvailable {
		t.Error("UpdateAvailable = false, want true")
	}
	if status.CurrentVersion != "v1.0.0" {
		t.Errorf("CurrentVersion = %q, want v1.0.0", status.CurrentVersion)
	}
}

func TestCheckForUpdates_NoMatchingChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		releases := []Release{{Version: "v1.0.0", Channel: "stable"}}
		_ = json.NewEncoder(w).Encode(releases)
	}))
	defer server.Close()

	status, err := CheckForUpdates(context.Background(), server.URL, "nightly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.UpdateAvailable {
		t.Error("UpdateAvailable = true, want false (no nightly release)")
	}
}

func TestCheckForUpdates_BadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := CheckForUpdates(context.Background(), server.URL, "stable")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not mention 404", err.Error())
	}
}

func TestCheckForUpdates_EmptyURL(t *testing.T) {
	_, err := CheckForUpdates(context.Background(), "", "stable")
	if err == nil {
		t.Fatal("expected error for empty manifest URL, got nil")
	}
}

func TestDownloadRelease_HappyPath(t *testing.T) {
	payload := []byte("simulated graphdb binary content")
	checksum := sha256Hex(payload)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	release := &Release{
		Version: "v1.2.3",
		Assets: []Asset{
			{
				Name:     "graphdb",
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				URL:      server.URL,
				Checksum: checksum,
			},
		},
	}

	dest := filepath.Join(t.TempDir(), "graphdb")
	if err := DownloadRelease(context.Background(), release, dest); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// dest exists and has the right content
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(payload) {
		t.Error("dest content mismatch")
	}
	// .tmp removed
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected .tmp to be removed, got err=%v", err)
	}
}

func TestDownloadRelease_ChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("actual content"))
	}))
	defer server.Close()

	release := &Release{
		Version: "v1.2.3",
		Assets: []Asset{
			{
				Name:     "graphdb",
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				URL:      server.URL,
				Checksum: "deadbeef0000000000000000000000000000000000000000000000000000beef",
			},
		},
	}

	dest := filepath.Join(t.TempDir(), "graphdb")
	err := DownloadRelease(context.Background(), release, dest)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q does not mention checksum mismatch", err.Error())
	}

	// dest must NOT exist (security: never write a binary that failed verification)
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("dest exists after checksum mismatch (security regression!): %v", err)
	}
	// .tmp must NOT linger
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp exists after checksum mismatch: %v", err)
	}
}

func TestDownloadRelease_NoChecksum(t *testing.T) {
	release := &Release{
		Version: "v1.2.3",
		Assets: []Asset{
			{Name: "graphdb", OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "http://example.com", Checksum: ""},
		},
	}
	err := DownloadRelease(context.Background(), release, filepath.Join(t.TempDir(), "graphdb"))
	if !errors.Is(err, ErrNoChecksum) {
		t.Errorf("expected ErrNoChecksum, got %v", err)
	}
}

func TestDownloadRelease_NoAsset(t *testing.T) {
	release := &Release{
		Version: "v1.2.3",
		Assets: []Asset{
			{Name: "graphdb", OS: "alien-os", Arch: "alien-arch", URL: "http://example.com", Checksum: "x"},
		},
	}
	err := DownloadRelease(context.Background(), release, filepath.Join(t.TempDir(), "graphdb"))
	if !errors.Is(err, ErrNoAsset) {
		t.Errorf("expected ErrNoAsset, got %v", err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	payload := []byte("hello world")
	expected := sha256Hex(payload)

	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("pass", func(t *testing.T) {
		if err := VerifyChecksum(path, expected); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("pass case-insensitive", func(t *testing.T) {
		if err := VerifyChecksum(path, strings.ToUpper(expected)); err != nil {
			t.Errorf("unexpected error with uppercase checksum: %v", err)
		}
	})

	t.Run("fail mismatch", func(t *testing.T) {
		err := VerifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000")
		if err == nil {
			t.Error("expected mismatch error, got nil")
		}
	})

	t.Run("fail missing file", func(t *testing.T) {
		if err := VerifyChecksum(filepath.Join(t.TempDir(), "nope"), expected); err == nil {
			t.Error("expected file-open error, got nil")
		}
	})
}

func TestApplyUpdate_HappyPath(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "graphdb")
	newPath := filepath.Join(dir, "graphdb.new")

	if err := os.WriteFile(exePath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := applyUpdateTo(newPath, exePath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read after swap: %v", err)
	}
	if string(got) != "NEW" {
		t.Errorf("exePath content = %q, want NEW", got)
	}

	// .old backup is removed on success
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup exists after successful swap, want removed: %v", err)
	}
	// new path is consumed
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("new path still exists after swap, want consumed: %v", err)
	}
}

func TestApplyUpdate_RollbackOnFailedInstall(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "graphdb")
	newPath := filepath.Join(dir, "does-not-exist") // intentionally missing

	if err := os.WriteFile(exePath, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := applyUpdateTo(newPath, exePath)
	if err == nil {
		t.Fatal("expected error when new binary is missing, got nil")
	}

	// Critical: after a failed swap, the original binary must be restored.
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read after failed swap: %v", err)
	}
	if string(got) != "OLD" {
		t.Errorf("exePath content = %q, want OLD (rollback should have restored)", got)
	}

	// Backup should be cleaned up by the rollback.
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Errorf("backup still exists after rollback: %v", err)
	}
}

// sha256Hex is a test helper.
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
