package vfstest

import (
	"os"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// MapCounter wraps a FileSystem and counts every call to Map, and every call
// to the release function each Map call returns.
//
// It exists to test a CALLER's cleanup discipline, not a driver's. Map hands
// back a byte slice and a release function (vfs.Mapper), and the Go garbage
// collector does not know about the memory mapping behind it: a caller that
// returns on an error path without calling release leaks address space until
// the process exits. Nothing in a normal test run notices that, because the
// leaked bytes are never read again. MapCounter turns the missing call into a
// number a test can assert on — Maps() and Releases() must agree after every
// path a caller can take.
//
// ServePayload also makes this driver double as the seam a test uses to feed
// a chosen, possibly corrupt, snapshot straight into a reader — the same
// capability vfs.Mapper was added for: no corrupt file is ever written to
// disk. Without ServePayload, Map delegates to the wrapped FileSystem through
// vfs.MapFile, so a MapCounter placed over vfs.OS() counts real mappings.
type MapCounter struct {
	base vfs.FileSystem
	name string

	mu         sync.Mutex
	payload    []byte
	usePayload bool
	maps       int
	releases   int
}

// NewMapCounter wraps base. Pass vfs.OS() to count real mappings, or another
// driver to stack behaviours.
func NewMapCounter(base vfs.FileSystem) *MapCounter {
	return &MapCounter{base: base, name: "mapcounter"}
}

// ServePayload makes every later Map call return data directly, instead of
// delegating to base. Safe to call between Map calls on the same driver.
func (c *MapCounter) ServePayload(data []byte) {
	c.mu.Lock()
	c.payload, c.usePayload = data, true
	c.mu.Unlock()
}

// Map implements vfs.Mapper.
func (c *MapCounter) Map(name string) ([]byte, func() error, error) {
	c.mu.Lock()
	c.maps++
	usePayload, payload := c.usePayload, c.payload
	c.mu.Unlock()

	var (
		data        []byte
		baseRelease func() error
		err         error
	)
	if usePayload {
		data, baseRelease, err = payload, func() error { return nil }, nil
	} else {
		data, baseRelease, err = vfs.MapFile(c.base, name)
	}
	if err != nil {
		return nil, nil, err
	}

	return data, func() error {
		c.mu.Lock()
		c.releases++
		c.mu.Unlock()
		return baseRelease()
	}, nil
}

// Maps reports how many times Map was called.
func (c *MapCounter) Maps() int { c.mu.Lock(); defer c.mu.Unlock(); return c.maps }

// Releases reports how many times a release function this driver returned
// was actually invoked.
func (c *MapCounter) Releases() int { c.mu.Lock(); defer c.mu.Unlock(); return c.releases }

// Balanced reports whether every Map this driver served has been released
// exactly once. False also catches a double release — a genuine bug this
// driver happens to detect even though it is not the one it was built for.
func (c *MapCounter) Balanced() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.maps == c.releases }

func (c *MapCounter) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	return c.base.Open(name, flag, perm)
}
func (c *MapCounter) Remove(name string) error { return c.base.Remove(name) }
func (c *MapCounter) Rename(oldpath, newpath string) error {
	return c.base.Rename(oldpath, newpath)
}
func (c *MapCounter) Stat(name string) (os.FileInfo, error) { return c.base.Stat(name) }
func (c *MapCounter) MkdirAll(path string, perm os.FileMode) error {
	return c.base.MkdirAll(path, perm)
}
func (c *MapCounter) ReadDir(name string) ([]os.DirEntry, error) { return c.base.ReadDir(name) }
func (c *MapCounter) Name() string                               { return c.name }
