//go:build unix

package vfs

import (
	"fmt"
	"os"
)

const dirSyncSupported = true

// syncDir opens the directory read-only and fsyncs the handle.
//
// POSIX allows fsync on a directory descriptor, and that is the only portable
// way to force the directory's own entries out to stable storage. The open
// goes through the driver, so a fault driver sees it and a test can fail it.
func syncDir(fsys FileSystem, dir string) error {
	d, err := fsys.Open(dir, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("vfs: open directory %s to sync it: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		// The handle is released whatever the sync reported. Returning here
		// with the descriptor still open would leak one per publish.
		_ = d.Close()
		return fmt.Errorf("vfs: sync directory %s: %w", dir, err)
	}
	if err := d.Close(); err != nil {
		return fmt.Errorf("vfs: close directory %s after syncing it: %w", dir, err)
	}
	return nil
}
