//go:build unix

package vfs

import (
	"fmt"
	"os"
	"syscall"
)

// Map implements Mapper for the OS driver with syscall.Mmap. This is the path
// that ships, and it is why the snapshot reader can serve a 937k-node store
// without materializing it.
func (osFS) Map(name string) ([]byte, func() error, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size := fi.Size()
	if size == 0 {
		// mmap of a zero-length file is EINVAL. An empty file is a legitimate
		// thing to read, so answer it directly rather than as an error.
		return []byte{}, func() error { return nil }, nil
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("mmap %q: %w", name, err)
	}
	return data, func() error { return syscall.Munmap(data) }, nil
}
