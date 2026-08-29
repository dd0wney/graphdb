package vfs

import (
	"errors"
	"io"
	"os"
)

// ErrMapUnsupported is returned by a Mapper that cannot map this file, or
// cannot map at all on this platform. MapFile treats it as "use the Open path
// instead", so it is a routing signal and not a failure.
var ErrMapUnsupported = errors.New("vfs: driver cannot map this file")

// Mapper is an OPTIONAL capability. A FileSystem may implement it to expose a
// file's whole contents as one byte slice.
//
// It is deliberately not a method on FileSystem. FileSystem is a published
// interface, and adding a method to it breaks every implementation outside this
// repository (see the Stability note on this package). An optional interface
// costs a driver that does not want it nothing.
//
// # Why graphdb needs it
//
// The mmap snapshot reader calls syscall.Mmap on a file descriptor. File has no
// Fd method and should not grow one: a fault driver has no descriptor to give,
// so an Fd-shaped seam would let the OS driver through and lock every other
// driver out — which is the same test-only seam this package exists to remove.
//
// Mapping is the operation the reader actually needs, so that is what the
// interface exposes. The OS driver satisfies it with mmap. A fault driver
// satisfies it by returning a buffer it chose, which is how a truncated or
// corrupt snapshot reaches the production read path without a corrupt file ever
// existing on disk. SQLite makes the same move with xFetch on sqlite3_io_methods.
type Mapper interface {
	// Map returns the file's entire contents and a function that releases them.
	//
	// The bytes stay valid until release is called, and the caller must not
	// retain them afterwards. Callers must call release exactly once, even
	// when they only read part of the data.
	//
	// A driver that cannot map returns ErrMapUnsupported, and the caller falls
	// back to reading the file through Open.
	Map(name string) (data []byte, release func() error, err error)
}

// MapFile returns the whole contents of name.
//
// It uses fs's Mapper when it has one, and reads the file through Open when it
// does not, so a driver written before Mapper existed keeps working. The
// returned release must be called exactly once.
func MapFile(fs FileSystem, name string) ([]byte, func() error, error) {
	if m, ok := fs.(Mapper); ok {
		data, release, err := m.Map(name)
		switch {
		case err == nil:
			return data, release, nil
		case errors.Is(err, ErrMapUnsupported):
			// Fall through: this driver cannot map, but it can still Open.
		default:
			return nil, nil, err
		}
	}
	return readWholeFile(fs, name)
}

// readWholeFile is the fallback path. It reads eagerly, where a real mapping is
// lazy, so a driver without Mapper pays the file's size in memory. That is
// acceptable for the fault and in-memory drivers this path exists to serve, and
// the OS driver never takes it on a platform with mmap.
func readWholeFile(fs FileSystem, name string) ([]byte, func() error, error) {
	f, err := fs.Open(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	return data, func() error { return nil }, nil
}
