package api

import (
	"io"

	"github.com/dd0wney/graphdb/pkg/backup"
)

// writeBackupArchive streams a gzip+tar backup of the store's dataDir to w.
// The archive logic lives in pkg/backup so offline tooling can reuse it without
// importing the HTTP server; this thin wrapper keeps the handler call site
// unchanged.
func writeBackupArchive(w io.Writer, dataDir, version string) error {
	return backup.WriteArchive(w, dataDir, version)
}
