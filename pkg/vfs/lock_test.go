//go:build unix

package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockFile_OSDriverIsExclusiveUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "LOCK")

	release, err := LockFile(OS(), path)
	if err != nil {
		t.Fatalf("first Lock: %v", err)
	}
	if _, err := LockFile(OS(), path); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock err = %v, want ErrLocked", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	again, err := LockFile(OS(), path)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	if err := again(); err != nil {
		t.Fatalf("release again: %v", err)
	}
}

// flock belongs to the open file description, so closing a different
// descriptor on the same file in this process must not drop it. A POSIX fcntl
// lock would be dropped here, which is why the design rejects fcntl.
func TestLockFile_ClosingAnotherDescriptorKeepsTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "LOCK")

	release, err := LockFile(OS(), path)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	t.Cleanup(func() { _ = release() })

	other, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := LockFile(OS(), path); !errors.Is(err, ErrLocked) {
		t.Fatalf("Lock after closing another descriptor err = %v, want ErrLocked", err)
	}
}

func TestLockFile_DriverWithoutLockerIsUnsupported(t *testing.T) {
	var fs FileSystem = struct{ FileSystem }{OS()} // hides osFS's methods beyond FileSystem
	if _, err := LockFile(fs, filepath.Join(t.TempDir(), "LOCK")); !errors.Is(err, ErrLockUnsupported) {
		t.Fatalf("LockFile err = %v, want ErrLockUnsupported", err)
	}
}
