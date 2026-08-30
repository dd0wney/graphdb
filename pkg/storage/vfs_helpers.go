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

// writeFileWithFS is os.WriteFile through a driver, plus a Sync.
//
// It SYNCS before Close, which os.WriteFile does not. POSIX does not make
// close(2) flush, so without it a caller's subsequent rename can publish a name
// over bytes that never reached the platter.
//
// This comment previously said the opposite: "it does not Sync ... the
// durability of the rename is what matters; adding a Sync here would be a
// behaviour change, not a refactor". Both halves were wrong. The rename was not
// durable either — nothing synced a parent directory anywhere in this
// repository until PR #530 — so the reasoning rested on a property no code
// provided. And a durable name over unsynced bytes is not durable data: the
// rename and the contents are two questions, not one.
//
// A crash sweep by the github.com/dd0wney/fault session generated the states a
// power cut can leave on this path. On main at 8a0e5e1, 20 of 38 broke, and 9
// of those left snapshot.json EMPTY so the store would not reopen at all. The
// same harness against this fix reports 16 states and 0 failures. See
// durable_json_publish_test.go.
//
// "A behaviour change, not a refactor" was true and overstated: there is
// exactly one caller, and it is the snapshot publish, which wants the Sync.
func writeFileWithFS(fs vfs.FileSystem, path string, data []byte, perm os.FileMode) error {
	f, err := fs.Open(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
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
