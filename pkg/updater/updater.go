package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"
)

// Version is the current build's semver string. Set via -ldflags:
//
//	go build -ldflags "-X github.com/dd0wney/cluso-graphdb/pkg/updater.Version=v1.2.3"
//
// If unset (i.e., "dev"), every CheckForUpdates call reports an update
// available — intentional for development builds.
var Version = "dev"

// ErrNoChecksum is returned when a release asset has an empty checksum.
// Refusing to download in that case is a security policy: a manifest
// author who omits checksums is asking us to install an unverified binary.
var ErrNoChecksum = errors.New("asset has no checksum; refusing to download untrusted binary")

// ErrNoAsset is returned when a release contains no asset matching the
// current runtime.GOOS / runtime.GOARCH.
var ErrNoAsset = errors.New("no asset for current OS/arch in release")

// CheckForUpdates fetches the JSON manifest at manifestURL, picks the
// highest-semver release in the named channel (channel "" matches any),
// and returns whether it's newer than the running Version.
func CheckForUpdates(ctx context.Context, manifestURL, channel string) (*UpdateStatus, error) {
	if manifestURL == "" {
		return nil, errors.New("manifest URL is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request returned status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	latest := pickLatest(releases, channel)
	if latest == nil {
		return &UpdateStatus{CurrentVersion: Version, UpdateAvailable: false}, nil
	}

	return &UpdateStatus{
		CurrentVersion:  Version,
		LatestVersion:   latest.Version,
		UpdateAvailable: IsNewer(Version, latest.Version),
		LatestRelease:   latest,
	}, nil
}

// pickLatest returns the highest-semver release in the given channel, or
// nil if no releases match. Releases with invalid semver are skipped.
func pickLatest(releases []Release, channel string) *Release {
	var latest *Release
	for i := range releases {
		r := &releases[i]
		if channel != "" && r.Channel != channel {
			continue
		}
		if !semver.IsValid(r.Version) {
			continue
		}
		if latest == nil || semver.Compare(r.Version, latest.Version) > 0 {
			latest = r
		}
	}
	return latest
}

// IsNewer reports whether latest is a strictly higher semver than current.
// "dev" and "" current versions are always considered older (so every
// dev build sees updates).
func IsNewer(current, latest string) bool {
	if current == "dev" || current == "" {
		return true
	}
	if !semver.IsValid(current) || !semver.IsValid(latest) {
		return false
	}
	return semver.Compare(latest, current) > 0
}

// DownloadRelease downloads the asset matching runtime.GOOS/GOARCH from
// the given release, writing first to dest+".tmp", then verifying its
// SHA256 against the manifest checksum, then renaming to dest. On any
// failure the tmp file is removed and an error is returned — dest is
// never written unless the checksum verifies.
func DownloadRelease(ctx context.Context, release *Release, dest string) error {
	asset := pickAsset(release)
	if asset == nil {
		return fmt.Errorf("%w: %s/%s in release %s",
			ErrNoAsset, runtime.GOOS, runtime.GOARCH, release.Version)
	}
	if asset.Checksum == "" {
		return fmt.Errorf("%w: %s", ErrNoChecksum, asset.Name)
	}

	tmpPath := dest + ".tmp"
	if err := downloadToFile(ctx, asset.URL, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := VerifyChecksum(tmpPath, asset.Checksum); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("verify %s: %w", asset.Name, err)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename %s -> %s: %w", tmpPath, dest, err)
	}
	return nil
}

func pickAsset(release *Release) *Asset {
	for i := range release.Assets {
		a := &release.Assets[i]
		if a.OS == runtime.GOOS && a.Arch == runtime.GOARCH {
			return a
		}
	}
	return nil
}

func downloadToFile(ctx context.Context, url, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// VerifyChecksum SHA256-hashes the file at path and returns nil iff its
// hex digest matches expected (case-insensitive). Wired into
// DownloadRelease — this is not an opt-in helper, it's a required step.
func VerifyChecksum(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: want %s, got %s", expected, got)
	}
	return nil
}

// ApplyUpdate atomically swaps the running executable with the binary at
// newBinaryPath. The current binary is moved to <exe>.old as a backup; if
// the second rename fails, the backup is restored. Successful swaps remove
// the backup.
//
// On Unix-like filesystems both renames are atomic. Windows is not
// currently handled — the running EXE is locked there; callers on Windows
// will see a rename error. A future enhancement would write to a .new
// file and let the orchestrator perform the swap on restart.
func ApplyUpdate(newBinaryPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolve symlinks for %s: %w", exePath, err)
	}
	return applyUpdateTo(newBinaryPath, realExe)
}

// applyUpdateTo performs the actual swap. Split out for testability: tests
// can drive arbitrary tempdir paths without renaming the real test binary.
func applyUpdateTo(newBinaryPath, exePath string) error {
	backupPath := exePath + ".old"
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore the backup so the user is left with a working binary.
		if rbErr := os.Rename(backupPath, exePath); rbErr != nil {
			return fmt.Errorf("install new binary failed (%w) and rollback also failed: %v",
				err, rbErr)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	// Successfully swapped — remove the backup. Failure to remove is
	// cosmetic (leftover .old file); don't propagate.
	_ = os.Remove(backupPath)
	return nil
}
