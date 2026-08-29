//go:build !unix

package vfs

// Map implements Mapper for the OS driver on platforms without syscall.Mmap.
// Returning ErrMapUnsupported routes MapFile to the Open path, so the caller
// gets the file's bytes either way.
func (osFS) Map(name string) ([]byte, func() error, error) {
	return nil, nil, ErrMapUnsupported
}
