package api

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// BuildVersion is the graphdb build version recorded in backup manifests.
// cmd/server sets it from main.Version at startup; defaults to "unknown"
// (e.g. in tests or dev builds that skip ldflags injection).
var BuildVersion = "unknown"

// handleBackup streams a gzip+tar archive of the store (admin only). It first
// takes a live-safe Snapshot() to flush a consistent point, then streams the
// snapshot + wal/ + auth/ + lsa/ (+ edgestore/) with a manifest. The archive
// contains password hashes and all tenants' data — admin-gated; use over TLS.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.graph.Snapshot(); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "backup snapshot"))
		return
	}
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"graphdb-backup-%s.tar.gz\"", ts))
	// Status 200 is committed once we start writing; a mid-stream error can no
	// longer change it — the client detects truncation via gzip/tar EOF.
	if err := writeBackupArchive(w, s.dataDir, BuildVersion); err != nil {
		log.Printf("ERROR [backup stream]: %v", err)
	}
}
