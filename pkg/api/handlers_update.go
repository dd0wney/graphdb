package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/updater"
)

type UpdateCheckResponse struct {
	CurrentVersion string           `json:"current_version"`
	LatestVersion  string           `json:"latest_version"`
	UpdateAvailable bool            `json:"update_available"`
	LatestRelease  *updater.Release `json:"latest_release,omitempty"`
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	currentVersion := "v1.0.0" // Should be injected at build time
	manifestURL := os.Getenv("UPDATE_MANIFEST_URL")
	if manifestURL == "" {
		manifestURL = "https://raw.githubusercontent.com/dd0wney/graphdb/main/releases.json"
	}

	status, err := updater.CheckForUpdates(r.Context(), currentVersion, channel, manifestURL)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to check for updates: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, UpdateCheckResponse{
		CurrentVersion:  status.CurrentVersion,
		LatestVersion:   status.LatestVersion,
		UpdateAvailable: status.UpdateAvailable,
		LatestRelease:   status.LatestRelease,
	})
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Channel == "" {
		req.Channel = "stable"
	}

	currentVersion := "v1.0.0"
	manifestURL := os.Getenv("UPDATE_MANIFEST_URL")
	if manifestURL == "" {
		manifestURL = "https://raw.githubusercontent.com/dd0wney/graphdb/main/releases.json"
	}

	status, err := updater.CheckForUpdates(r.Context(), currentVersion, req.Channel, manifestURL)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Update check failed: %v", err))
		return
	}

	if !status.UpdateAvailable {
		s.respondError(w, http.StatusBadRequest, "No update available")
		return
	}

	// In a real implementation, we'd start a background goroutine to perform the update
	// so we can respond to the HTTP request first.
	go func() {
		tempPath := os.TempDir() + "/graphdb-srv-new"
		if err := updater.DownloadRelease(context.Background(), status.LatestRelease, tempPath); err != nil {
			fmt.Printf("Update background task: download failed: %v\n", err)
			return
		}

		if err := updater.ApplyUpdate(tempPath); err != nil {
			fmt.Printf("Update background task: apply failed: %v\n", err)
			return
		}

		fmt.Printf("Update applied. Restarting server...\n")
		// Force exit, let orchestrator (systemd/docker) restart
		os.Exit(0)
	}()

	s.respondJSON(w, http.StatusAccepted, map[string]string{
		"message": "Update started. Server will restart shortly.",
		"version": status.LatestVersion,
	})
}
