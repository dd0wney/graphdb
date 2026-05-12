package updater

import (
	"time"
)

// Release represents a software release
type Release struct {
	Version     string    `json:"version"`
	Channel     string    `json:"channel"`
	ReleaseDate time.Time `json:"release_date"`
	Description string    `json:"description"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a downloadable file for a release
type Asset struct {
	Name     string `json:"name"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	Checksum string `json:"checksum,omitempty"`
}

// UpdateStatus contains the result of an update check
type UpdateStatus struct {
	CurrentVersion string   `json:"current_version"`
	LatestVersion  string   `json:"latest_version"`
	UpdateAvailable bool    `json:"update_available"`
	LatestRelease  *Release `json:"latest_release,omitempty"`
}
