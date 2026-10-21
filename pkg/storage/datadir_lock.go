package storage

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// dataDirLockFile is never deleted. Unlinking a lock file races: a process
// that opened the old inode keeps its lock while a third creates a new file
// and locks that, and both believe they hold the directory. pkg/backup
// archives a fixed list of names, so LOCK is never part of a backup.
const dataDirLockFile = "LOCK"

// lockDataDir takes the inter-process lock on dir and records how to release
// it. A driver with no Locker, or a platform without flock, has no second
// process to exclude, so it opens without a lock. See
// docs/internals/design/DESIGN_datadir_flock_2026-10-22.md.
func (gs *GraphStorage) lockDataDir(dir string) error {
	release, err := vfs.LockFile(gs.fs, filepath.Join(dir, dataDirLockFile))
	switch {
	case err == nil:
		gs.releaseDataDirLock = release
		return nil
	case errors.Is(err, vfs.ErrLocked):
		return &DataDirLockedError{Dir: dir}
	case errors.Is(err, vfs.ErrLockUnsupported):
		return nil
	default:
		return fmt.Errorf("lock data directory %s: %w", dir, err)
	}
}
