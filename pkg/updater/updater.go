package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// CheckForUpdates queries the release source to see if a newer version is available
func CheckForUpdates(ctx context.Context, currentVersion, channel string, manifestURL string) (*UpdateStatus, error) {
	if manifestURL == "" {
		return nil, fmt.Errorf("manifest URL is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest request failed with status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	// Filter by channel and sort by date/version
	// (Simplified: find the first release in the desired channel)
	var latest *Release
	for _, r := range releases {
		if r.Channel == channel || channel == "" {
			latest = &r
			break
		}
	}

	if latest == nil {
		return &UpdateStatus{
			CurrentVersion:  currentVersion,
			UpdateAvailable: false,
		}, nil
	}

	status := &UpdateStatus{
		CurrentVersion:  currentVersion,
		LatestVersion:   latest.Version,
		UpdateAvailable: isVersionNewer(currentVersion, latest.Version),
		LatestRelease:   latest,
	}

	return status, nil
}

// DownloadRelease downloads the appropriate asset for the current OS/Arch
func DownloadRelease(ctx context.Context, release *Release, targetPath string) error {
	var targetAsset *Asset
	for _, a := range release.Assets {
		if a.OS == runtime.GOOS && a.Arch == runtime.GOARCH {
			targetAsset = &a
			break
		}
	}

	if targetAsset == nil {
		return fmt.Errorf("no asset found for %s/%s in release %s", runtime.GOOS, runtime.GOARCH, release.Version)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetAsset.URL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to start download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	f, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	return nil
}

// ApplyUpdate atomically swaps the current executable with the new one
func ApplyUpdate(newBinaryPath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// For safety, move the current binary to a backup first
	oldPath := exePath + ".old"
	if err := os.Rename(exePath, oldPath); err != nil {
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// Move the new binary to the executable path
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Rollback backup if move fails
		_ = os.Rename(oldPath, exePath)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// Clean up old backup
	_ = os.Remove(oldPath)

	return nil
}

// isVersionNewer performs a simple semantic version comparison
func isVersionNewer(current, latest string) bool {
	// Remove 'v' prefix if present
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	if current == "dev" || current == "" {
		return true // Always update dev builds
	}

	// Basic string comparison for spike (in real app, use semver library)
	return latest > current
}

// VerifyChecksum verifies the SHA256 checksum of a file
func VerifyChecksum(filePath, expectedChecksum string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(h.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}
