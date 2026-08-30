//go:build !unix

package vfs

const dirSyncSupported = false

// syncDir does nothing on a platform that cannot sync a directory handle.
//
// Windows is the case that matters. A directory there is not a file handle a
// program can flush the way POSIX allows, so there is no operation to call.
// plan9 and js/wasm have none either.
//
// Returning nil rather than an error is deliberate. An error would make every
// snapshot publish and every WAL rotation fail on those platforms, which
// trades a crash-window defect for a total outage. The cost is stated rather
// than hidden: on such a platform the rename stays atomic, but the durability
// of the new directory entry is whatever the filesystem gives on its own, and
// graphdb adds nothing. DirSyncSupported is false here, so a test that asserts
// the sync happened skips instead of failing.
func syncDir(FileSystem, string) error { return nil }
