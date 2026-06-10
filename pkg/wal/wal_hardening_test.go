package wal

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestWAL_FilePermissionsOwnerOnly pins security audit finding H-2: WAL
// files and their directory must be owner-only. WAL entries hold the full
// serialized JSON of every node and edge, so a world-readable 0644 file
// exposed all-tenant data to any local OS user.
//
// RED against pre-fix code: files were created 0644 and the dir 0755.
func TestWAL_FilePermissionsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not applicable on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "wal-perms")
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()
	if _, err := w.Append(OpCreateNode, []byte("sensitive node payload")); err != nil {
		t.Fatalf("Append: %v", err)
	}

	dirInfo, err := os.Stat(dataDir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("WAL dir mode %o: group/other bits set (want owner-only)", perm)
	}

	fileInfo, err := os.Stat(filepath.Join(dataDir, "wal.log"))
	if err != nil {
		t.Fatalf("stat wal.log: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("wal.log mode %o: group/other bits set (want owner-only)", perm)
	}
}

// TestWAL_OversizeRecordDoesNotAllocate pins security audit finding H-4:
// a record whose declared DataLen exceeds maxWALRecordSize must be treated
// as corruption, not allocated. A crafted dataLen of 0xFFFFFFFF (~4 GiB)
// would otherwise OOM-kill the server on every restart.
//
// The test writes one valid entry, then appends a poisoned record header
// claiming a 4 GiB payload, and asserts ReadAll recovers exactly the valid
// prefix without panicking or allocating gigabytes.
//
// RED against pre-fix code: readEntry does make([]byte, 0xFFFFFFFF) before
// any bound check, so this either OOMs or (with enough RAM) blocks for a
// long time on a 4 GiB allocation.
func TestWAL_OversizeRecordDoesNotAllocate(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	if _, err := w.Append(OpCreateNode, []byte("valid entry")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append a poisoned record: [LSN:8][OpType:1][DataLen:4 = 0xFFFFFFFF].
	// readEntry reaches the DataLen and must reject it before allocating.
	walPath := filepath.Join(dataDir, "wal.log")
	f, err := os.OpenFile(walPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open for poison append: %v", err)
	}
	poison := make([]byte, 0, 13)
	poison = binary.LittleEndian.AppendUint64(poison, 99) // LSN
	poison = append(poison, byte(OpCreateNode))           // OpType
	poison = binary.LittleEndian.AppendUint32(poison, 0xFFFFFFFF)
	if _, err := f.Write(poison); err != nil {
		t.Fatalf("write poison: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close poison file: %v", err)
	}

	// Reopen and replay. recoverLSN + a fresh ReadAll both walk the read
	// path; neither must OOM. ReadAll returns the valid prefix.
	w2, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("reopen WAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after poison: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("recovered %d entries, want 1 (the valid prefix before the poisoned record)", len(entries))
	}
	if string(entries[0].Data) != "valid entry" {
		t.Errorf("recovered entry payload = %q, want %q", entries[0].Data, "valid entry")
	}
}
