package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/updater"
)

const defaultUpdateManifestURL = "https://raw.githubusercontent.com/dd0wney/graphdb/main/releases.json"

// UpdateJobStatus is the lifecycle state of an update job.
type UpdateJobStatus string

// Update job lifecycle states. UpdateJobRunning means the goroutine is
// active; the two terminal states (Succeeded / Failed) have CompletedAt
// set and (for Failed) an Error message.
const (
	UpdateJobRunning   UpdateJobStatus = "running"
	UpdateJobSucceeded UpdateJobStatus = "succeeded"
	UpdateJobFailed    UpdateJobStatus = "failed"
)

// UpdateJob is the externally-visible state of an in-flight or completed
// update.
type UpdateJob struct {
	ID            string          `json:"id"`
	Status        UpdateJobStatus `json:"status"`
	TargetVersion string          `json:"target_version"`         //nolint:tagliatelle // external API schema
	StartedAt     time.Time       `json:"started_at"`             //nolint:tagliatelle
	CompletedAt   *time.Time      `json:"completed_at,omitempty"` //nolint:tagliatelle
	Error         string          `json:"error,omitempty"`
}

// updateJobManager tracks update jobs in memory. State is lost on
// process restart — which is normal: a successful update IS the
// restart, and preserving job state across the binary swap is moot.
type updateJobManager struct {
	mu   sync.RWMutex
	jobs map[string]*UpdateJob
}

func newUpdateJobManager() *updateJobManager {
	return &updateJobManager{jobs: make(map[string]*UpdateJob)}
}

func (m *updateJobManager) start(targetVersion string) *UpdateJob {
	job := &UpdateJob{
		ID:            generateUpdateJobID(),
		Status:        UpdateJobRunning,
		TargetVersion: targetVersion,
		StartedAt:     time.Now().UTC(),
	}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()
	return cloneUpdateJob(job)
}

func (m *updateJobManager) complete(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return
	}
	now := time.Now().UTC()
	job.CompletedAt = &now
	if err != nil {
		job.Status = UpdateJobFailed
		job.Error = err.Error()
	} else {
		job.Status = UpdateJobSucceeded
	}
}

// get returns a defensive copy. Callers must not mutate the returned
// struct expecting changes to persist.
func (m *updateJobManager) get(id string) (*UpdateJob, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneUpdateJob(job), true
}

func cloneUpdateJob(j *UpdateJob) *UpdateJob {
	out := *j
	if j.CompletedAt != nil {
		t := *j.CompletedAt
		out.CompletedAt = &t
	}
	return &out
}

func generateUpdateJobID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// UpdateCheckResponse is the response body for GET /admin/update/check.
type UpdateCheckResponse struct {
	CurrentVersion  string           `json:"current_version"`          //nolint:tagliatelle
	LatestVersion   string           `json:"latest_version"`           //nolint:tagliatelle
	UpdateAvailable bool             `json:"update_available"`         //nolint:tagliatelle
	LatestRelease   *updater.Release `json:"latest_release,omitempty"` //nolint:tagliatelle
}

// UpdateApplyResponse is the response body for POST /admin/update/apply.
type UpdateApplyResponse struct {
	JobID         string `json:"job_id"`         //nolint:tagliatelle
	TargetVersion string `json:"target_version"` //nolint:tagliatelle
	Message       string `json:"message"`
}

func updateManifestURL() string {
	if url := os.Getenv("UPDATE_MANIFEST_URL"); url != "" {
		return url
	}
	return defaultUpdateManifestURL
}

// handleUpdateCheck implements GET /admin/update/check.
// Query params: channel (default "stable").
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "stable"
	}

	status, err := updater.CheckForUpdates(r.Context(), updateManifestURL(), channel)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("check for updates: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, UpdateCheckResponse{
		CurrentVersion:  status.CurrentVersion,
		LatestVersion:   status.LatestVersion,
		UpdateAvailable: status.UpdateAvailable,
		LatestRelease:   status.LatestRelease,
	})
}

// handleUpdateApply implements POST /admin/update/apply.
// Body: {"channel": "stable"} (channel optional).
// Returns 202 Accepted with a job ID; caller polls /admin/update/jobs/{id}.
//
// Unlike the audited spike, this does NOT call os.Exit at the end of the
// goroutine. The binary is swapped on disk; the orchestrator (systemd,
// docker, kubelet) is responsible for restart timing. Exiting from inside
// an HTTP handler races with in-flight requests.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}
	if r.ContentLength > 0 && r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if req.Channel == "" {
		req.Channel = "stable"
	}

	status, err := updater.CheckForUpdates(r.Context(), updateManifestURL(), req.Channel)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("check for updates: %v", err))
		return
	}
	if !status.UpdateAvailable {
		s.respondError(w, http.StatusBadRequest, "no update available")
		return
	}

	job := s.updateJobs.start(status.LatestVersion)
	target := status.LatestRelease

	// Start the download+apply in a tracked goroutine. Errors are recorded
	// on the job, retrievable via GET /admin/update/jobs/{id}.
	// nolint:gosec // G118: intentional context.Background — the goroutine
	// outlives the HTTP request that returns 202 below; r.Context() would
	// cancel the download/apply within milliseconds.
	go s.runUpdateJob(job.ID, target)

	s.respondJSON(w, http.StatusAccepted, UpdateApplyResponse{
		JobID:         job.ID,
		TargetVersion: status.LatestVersion,
		Message:       "update started; poll /admin/update/jobs/" + job.ID + " for status",
	})
}

// runUpdateJob is the goroutine body for an apply. Separated so tests can
// run it synchronously without invoking the HTTP layer.
func (s *Server) runUpdateJob(jobID string, release *updater.Release) {
	// Fresh context — the HTTP request that triggered this has likely
	// already returned by the time the goroutine reaches here.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tmpPath := filepath.Join(os.TempDir(), "graphdb-update-"+jobID)
	if err := updater.DownloadRelease(ctx, release, tmpPath); err != nil {
		log.Printf("update job %s: download failed: %v", jobID, err)
		s.updateJobs.complete(jobID, fmt.Errorf("download: %w", err))
		return
	}

	if err := updater.ApplyUpdate(tmpPath); err != nil {
		log.Printf("update job %s: apply failed: %v", jobID, err)
		s.updateJobs.complete(jobID, fmt.Errorf("apply: %w", err))
		return
	}

	log.Printf("update job %s: succeeded → %s; orchestrator should restart", jobID, release.Version)
	s.updateJobs.complete(jobID, nil)
}

// handleUpdateJob implements GET /admin/update/jobs/{id}.
func (s *Server) handleUpdateJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const prefix = "/admin/update/jobs/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		s.respondError(w, http.StatusBadRequest, "invalid jobs path")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if id == "" {
		s.respondError(w, http.StatusBadRequest, "job ID required")
		return
	}
	job, ok := s.updateJobs.get(id)
	if !ok {
		s.respondError(w, http.StatusNotFound, "job not found")
		return
	}
	s.respondJSON(w, http.StatusOK, job)
}
