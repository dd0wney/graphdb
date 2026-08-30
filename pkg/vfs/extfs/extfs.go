// Package extfs adapts an EXTERNAL filesystem — one graphdb does not own and
// does not import — onto pkg/vfs.FileSystem, so a third-party fault-injection
// or crash-simulation library can drive graphdb's real write paths.
//
// # Why the interface is declared here rather than imported
//
// Go interfaces are structural, so the external library never learns this
// package exists. It satisfies FS by shape. The dependency arrow therefore
// points from the external library to graphdb, never the other way, and
// graphdb gains no dependency for a capability only its tests use.
//
// The first consumer this was written for is a crash-simulation library whose
// own FS interface is exactly the shape below. If a future consumer's shape
// differs, the break is a compile error at that consumer's call site, which is
// the loud direction.
//
// # Why Seek, ReadAt and WriteAt refuse instead of approximating
//
// pkg/vfs.File carries io.Seeker, ReadAt and WriteAt. A recorder that tracks a
// handle's offset by ADDITION — starting at zero and advancing by the bytes
// each read and write carries — is correct only for a filesystem that cannot
// seek. graphdb does seek: pkg/lsm does it at five production sites, including
// the SSTable header backpatch, which seeks to 0 and rewrites the header after
// the body is written.
//
// If this adapter approximated those calls, a recorder would place that
// backpatch at the END of the file. Every state it then generated for the LSM
// paths would be fiction, and the defects it reported would be ones graphdb
// cannot have. That is worse than no tool: a reader who cannot act on a
// finding learns to skip the next one.
//
// So they return ErrUnsupported, naming the operation. The failure lands at the
// exact call site that needs support, and converts an invisible wrong answer
// into a visible missing feature.
//
// # What graphdb currently needs, measured rather than assumed
//
// Every write path needs at least one operation this interface lacks. Counted
// on 62e53c0, and each is pinned by a test in this package so the numbers
// cannot rot:
//
//   - pkg/storage: ONE. mmap_snapshot_writer.go:184 backpatches the header with
//     WriteAt(hdr, 0) after writing the body.
//   - pkg/wal: TWO Seeks. wal.go:138 rewinds to read the log, wal.go:217 seeks
//     to the end to append.
//   - pkg/lsm: NINETEEN positional calls.
//
// So an external library gains graphdb's snapshot publish by adding WriteAt,
// and its WAL paths by adding Seek. Three calls reach both durability-critical
// paths; pkg/lsm is a much larger job.
//
// An earlier version of this comment said these paths "do not seek, so they run
// through this adapter unmodified". That was wrong, and the adapter itself
// disproved it on its first run — which is the behaviour it exists to have.
package extfs

import (
	"errors"
	"fmt"
	"os"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// FS is the filesystem an external library must provide. It is deliberately
// the smallest set graphdb's storage and WAL paths use, so that a library
// which cannot seek can still drive them.
type FS interface {
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	Remove(name string) error
	Rename(oldpath, newpath string) error
	Stat(name string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(name string) ([]os.DirEntry, error)
}

// File is the handle an external library must provide. Note what is absent:
// Seek, ReadAt, WriteAt and Name. The adapter supplies Name from the path the
// file was opened with, and refuses the other three. See the package comment.
type File interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
	Sync() error
	Truncate(size int64) error
	Close() error
}

// ErrUnsupported reports an operation the external filesystem cannot express.
//
// A caller that sees this has reached a graphdb path the external library
// cannot model correctly. It is not a defect in graphdb and it is not a defect
// in the library — it is the boundary between them, reported at the call site
// rather than hidden behind a wrong answer.
var ErrUnsupported = errors.New("extfs: the external filesystem cannot perform this operation")

type adapter struct {
	fs   FS
	name string
}

// New wraps an external filesystem as a pkg/vfs.FileSystem.
//
// name is what vfs.FileSystem.Name reports, and it appears in graphdb's
// diagnostics. Give it something identifying, so a failure says which driver
// produced it.
func New(fs FS, name string) vfs.FileSystem {
	if name == "" {
		name = "extfs"
	}
	return &adapter{fs: fs, name: name}
}

func (a *adapter) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f, err := a.fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &file{File: f, name: name}, nil
}

func (a *adapter) Remove(name string) error                     { return a.fs.Remove(name) }
func (a *adapter) Rename(oldpath, newpath string) error         { return a.fs.Rename(oldpath, newpath) }
func (a *adapter) Stat(name string) (os.FileInfo, error)        { return a.fs.Stat(name) }
func (a *adapter) MkdirAll(path string, perm os.FileMode) error { return a.fs.MkdirAll(path, perm) }
func (a *adapter) ReadDir(name string) ([]os.DirEntry, error)   { return a.fs.ReadDir(name) }
func (a *adapter) Name() string                                 { return a.name }

type file struct {
	File
	name string
}

func (f *file) Name() string { return f.name }

// Seek, ReadAt and WriteAt refuse. See the package comment for why an
// approximation here would make a recorder produce fiction.

func (f *file) Seek(offset int64, whence int) (int64, error) {
	return 0, fmt.Errorf("Seek(%d, %d) on %q: %w", offset, whence, f.name, ErrUnsupported)
}

func (f *file) ReadAt(p []byte, off int64) (int, error) {
	return 0, fmt.Errorf("ReadAt(%d bytes, offset %d) on %q: %w", len(p), off, f.name, ErrUnsupported)
}

func (f *file) WriteAt(p []byte, off int64) (int, error) {
	return 0, fmt.Errorf("WriteAt(%d bytes, offset %d) on %q: %w", len(p), off, f.name, ErrUnsupported)
}

// The adapter must satisfy the interface it claims to. A method added to
// pkg/vfs breaks this line rather than breaking a consumer at run time.
var _ vfs.FileSystem = (*adapter)(nil)
var _ vfs.File = (*file)(nil)
