// Package updater fetches release manifests, downloads matching binaries,
// verifies them by SHA256 checksum, and atomically swaps the running
// executable.
//
// Design notes:
//   - Version comparison uses golang.org/x/mod/semver (Go's curated semver)
//     so "v0.10.0" is correctly newer than "v0.9.0" — the original spike
//     used lexicographic string comparison and silently broke double-digit
//     minor versions.
//   - DownloadRelease writes to dest+".tmp", verifies the SHA256 checksum
//     against the asset's manifest entry, then renames. A mismatch removes
//     the tmp file and returns an error before any swap happens.
//   - Asset.Checksum is REQUIRED (not omitempty in the JSON sense, and an
//     empty value is rejected at download time as "untrusted binary"). The
//     original spike had VerifyChecksum as a public function with no
//     callers; this rewrite wires it in as a security-required step.
//   - ApplyUpdate uses os.Rename + os.Rename rollback on Unix-like
//     filesystems. Windows is not currently handled (the running EXE is
//     locked); a future enhancement would write to a .new file and let
//     the orchestrator swap on restart.
//   - The current version is held in package var Version, settable at
//     build time via:
//     go build -ldflags "-X github.com/dd0wney/cluso-graphdb/pkg/updater.Version=v1.2.3"
//     If unset, Version defaults to "dev" and every update check returns
//     "update available" (intentional for dev builds).
package updater

import "time"

// Release represents a single entry in the release manifest.
type Release struct {
	Version     string    `json:"version"`      // semver, e.g. "v1.2.3"
	Channel     string    `json:"channel"`      // e.g. "stable", "beta"
	ReleaseDate time.Time `json:"release_date"` //nolint:tagliatelle // external manifest schema; keep snake_case
	Description string    `json:"description"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a downloadable binary for a specific OS/arch.
// Checksum is required at download time; a release entry with an empty
// Checksum is rejected as untrusted.
type Asset struct {
	Name     string `json:"name"`
	OS       string `json:"os"`   // matches runtime.GOOS, e.g. "linux"
	Arch     string `json:"arch"` // matches runtime.GOARCH, e.g. "amd64"
	URL      string `json:"url"`
	Checksum string `json:"checksum"` // SHA256 hex digest (case-insensitive)
}

// UpdateStatus is the result of a CheckForUpdates call.
type UpdateStatus struct {
	CurrentVersion  string   `json:"current_version"`          //nolint:tagliatelle // external API schema
	LatestVersion   string   `json:"latest_version"`           //nolint:tagliatelle
	UpdateAvailable bool     `json:"update_available"`         //nolint:tagliatelle
	LatestRelease   *Release `json:"latest_release,omitempty"` //nolint:tagliatelle
}
