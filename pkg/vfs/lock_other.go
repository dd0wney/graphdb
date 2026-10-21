//go:build !unix

package vfs

// Lock implements Locker for the OS driver on platforms without flock. No
// release or CI job builds such a platform; ErrLockUnsupported lets the caller
// decide what no lock means rather than failing every open.
func (osFS) Lock(name string) (func() error, error) {
	return nil, ErrLockUnsupported
}
