// Package vfs is graphdb's filesystem driver interface.
//
// Every file operation graphdb performs goes through a FileSystem. The default
// is OS(), which calls the os package directly and is what ships. A caller may
// install a different one — to inject I/O errors, to simulate a power cut, to
// serve a store from memory — and graphdb's real code paths then run against
// it.
//
// # Why this exists in production code
//
// graphdb's earlier fault-injection seams lived in _test.go files: an
// unexported interface that only a test ever substituted. That tests a
// specially-constructed object rather than the artifact that ships, which is
// the distinction SQLite draws between its TCL suite ("a source code test, not
// an object code test, because it's not testing exactly the same code that
// you're delivering") and TH3. SQLite's answer is sqlite3_vfs_register: the OS
// interface is replaceable through published API present in every production
// build, so its I/O-error, crash and power-loss tests drive the shipped
// library. This package is the same idea.
//
// A second consequence matters as much: a downstream consumer can inject faults
// into ITS use of graphdb, which a test-only seam can never offer.
//
// # Stability
//
// FileSystem and File are a published interface. Adding a method to either is a
// breaking change for any implementation outside this repository, so treat them
// with the same care as the on-disk formats described in CLAUDE.md.
package vfs

import (
	"io"
	"os"
)

// FileSystem is the set of filesystem operations graphdb performs.
//
// An implementation must be safe for concurrent use: graphdb opens and syncs
// files from several goroutines.
type FileSystem interface {
	// Open opens a file with the semantics of os.OpenFile.
	Open(name string, flag int, perm os.FileMode) (File, error)

	// Remove deletes a file, with the semantics of os.Remove.
	Remove(name string) error

	// Rename moves a file, with the semantics of os.Rename. graphdb relies on
	// this being atomic on POSIX for snapshot and WAL rotation, so an
	// implementation that cannot offer atomicity must say so in its docs.
	Rename(oldpath, newpath string) error

	// Stat returns file metadata, with the semantics of os.Stat.
	Stat(name string) (os.FileInfo, error)

	// MkdirAll creates a directory tree, with the semantics of os.MkdirAll.
	MkdirAll(path string, perm os.FileMode) error

	// Name identifies the driver, for diagnostics and for tests that assert
	// which driver is installed. "os" is the default.
	Name() string
}

// File is an open file.
//
// The method set is the intersection of what pkg/wal, pkg/lsm, pkg/btree and
// pkg/storage actually use, deliberately: a wider interface is harder to
// implement correctly in a fault driver, and every method here is one a wonky
// implementation has to get right.
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer

	ReadAt(p []byte, off int64) (n int, err error)
	WriteAt(p []byte, off int64) (n int, err error)

	// Sync commits the file's contents to stable storage. A driver that
	// simulates a power cut discards everything not yet passed to Sync.
	Sync() error

	// Truncate changes the file's size.
	Truncate(size int64) error

	// Name returns the name the file was opened with.
	Name() string
}
