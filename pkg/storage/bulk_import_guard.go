package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// guardBulkImportWALReplayed refuses a BulkImportMode open when a WAL file in
// the data directory holds anything the loaded snapshot does not account for.
// Bulk mode opens no WAL, so it cannot replay one: the session would hand the
// entries' IDs out again and its Close would publish a snapshot without them,
// losing acknowledged writes with no error.
//
// Size alone cannot decide this, unlike guardWALBackendNotSwitched: Snapshot
// keeps the entries it covers, so a non-empty WAL can hold nothing to replay.
// LSNs only grow, so the highest one on disk decides. Both backend files are
// read, because the session that wrote them may have used either.
//
// Constructor-only; no locking. Must run after the snapshot load sets
// gs.snapshotBoundaryLSN.
func (gs *GraphStorage) guardBulkImportWALReplayed() error {
	walDir := filepath.Join(gs.dataDir, "wal")
	for _, file := range []struct {
		name       string
		compressed bool
	}{{"wal.log", false}, {"wal_compressed.log", true}} {
		path := filepath.Join(walDir, file.name)
		info, err := gs.fs.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			// A guard against silent loss cannot treat "could not look" as
			// "nothing there".
			return fmt.Errorf("bulk import mode: stat %q to check for unreplayed writes: %w", path, err)
		}
		if info.Size() == 0 {
			continue
		}

		highest, err := gs.highestWALLSN(walDir, file.compressed)
		if err != nil {
			return fmt.Errorf("bulk import mode: read %q to check for unreplayed writes: %w", path, err)
		}
		if highest > gs.snapshotBoundaryLSN {
			return fmt.Errorf(
				"%w: %q holds entries up to LSN %d and the snapshot covers LSN %d. "+
					"Open %q once without BulkImportMode and Close it, then reopen in bulk mode",
				ErrBulkImportUnreplayedWAL, path, highest, gs.snapshotBoundaryLSN, gs.dataDir)
		}
		// The normal open refuses this too (guardWALNotBehindSnapshot): the
		// WAL did not come from the session that wrote the snapshot, so its
		// entries may be newer writes under reused LSNs.
		if highest != 0 && highest < gs.snapshotBoundaryLSN {
			return fmt.Errorf("%w: bulk import mode: %q ends at LSN %d and the snapshot covers LSN %d",
				ErrWALBehindSnapshot, path, highest, gs.snapshotBoundaryLSN)
		}
	}
	return nil
}

// highestWALLSN opens a WAL file with the backend that wrote it, reads the LSN
// that backend recovers from disk, and closes it again. Neither constructor
// writes to an existing file, but both open it read-write, so a bulk open of a
// read-only data directory with a non-empty WAL fails here rather than
// skipping the check.
func (gs *GraphStorage) highestWALLSN(walDir string, compressed bool) (uint64, error) {
	if compressed {
		cw, err := wal.NewCompressedWALWithFS(walDir, gs.fs)
		if err != nil {
			return 0, err
		}
		lsn := cw.GetCurrentLSN()
		return lsn, cw.Close()
	}
	w, err := wal.NewWALWithFS(walDir, gs.fs)
	if err != nil {
		return 0, err
	}
	lsn := w.GetCurrentLSN()
	return lsn, w.Close()
}
