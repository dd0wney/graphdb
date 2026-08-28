package vfstest

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// These drivers are instruments. An instrument nobody has calibrated reports
// numbers nobody should trust, so each test below checks that a driver both
// fires when armed AND stays silent when not.

func tempFile(t *testing.T, fs vfs.FileSystem, name string) vfs.File {
	t.Helper()
	f, err := fs.Open(filepath.Join(t.TempDir(), name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestFaultFS_TransparentUntilArmed(t *testing.T) {
	fs := NewFaults(vfs.OS(), "faults")
	f := tempFile(t, fs, "quiet")

	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("unarmed write failed: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("unarmed sync failed: %v", err)
	}
	if fs.Writes() != 1 || fs.Syncs() != 1 {
		t.Errorf("counters = writes %d syncs %d, want 1 and 1", fs.Writes(), fs.Syncs())
	}
}

func TestFaultFS_OnceFiresExactlyOnce(t *testing.T) {
	fs := NewFaults(vfs.OS(), "faults")
	f := tempFile(t, fs, "once")

	fs.FailWrite(Once, 0)
	if _, err := f.Write([]byte("a")); !errors.Is(err, ErrInjected) {
		t.Fatalf("first write err = %v, want ErrInjected", err)
	}
	if _, err := f.Write([]byte("b")); err != nil {
		t.Fatalf("second write should succeed after Once disarmed, got %v", err)
	}
}

func TestFaultFS_AlwaysKeepsFiring(t *testing.T) {
	fs := NewFaults(vfs.OS(), "faults")
	f := tempFile(t, fs, "always")

	fs.FailSync(Always)
	for i := 0; i < 3; i++ {
		if err := f.Sync(); !errors.Is(err, ErrInjected) {
			t.Fatalf("sync %d err = %v, want ErrInjected", i, err)
		}
	}
	fs.Clear()
	if err := f.Sync(); err != nil {
		t.Fatalf("sync after Clear failed: %v", err)
	}
}

// TestFaultFS_FailWriteAfterWalksThePoint is the mode SQLite uses to advance
// the point of failure through a run.
func TestFaultFS_FailWriteAfterWalksThePoint(t *testing.T) {
	fs := NewFaults(vfs.OS(), "faults")
	f := tempFile(t, fs, "after")

	fs.FailWrite(Always, 2) // the first two writes succeed
	for i := 0; i < 2; i++ {
		if _, err := f.Write([]byte("ok")); err != nil {
			t.Fatalf("write %d should have succeeded, got %v", i, err)
		}
	}
	if _, err := f.Write([]byte("boom")); !errors.Is(err, ErrInjected) {
		t.Fatalf("third write err = %v, want ErrInjected", err)
	}
}

func TestFaultFS_CloseStillReleasesTheDescriptor(t *testing.T) {
	dir := t.TempDir()
	fs := NewFaults(vfs.OS(), "faults")
	f, err := fs.Open(filepath.Join(dir, "closed"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	fs.FailClose(Once)
	if err := f.Close(); !errors.Is(err, ErrInjected) {
		t.Fatalf("Close err = %v, want ErrInjected", err)
	}
	// A second close on a released descriptor errors; on a leaked one it
	// succeeds. That difference is how the test can tell.
	if err := f.Close(); err == nil {
		t.Error("the driver leaked the descriptor: a second Close succeeded")
	}
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return b
}

func TestCrashFS_SyncedDataSurvivesAndUnsyncedDoesNot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal")
	fs := NewCrash(vfs.OS(), "crash", 1)

	f, err := fs.Open(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := f.Write([]byte("DURABLE")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := f.Write([]byte("VOLATILE")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	touched, err := fs.Crash()
	if err != nil {
		t.Fatalf("Crash: %v", err)
	}
	if touched != 1 {
		t.Fatalf("Crash touched %d files, want 1 — the crash reached nothing", touched)
	}

	got := readAll(t, path)
	if !bytes.Equal(got, []byte("DURABLE")) {
		t.Errorf("after the cut the file holds %q, want %q", got, "DURABLE")
	}
	_ = f.Close()
}

// TestCrashFS_CrashWithNothingUnsyncedReportsNothing is the negative control.
// Without it, a Crash that silently does nothing would look identical to one
// that worked.
func TestCrashFS_CrashWithNothingUnsyncedReportsNothing(t *testing.T) {
	dir := t.TempDir()
	fs := NewCrash(vfs.OS(), "crash", 1)

	f, err := fs.Open(filepath.Join(dir, "clean"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte("committed")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	touched, err := fs.Crash()
	if err != nil {
		t.Fatalf("Crash: %v", err)
	}
	if touched != 0 {
		t.Errorf("Crash touched %d files, want 0 — everything was synced", touched)
	}
}

// TestCrashFS_ReorderProducesAStateTruncationCannot is the point of the whole
// driver. A reordered crash must be able to leave a file whose tail survived
// while a span before it did not — a shape no truncation can produce, and the
// shape graphdb's crash tests had never seen.
func TestCrashFS_ReorderProducesAStateTruncationCannot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reorder")

	// Try several seeds: the policy is random by design, so one seed proving
	// nothing is expected. What must not happen is EVERY seed behaving like a
	// truncation.
	sawHole := false
	for seed := int64(0); seed < 40 && !sawHole; seed++ {
		if err := os.RemoveAll(path); err != nil {
			t.Fatalf("reset: %v", err)
		}
		fs := NewCrash(vfs.OS(), "crash", seed)
		fs.SetPolicy(ReorderAndLose)

		f, err := fs.Open(path, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		// A durable baseline of dots, then eight unsynced single-byte writes.
		if _, err := f.Write(bytes.Repeat([]byte("."), 8)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := f.Sync(); err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatalf("Seek: %v", err)
		}
		for i := 0; i < 8; i++ {
			if _, err := f.Write([]byte{byte('A' + i)}); err != nil {
				t.Fatalf("Write %d: %v", i, err)
			}
		}
		if _, err := fs.Crash(); err != nil {
			t.Fatalf("Crash: %v", err)
		}
		_ = f.Close()

		got := readAll(t, path)
		// A hole: some later byte landed while an earlier one did not.
		for i := 0; i < len(got)-1; i++ {
			if got[i] == '.' {
				for j := i + 1; j < len(got); j++ {
					if got[j] != '.' {
						sawHole = true
					}
				}
			}
		}
	}

	if !sawHole {
		t.Error("40 seeds of ReorderAndLose never produced a hole; the policy is behaving like a truncation")
	}
}

func TestRegistry_ResolveRefusesAnUnknownName(t *testing.T) {
	// A silent fallback here would run a test against the real filesystem while
	// the test believed a fault driver was installed — a pass that means
	// nothing.
	if _, err := vfs.Resolve("no-such-driver"); err == nil {
		t.Error("Resolve accepted an unknown driver name")
	}
	if fs, err := vfs.Resolve(""); err != nil || fs.Name() != "os" {
		t.Errorf("Resolve(\"\") = %v, %v; want the os driver", fs, err)
	}
}

func TestRegistry_RoundTripAndOsIsProtected(t *testing.T) {
	fs := NewFaults(vfs.OS(), "registry-case")
	if err := vfs.Register("registry-case", fs); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { vfs.Unregister("registry-case") })

	got, err := vfs.Resolve("registry-case")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name() != "registry-case" {
		t.Errorf("resolved driver is %q, want %q", got.Name(), "registry-case")
	}

	if err := vfs.Register("os", fs); err == nil {
		t.Error("Register replaced the os driver; every other test in the binary would inherit it")
	}
	if d, _ := vfs.Get("os"); d.Name() != "os" {
		t.Errorf("the os driver is now %q", d.Name())
	}
}
