// Package vfstest provides filesystem drivers that misbehave on purpose.
//
// These are graphdb's equivalent of the wonky VFS implementations SQLite writes
// for its I/O-error, crash and power-loss suites. They are ordinary code in an
// ordinary package rather than _test.go helpers, for two reasons:
//
//   - Any package's tests can install one, through the same published registry
//     a production caller would use. The code under test is therefore the code
//     that ships.
//   - A downstream consumer can inject faults into its own use of graphdb,
//     which a test-only seam can never offer.
//
// Nothing here is imported by graphdb's production code. Importing this package
// into a server binary would be a mistake, not a feature.
package vfstest

import (
	"fmt"
	"os"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// ErrInjected is returned by every simulated failure. Tests match on it with
// errors.Is rather than on a message.
var ErrInjected = fmt.Errorf("vfstest: injected I/O error")

// Mode selects when an armed fault fires.
type Mode int

const (
	// Off means the operation behaves normally.
	Off Mode = iota
	// Once fails the next matching call, then disarms itself. This is SQLite's
	// "fail just once" mode.
	Once
	// Always fails every matching call from now on. SQLite's "fail
	// continuously after the first failure".
	Always
)

// FaultFS wraps a filesystem and fails operations on demand.
//
// The zero behaviour is transparent: until a fault is armed, every call
// forwards to the base filesystem unchanged. Arm faults with the FailX methods,
// which are safe to call while the store is open.
type FaultFS struct {
	base vfs.FileSystem
	name string

	mu sync.Mutex
	// openMode fires on Open, which is the fault a store meets at startup.
	openMode Mode
	// writeMode and syncMode fire on the corresponding File methods.
	writeMode Mode
	syncMode  Mode
	closeMode Mode
	// writeAfter delays a write fault until this many writes have succeeded,
	// which is how SQLite walks the point of failure through a run.
	writeAfter int
	writes     int
	syncs      int
}

// NewFaults wraps base. Pass vfs.OS() for a driver that touches a real disk, or
// another driver to stack behaviours.
func NewFaults(base vfs.FileSystem, name string) *FaultFS {
	return &FaultFS{base: base, name: name}
}

// FailOpen arms a fault on Open.
func (f *FaultFS) FailOpen(m Mode) { f.mu.Lock(); f.openMode = m; f.mu.Unlock() }

// FailWrite arms a fault on Write, after `after` writes have succeeded.
func (f *FaultFS) FailWrite(m Mode, after int) {
	f.mu.Lock()
	f.writeMode, f.writeAfter, f.writes = m, after, 0
	f.mu.Unlock()
}

// FailSync arms a fault on Sync. This is the one that matters most: a store
// that reports a successful commit after a failed fsync has lied about
// durability.
func (f *FaultFS) FailSync(m Mode) { f.mu.Lock(); f.syncMode = m; f.mu.Unlock() }

// FailClose arms a fault on Close.
func (f *FaultFS) FailClose(m Mode) { f.mu.Lock(); f.closeMode = m; f.mu.Unlock() }

// Clear disarms every fault.
func (f *FaultFS) Clear() {
	f.mu.Lock()
	f.openMode, f.writeMode, f.syncMode, f.closeMode = Off, Off, Off, Off
	f.writes, f.writeAfter = 0, 0
	f.mu.Unlock()
}

// Writes and Syncs report how many of each reached the base filesystem. A test
// that asserts a fault fired should also assert the operation was attempted,
// or it cannot tell a fired fault from a code path that never ran.
func (f *FaultFS) Writes() int { f.mu.Lock(); defer f.mu.Unlock(); return f.writes }
func (f *FaultFS) Syncs() int  { f.mu.Lock(); defer f.mu.Unlock(); return f.syncs }

// fire reports whether an armed mode fires now, consuming a Once.
func fire(m *Mode) bool {
	switch *m {
	case Always:
		return true
	case Once:
		*m = Off
		return true
	default:
		return false
	}
}

func (f *FaultFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f.mu.Lock()
	shouldFail := fire(&f.openMode)
	f.mu.Unlock()
	if shouldFail {
		return nil, fmt.Errorf("open %s: %w", name, ErrInjected)
	}
	inner, err := f.base.Open(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: inner, fs: f}, nil
}

func (f *FaultFS) Remove(name string) error               { return f.base.Remove(name) }
func (f *FaultFS) Rename(old, new string) error           { return f.base.Rename(old, new) }
func (f *FaultFS) Stat(name string) (os.FileInfo, error)  { return f.base.Stat(name) }
func (f *FaultFS) MkdirAll(p string, m os.FileMode) error { return f.base.MkdirAll(p, m) }
func (f *FaultFS) Name() string                           { return f.name }

type faultFile struct {
	vfs.File
	fs *FaultFS
}

func (ff *faultFile) Write(p []byte) (int, error) {
	ff.fs.mu.Lock()
	ff.fs.writes++
	shouldFail := ff.fs.writes > ff.fs.writeAfter && fire(&ff.fs.writeMode)
	ff.fs.mu.Unlock()
	if shouldFail {
		return 0, fmt.Errorf("write %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Write(p)
}

func (ff *faultFile) Sync() error {
	ff.fs.mu.Lock()
	ff.fs.syncs++
	shouldFail := fire(&ff.fs.syncMode)
	ff.fs.mu.Unlock()
	if shouldFail {
		return fmt.Errorf("sync %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Sync()
}

func (ff *faultFile) Close() error {
	ff.fs.mu.Lock()
	shouldFail := fire(&ff.fs.closeMode)
	ff.fs.mu.Unlock()
	if shouldFail {
		// The descriptor is still released. A fault driver that leaked one
		// would make every test using it leak, and the leak would be blamed on
		// the code under test.
		_ = ff.File.Close()
		return fmt.Errorf("close %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Close()
}
