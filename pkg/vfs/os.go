package vfs

import "os"

// osFS is the default driver. It forwards to the os package with no behaviour
// of its own, so installing a different driver is the only way graphdb's file
// behaviour can change.
type osFS struct{}

// OS returns the filesystem driver graphdb uses unless told otherwise.
func OS() FileSystem { return osFS{} }

func (osFS) Open(name string, flag int, perm os.FileMode) (File, error) {
	f, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (osFS) Remove(name string) error                     { return os.Remove(name) }
func (osFS) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }
func (osFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (osFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFS) Name() string                                 { return "os" }

// *os.File already satisfies File. This fails to compile if the interface ever
// grows a method os.File does not have, which is the signal that the new method
// needs a considered default rather than a silent one.
var _ File = (*os.File)(nil)
