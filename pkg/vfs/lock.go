package vfs

import "errors"

// ErrLocked is returned by a Locker when another holder has the lock.
var ErrLocked = errors.New("vfs: file is locked by another holder")

// ErrLockUnsupported is returned by a Locker that cannot lock on this platform,
// and by LockFile for a driver with no Locker. Like ErrMapUnsupported it is a
// routing signal, not a failure: the caller decides what no lock means.
var ErrLockUnsupported = errors.New("vfs: driver cannot lock files")

// Locker is an OPTIONAL capability: an exclusive, non-blocking lock on a file,
// which the operating system releases when the holder dies.
//
// It is optional for the reason Mapper is: FileSystem is a published
// interface, and a new method on it breaks every driver outside this
// repository. A fault or in-memory driver has no second process to exclude, so
// it has nothing to implement.
//
// # Why not File.Fd
//
// The OS driver needs a descriptor for flock, and File has no Fd method for
// the reason Mapper documents. Locking is the operation the caller needs, so
// that is what the interface exposes.
type Locker interface {
	// Lock takes the lock on name, creating the file if it does not exist. It
	// does not wait: a lock another holder has returns ErrLocked at once.
	// release drops the lock and must be called exactly once.
	Lock(name string) (release func() error, err error)
}

// LockFile locks name through fs's Locker. A driver without one returns
// ErrLockUnsupported.
func LockFile(fs FileSystem, name string) (func() error, error) {
	l, ok := fs.(Locker)
	if !ok {
		return nil, ErrLockUnsupported
	}
	return l.Lock(name)
}
