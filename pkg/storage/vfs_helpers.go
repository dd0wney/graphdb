package storage

// Small adapters for the two os package helpers that vfs.FileSystem does not
// carry. FileSystem's method set is deliberately the intersection of what
// callers use (see the pkg/vfs doc comment), so whole-file read and write are
// composed here from Open rather than added to the published interface.

import (
	"io"
	"os"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// writeFileWithFS is os.WriteFile through a driver.
//
// It does not Sync, matching os.WriteFile exactly. Every current caller writes
// a temp file and then renames it, so the durability of the rename is what
// matters; adding a Sync here would be a behaviour change, not a refactor.
func writeFileWithFS(fs vfs.FileSystem, path string, data []byte, perm os.FileMode) error {
	f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// readFileWithFS is os.ReadFile through a driver. A missing file yields an
// error that satisfies os.IsNotExist, which the snapshot loader depends on to
// tell "no snapshot yet" from a real failure.
func readFileWithFS(fs vfs.FileSystem, path string) ([]byte, error) {
	f, err := fs.Open(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
