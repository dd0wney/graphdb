//go:build unix

package storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// Two stores on one data directory each append to the WAL and each write the
// snapshot on Close, so one silently discards the other's writes. The lock on
// <DataDir>/LOCK refuses the second open (design:
// docs/internals/design/DESIGN_datadir_flock_2026-10-22.md).

const lockHolderEnv = "GRAPHDB_TEST_LOCK_HOLDER_DIR"

func openLockTestStore(t *testing.T, dir string) (*GraphStorage, error) {
	t.Helper()
	return NewGraphStorageWithConfig(DefaultStorageConfig(dir))
}

func wantDataDirLocked(t *testing.T, err error, dir string) {
	t.Helper()
	if !errors.Is(err, ErrDataDirLocked) {
		t.Fatalf("open err = %v, want ErrDataDirLocked", err)
	}
	var lerr *DataDirLockedError
	if !errors.As(err, &lerr) || lerr.Dir != dir {
		t.Fatalf("open err = %#v, want *DataDirLockedError naming %s", err, dir)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("message %q does not name the directory", err.Error())
	}
}

func TestDataDirLock_SecondOpenInProcessRefuses(t *testing.T) {
	dir := t.TempDir()
	first, err := openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := openLockTestStore(t, dir)
	if err == nil {
		_ = second.Close()
	}
	wantDataDirLocked(t, err, dir)
}

func TestDataDirLock_ReopenAfterCloseSucceeds(t *testing.T) {
	dir := t.TempDir()
	first, err := openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// A failed open must not keep the lock: the next open, once the cause is
// fixed, would otherwise refuse until the process exits.
func TestDataDirLock_FailedOpenReleasesTheLock(t *testing.T) {
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rules, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed rules.json: %v", err)
	}
	gs, err := openLockTestStore(t, dir)
	if err == nil {
		_ = gs.Close()
		t.Fatal("open with a corrupt rules.json succeeded; the test needs a failing open")
	}
	if errors.Is(err, ErrDataDirLocked) {
		t.Fatalf("open err = %v, want the rules.json failure", err)
	}

	if err := os.Remove(rules); err != nil {
		t.Fatalf("repair: %v", err)
	}
	gs, err = openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("open after a failed open: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// Closing a different descriptor on LOCK in this process must not drop the
// lock. A POSIX fcntl lock would be dropped here; flock is not.
func TestDataDirLock_ClosingAnotherDescriptorKeepsTheLock(t *testing.T) {
	dir := t.TempDir()
	first, err := openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	f, err := os.Open(filepath.Join(dir, "LOCK"))
	if err != nil {
		t.Fatalf("open LOCK: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close LOCK: %v", err)
	}

	second, err := openLockTestStore(t, dir)
	if err == nil {
		_ = second.Close()
	}
	wantDataDirLocked(t, err, dir)
}

// A driver with no Locker has no second process to exclude, so it gets no
// lock. The crash-sweep drivers depend on this: they reopen one directory in
// one process.
func TestDataDirLock_DriverWithoutLockerTakesNoLock(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultStorageConfig(dir)
	cfg.FS = vfstest.NewHandles(vfs.OS())
	first, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("second open through a driver with no Locker: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
}

// TestDataDirLock_HolderProcess is not a test on its own. The cross-process
// test runs the test binary again with lockHolderEnv set, and this function
// is the child: it opens the directory, reports that it holds it, and waits
// to be killed.
func TestDataDirLock_HolderProcess(t *testing.T) {
	dir := os.Getenv(lockHolderEnv)
	if dir == "" {
		t.Skip("child process for TestDataDirLock_OtherProcess")
	}
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(dir))
	if err != nil {
		os.Stdout.WriteString("open failed: " + err.Error() + "\n")
		os.Exit(2)
	}
	_ = gs
	os.Stdout.WriteString("held\n")
	time.Sleep(time.Minute)
	os.Exit(3)
}

// Another process holds the directory: the open refuses. When that process
// dies without Close, the kernel drops the lock and the next open succeeds —
// the crash case a PID file gets wrong.
func TestDataDirLock_OtherProcess(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDataDirLock_HolderProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), lockHolderEnv+"="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	ready := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if line := sc.Text(); line == "held" || strings.HasPrefix(line, "open failed") {
				ready <- line
				return
			}
		}
		ready <- fmt.Sprintf("holder exited before it held the directory (scan err %v)", sc.Err())
	}()
	select {
	case line := <-ready:
		if line != "held" {
			t.Fatalf("holder: %s", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("holder did not report within 30s")
	}

	gs, err := openLockTestStore(t, dir)
	if err == nil {
		_ = gs.Close()
	}
	wantDataDirLocked(t, err, dir)

	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_ = cmd.Wait()

	gs, err = openLockTestStore(t, dir)
	if err != nil {
		t.Fatalf("open after the holder was killed: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
