package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// BuildVersion is the graphdb build version. GET /health reports it and backup
// manifests record it. cmd/server sets it once at startup, before the server
// accepts a request; it stays "unknown" in tests and in builds that do not set
// it.
var BuildVersion = "unknown"

// countingWriter tallies the bytes written through it so the handler can
// report the produced archive size to metrics without buffering the stream.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// handleBackup streams a gzip+tar archive of the store (admin only). It first
// takes a live-safe Snapshot() to flush a consistent point, then streams the
// snapshot + wal/ + auth/ + lsa/ (+ edgestore/) with a manifest. The archive
// contains password hashes and all tenants' data — admin-gated; use over TLS.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	start := time.Now()
	if err := s.graph.Snapshot(); err != nil {
		s.recordBackup("error", 0, time.Since(start))
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "backup snapshot"))
		return
	}
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"graphdb-backup-%s.tar.gz\"", ts))
	// Status 200 is committed once we start writing; a mid-stream error can no
	// longer change it — the client detects truncation via gzip/tar EOF.
	cw := &countingWriter{w: w}
	result := "success"
	if err := writeBackupArchive(cw, s.dataDir, BuildVersion); err != nil {
		log.Printf("ERROR [backup stream]: %v", err)
		result = "error"
	}
	s.recordBackup(result, cw.n, time.Since(start))
}

// recordBackup records backup metrics if a registry is configured.
func (s *Server) recordBackup(result string, bytes int64, dur time.Duration) {
	if s.metricsRegistry == nil {
		return
	}
	s.metricsRegistry.RecordBackup(result, bytes, dur)
}
