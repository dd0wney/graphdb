package wal

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// errInjected is returned by faultyFile for every simulated I/O failure.
var errInjected = errors.New("injected I/O error")

// faultMode selects when an injected operation fails.
type faultMode int

const (
	faultNever  faultMode = iota // never fail
	faultOnce                    // fail the next call, then behave normally
	faultAlways                  // fail every call from now on
)

// faultyFile wraps a real *os.File and fails Write, Sync or Close on demand.
//
// Read and Seek always delegate, so a WAL under fault injection still replays
// whatever reached the disk. That matters: the interesting bugs are the ones
// where a write half-succeeds and recovery has to cope.
//
// This is graphdb's analogue of SQLite's I/O-error VFS. SQLite fails the Nth
// I/O operation and then re-runs the suite with N incremented; the same shape
// is available here by setting failAfter.
type faultyFile struct {
	*os.File

	writeMode faultMode
	syncMode  faultMode
	closeMode faultMode

	// failAfter delays injection until this many writes have succeeded.
	// Zero fails the first write.
	failAfter int

	writes int
	syncs  int
	// closes counts calls that reached the real file. A leaked descriptor
	// shows up as closes == 0 after WAL.Close returned.
	closes int
}

func (f *faultyFile) Write(p []byte) (int, error) {
	f.writes++
	if f.shouldFail(&f.writeMode) && f.writes > f.failAfter {
		return 0, errInjected
	}
	return f.File.Write(p)
}

func (f *faultyFile) Sync() error {
	f.syncs++
	if f.shouldFail(&f.syncMode) {
		return errInjected
	}
	return f.File.Sync()
}

func (f *faultyFile) Close() error {
	if f.shouldFail(&f.closeMode) {
		return errInjected
	}
	f.closes++
	return f.File.Close()
}

// shouldFail reports whether this call fails, and consumes a faultOnce.
func (f *faultyFile) shouldFail(mode *faultMode) bool {
	switch *mode {
	case faultAlways:
		return true
	case faultOnce:
		*mode = faultNever
		return true
	default:
		return false
	}
}

// newFaultyWAL opens a real WAL, then swaps its file handle for a faultyFile
// wrapping the same descriptor. The returned WAL writes to a real file on
// disk; only the injected operations diverge.
func newFaultyWAL(t *testing.T) (*WAL, *faultyFile) {
	t.Helper()

	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	real, ok := w.file.(*os.File)
	if !ok {
		t.Fatalf("expected WAL to hold an *os.File, got %T", w.file)
	}

	faulty := &faultyFile{File: real}
	w.file = faulty
	w.writer = bufio.NewWriter(faulty)

	return w, faulty
}

// TestWAL_AppendSurfacesSyncError guards the durability contract: a caller that
// gets a nil error from Append must be able to assume the entry is on stable
// storage. If fsync fails, Append must say so.
func TestWAL_AppendSurfacesSyncError(t *testing.T) {
	w, faulty := newFaultyWAL(t)
	defer func() {
		faulty.syncMode = faultNever
		_ = w.Close()
	}()

	faulty.syncMode = faultAlways

	if _, err := w.Append(OpCreateNode, []byte("payload")); err == nil {
		t.Fatal("Append returned nil error, want the injected sync failure")
	}
}

// TestWAL_AppendSurfacesWriteError is the same contract for the write itself.
func TestWAL_AppendSurfacesWriteError(t *testing.T) {
	w, faulty := newFaultyWAL(t)
	defer func() {
		faulty.writeMode = faultNever
		faulty.syncMode = faultNever
		_ = w.Close()
	}()

	faulty.writeMode = faultAlways

	if _, err := w.Append(OpCreateNode, []byte("payload")); err == nil {
		t.Fatal("Append returned nil error, want the injected write failure")
	}
}

// TestWAL_ReplayStopsAtTornEntryAndKeepsPriorEntries documents what a mid-write
// I/O failure costs on recovery. Entries written before the fault must survive.
func TestWAL_ReplayStopsAtTornEntryAndKeepsPriorEntries(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	const goodEntries = 3
	for i := 0; i < goodEntries; i++ {
		if _, err := w.Append(OpCreateNode, []byte("durable")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Truncate the last byte to simulate a write torn by power loss.
	walPath := filepath.Join(dir, "wal.log")
	info, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := os.Truncate(walPath, info.Size()-1); err != nil {
		t.Fatalf("Truncate: %v", err)
	}

	reopened, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("reopen after torn write: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	entries, err := reopened.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after torn write: %v", err)
	}
	if len(entries) != goodEntries-1 {
		t.Errorf("recovered %d entries, want %d (the torn one must be dropped, the rest kept)",
			len(entries), goodEntries-1)
	}
}
