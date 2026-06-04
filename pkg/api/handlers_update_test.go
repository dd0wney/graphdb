package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/updater"
)

func TestUpdateJobManager(t *testing.T) {
	m := newUpdateJobManager()

	t.Run("get missing returns false", func(t *testing.T) {
		if _, ok := m.get("nope"); ok {
			t.Error("expected false for missing job")
		}
	})

	t.Run("start then get returns running", func(t *testing.T) {
		started := m.start("v1.2.3")
		if started.Status != UpdateJobRunning {
			t.Errorf("status = %s, want running", started.Status)
		}
		got, ok := m.get(started.ID)
		if !ok {
			t.Fatal("expected job after start")
		}
		if got.Status != UpdateJobRunning {
			t.Errorf("got status %s, want running", got.Status)
		}
		if got.TargetVersion != "v1.2.3" {
			t.Errorf("TargetVersion = %q, want v1.2.3", got.TargetVersion)
		}
		if got.CompletedAt != nil {
			t.Error("CompletedAt should be nil for running job")
		}
	})

	t.Run("complete with success", func(t *testing.T) {
		started := m.start("v1.2.3")
		m.complete(started.ID, nil)
		got, _ := m.get(started.ID)
		if got.Status != UpdateJobSucceeded {
			t.Errorf("status = %s, want succeeded", got.Status)
		}
		if got.CompletedAt == nil {
			t.Error("CompletedAt should be set on terminal state")
		}
		if got.Error != "" {
			t.Errorf("Error = %q on success, want empty", got.Error)
		}
	})

	t.Run("complete with error", func(t *testing.T) {
		started := m.start("v1.2.3")
		m.complete(started.ID, fmt.Errorf("oops"))
		got, _ := m.get(started.ID)
		if got.Status != UpdateJobFailed {
			t.Errorf("status = %s, want failed", got.Status)
		}
		if got.Error != "oops" {
			t.Errorf("Error = %q, want oops", got.Error)
		}
	})

	t.Run("get returns a copy (no mutation leakage)", func(t *testing.T) {
		started := m.start("v1.2.3")
		got, _ := m.get(started.ID)
		got.Status = UpdateJobFailed // mutate the returned copy
		again, _ := m.get(started.ID)
		if again.Status != UpdateJobRunning {
			t.Errorf("internal job mutated via returned reference; isolation broken (status now %s)",
				again.Status)
		}
	})

	t.Run("complete on missing job is a no-op", func(t *testing.T) {
		// Should not panic.
		m.complete("nope-not-here", fmt.Errorf("won't be recorded"))
	})
}

func TestHandleUpdateCheck_HappyPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	manifest := []updater.Release{
		{Version: "v1.0.0", Channel: "stable", ReleaseDate: time.Now()},
		{Version: "v1.2.0", Channel: "stable", ReleaseDate: time.Now()},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer ts.Close()
	t.Setenv("UPDATE_MANIFEST_URL", ts.URL)

	origVersion := updater.Version
	updater.Version = "v1.0.0"
	defer func() { updater.Version = origVersion }()

	req := httptest.NewRequest(http.MethodGet, "/admin/update/check?channel=stable", nil)
	rr := httptest.NewRecorder()
	server.handleUpdateCheck(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
	}
	var resp UpdateCheckResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.UpdateAvailable {
		t.Error("UpdateAvailable = false, want true")
	}
	if resp.LatestVersion != "v1.2.0" {
		t.Errorf("LatestVersion = %q, want v1.2.0", resp.LatestVersion)
	}
}

func TestHandleUpdateCheck_BadMethod(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/admin/update/check", nil)
	rr := httptest.NewRecorder()
	server.handleUpdateCheck(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleUpdateApply_NoUpdate(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]updater.Release{}) // empty manifest
	}))
	defer ts.Close()
	t.Setenv("UPDATE_MANIFEST_URL", ts.URL)

	req := httptest.NewRequest(http.MethodPost, "/admin/update/apply",
		strings.NewReader(`{"channel":"stable"}`))
	rr := httptest.NewRecorder()
	server.handleUpdateApply(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
}

func TestHandleUpdateApply_StartsJob(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Manifest with a release whose asset will be unreachable / mismatched.
	// We want apply to RETURN 202 with a job ID; the goroutine will fail
	// fast at either pickAsset (no OS/arch match) or download (connection
	// refused). Either way, the job reaches a terminal Failed state.
	manifest := []updater.Release{
		{
			Version: "v1.2.0", Channel: "stable", ReleaseDate: time.Now(),
			Assets: []updater.Asset{
				{
					Name:     "graphdb",
					OS:       "alien-os",
					Arch:     "alien-arch",
					URL:      "http://127.0.0.1:1/graphdb",
					Checksum: "00",
				},
			},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer ts.Close()
	t.Setenv("UPDATE_MANIFEST_URL", ts.URL)

	origVersion := updater.Version
	updater.Version = "v1.0.0"
	defer func() { updater.Version = origVersion }()

	req := httptest.NewRequest(http.MethodPost, "/admin/update/apply",
		strings.NewReader(`{"channel":"stable"}`))
	rr := httptest.NewRecorder()
	server.handleUpdateApply(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rr.Code, rr.Body.String())
	}
	var resp UpdateApplyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.JobID == "" {
		t.Error("JobID empty")
	}
	if resp.TargetVersion != "v1.2.0" {
		t.Errorf("TargetVersion = %q, want v1.2.0", resp.TargetVersion)
	}

	// Poll job until terminal. The goroutine will fail fast (no matching
	// asset for the runtime), so 2s is plenty of slack.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := server.updateJobs.get(resp.JobID)
		if !ok {
			t.Fatal("job not found in manager after 202")
		}
		if got.Status != UpdateJobRunning {
			if got.Status != UpdateJobFailed {
				t.Errorf("terminal status = %s, want failed (no matching asset)", got.Status)
			}
			if got.Error == "" {
				t.Error("Error empty on Failed job")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("job did not reach terminal state within 2s")
}

func TestHandleUpdateJob(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/update/jobs/nonexistent", nil)
		rr := httptest.NewRecorder()
		server.handleUpdateJob(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rr.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/update/jobs/", nil)
		rr := httptest.NewRecorder()
		server.handleUpdateJob(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rr.Code)
		}
	})

	t.Run("returns running job", func(t *testing.T) {
		started := server.updateJobs.start("v1.2.0")
		req := httptest.NewRequest(http.MethodGet, "/admin/update/jobs/"+started.ID, nil)
		rr := httptest.NewRecorder()
		server.handleUpdateJob(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
		}
		var got UpdateJob
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.ID != started.ID {
			t.Errorf("ID = %q, want %q", got.ID, started.ID)
		}
		if got.Status != UpdateJobRunning {
			t.Errorf("Status = %s, want running", got.Status)
		}
	})

	t.Run("returns completed job", func(t *testing.T) {
		started := server.updateJobs.start("v1.3.0")
		server.updateJobs.complete(started.ID, fmt.Errorf("simulated failure"))

		req := httptest.NewRequest(http.MethodGet, "/admin/update/jobs/"+started.ID, nil)
		rr := httptest.NewRecorder()
		server.handleUpdateJob(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rr.Code, rr.Body.String())
		}
		var got UpdateJob
		if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Status != UpdateJobFailed {
			t.Errorf("Status = %s, want failed", got.Status)
		}
		if got.Error != "simulated failure" {
			t.Errorf("Error = %q, want simulated failure", got.Error)
		}
		if got.CompletedAt == nil {
			t.Error("CompletedAt nil on terminal state")
		}
	})
}
