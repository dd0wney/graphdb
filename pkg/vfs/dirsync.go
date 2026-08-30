package vfs

import "path/filepath"

// SyncParentDir makes the directory entry for path durable.
//
// graphdb publishes durable state with write-temp, sync-file, rename. POSIX
// makes the rename atomic, so a reader sees the old name or the new one and
// never a half-written file. Atomic is not the same as durable. The new name
// is an entry in the parent directory, and syncing the FILE writes the file's
// data and inode, not the directory block that holds the name. A power cut
// after the rename can therefore lose the entry: the bytes are on the disk and
// nothing points at them.
//
// fsync on the parent directory is what publishes the name. Call this after
// every rename that publishes state, and after creating a file whose existence
// must survive a crash.
//
// Report the error. A caller that ignores it has the durability gap it had
// before the call, and no way to know.
func SyncParentDir(fsys FileSystem, path string) error {
	return syncDir(fsys, filepath.Dir(path))
}

// DirSyncSupported reports whether SyncParentDir does real work on this
// platform. It is false where a directory handle cannot be synced.
//
// A test that asserts a directory sync happened must skip when this is false.
// Without the constant such a test fails on that platform, and the failure
// says the code is wrong when the platform is simply different.
const DirSyncSupported = dirSyncSupported
