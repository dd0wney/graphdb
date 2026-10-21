//go:build unix

package vfs

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Lock implements Locker for the OS driver with flock(LOCK_EX|LOCK_NB).
//
// flock, not fcntl: an fcntl lock belongs to the process, so closing ANY
// descriptor on the file in that process drops it, and graphdb opens files in
// the data directory by path in several places. An flock lock belongs to this
// open file description, and the kernel drops it when the holder dies, so a
// crash never leaves a stale lock the way a PID file does.
func (osFS) Lock(name string) (func() error, error) {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("flock %s: %w", name, err)
	}
	// Closing the descriptor drops the lock; no LOCK_UN first, because a close
	// that fails after an unlock that succeeded would report a lock still held.
	return f.Close, nil
}
