package wal

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

// Crash and power-loss testing, SQLite technique 6. SQLite runs a VFS that
// "randomly reorders and corrupts the unsynchronized write operations to
// simulate the effect of buffered filesystems", and separately loops a snapshot
// point forward through the I/O sequence, damaging the file at each step
// (sqlite.org/testing.html §7).
//
// graphdb had three crash tests before this file, and all three close the store
// and reopen it. That exercises WAL replay on an intact file. It does not
// exercise recovery from a file that a power cut left half-written, which is
// the state a crash actually produces.
//
// The contract under test is the one Append states by construction: it writes,
// flushes and fsyncs before returning the LSN, so an Append that returned nil
// must survive a power cut. An Append that returned an error may or may not
// survive, but it must never cost an earlier one.

// crashFile models a buffered filesystem. Writes accumulate in memory and reach
// the disk only on Sync. crash() discards whatever has not been synced, which
// is what a power cut does to a page cache.
type crashFile struct {
	*os.File
	pending []byte
}

func (f *crashFile) Write(p []byte) (int, error) {
	f.pending = append(f.pending, p...)
	return len(p), nil
}

func (f *crashFile) Sync() error {
	if len(f.pending) > 0 {
		if _, err := f.File.Write(f.pending); err != nil {
			return err
		}
		f.pending = nil
	}
	return f.File.Sync()
}

// crash drops unsynced writes, the way a power cut does.
func (f *crashFile) crash() { f.pending = nil }

// TestWAL_PowerLossKeepsEverySyncedAppend is the durability contract. Append
// fsyncs before it returns, so every LSN it handed back must be readable after
// a power cut that discards unsynced data.
func TestWAL_PowerLossKeepsEverySyncedAppend(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	real, ok := w.file.(*os.File)
	if !ok {
		t.Fatalf("expected *os.File, got %T", w.file)
	}
	cf := &crashFile{File: real}
	w.file = cf
	w.writer = bufio.NewWriter(cf)

	const appends = 8
	for i := 0; i < appends; i++ {
		if _, err := w.Append(OpCreateNode, []byte("durable")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Power cut. Everything Append acknowledged was fsynced, so nothing should
	// be pending and nothing should be lost.
	cf.crash()

	reopened, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("reopen after power loss: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	entries, err := reopened.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != appends {
		t.Errorf("recovered %d entries, want %d — an acknowledged Append did not survive the power cut",
			len(entries), appends)
	}
}

// TestWAL_PowerLossDiscardsUnsyncedTailWithoutLosingHistory covers the other
// half: data written but never synced is allowed to vanish, and its loss must
// not damage the records that were synced before it.
func TestWAL_PowerLossDiscardsUnsyncedTailWithoutLosingHistory(t *testing.T) {
	dir := t.TempDir()

	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	real, ok := w.file.(*os.File)
	if !ok {
		t.Fatalf("expected *os.File, got %T", w.file)
	}
	cf := &crashFile{File: real}
	w.file = cf
	w.writer = bufio.NewWriter(cf)

	const durable = 4
	for i := 0; i < durable; i++ {
		if _, err := w.Append(OpCreateNode, []byte("durable")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// A record that reaches the buffer and the filesystem but is never synced:
	// the state a crash finds mid-batch.
	if err := w.writer.WriteByte(0x7F); err != nil {
		t.Fatalf("seed unsynced byte: %v", err)
	}
	if err := w.writer.Flush(); err != nil {
		t.Fatalf("flush to the crash file: %v", err)
	}

	cf.crash()

	reopened, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	entries, err := reopened.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != durable {
		t.Errorf("recovered %d entries, want the %d that were synced", len(entries), durable)
	}
}

// TestWAL_TornWriteSweep is SQLite's loop: advance the point of failure one byte
// at a time and require that recovery is sane at every one of them.
//
// A single truncation test proves one offset. The interesting offsets are the
// ones inside a length prefix or a checksum, and a single test picks those only
// by luck.
func TestWAL_TornWriteSweep(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}

	const good = 5
	for i := 0; i < good; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	walPath := filepath.Join(dir, "wal.log")
	intact, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Sweep every truncation point. At each one, recovery must return some
	// prefix of the original entries — never more, never a mangled entry, and
	// never a panic.
	for cut := 0; cut <= len(intact); cut++ {
		if err := os.WriteFile(walPath, intact[:cut], walFilePerm); err != nil {
			t.Fatalf("stage truncation at %d: %v", cut, err)
		}

		reopened, err := NewWAL(dir)
		if err != nil {
			// A refusal is acceptable; a panic is not, and the test would have
			// crashed already if one occurred.
			continue
		}
		entries, err := reopened.ReadAll()
		_ = reopened.Close()
		if err != nil {
			continue
		}
		if len(entries) > good {
			t.Fatalf("truncation at %d recovered %d entries, more than the %d ever written",
				cut, len(entries), good)
		}
		for i, e := range entries {
			if e.LSN != uint64(i+1) {
				t.Fatalf("truncation at %d: entry %d has LSN %d, want %d — recovery returned a non-prefix",
					cut, i, e.LSN, i+1)
			}
		}
	}
}

// TestWAL_HoleInTheMiddleDoesNotInventEntries models a partially written page
// rather than a short file: the tail arrived, a span in the middle did not.
// Truncation cannot produce this shape, and a buffered filesystem can.
func TestWAL_HoleInTheMiddleDoesNotInventEntries(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	const good = 6
	for i := 0; i < good; i++ {
		if _, err := w.Append(OpCreateNode, []byte("payload")); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	walPath := filepath.Join(dir, "wal.log")
	intact, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(intact) < 32 {
		t.Skipf("WAL too small (%d bytes) to punch a hole in", len(intact))
	}

	holed := make([]byte, len(intact))
	copy(holed, intact)
	for i := len(holed) / 3; i < len(holed)/3+8 && i < len(holed); i++ {
		holed[i] = 0x00
	}
	if err := os.WriteFile(walPath, holed, walFilePerm); err != nil {
		t.Fatalf("stage the hole: %v", err)
	}

	reopened, err := NewWAL(dir)
	if err != nil {
		return // a refusal is acceptable
	}
	defer func() { _ = reopened.Close() }()

	entries, err := reopened.ReadAll()
	if err != nil {
		return
	}
	if len(entries) > good {
		t.Errorf("a hole produced %d entries, more than the %d ever written", len(entries), good)
	}
	for i, e := range entries {
		if e.LSN != uint64(i+1) {
			t.Errorf("entry %d has LSN %d, want %d — recovery crossed the hole and kept going",
				i, e.LSN, i+1)
			break
		}
	}
}
