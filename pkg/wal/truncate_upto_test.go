package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// M-1 Option A (DESIGN_m1_wal_remanence_2026-06-10.md): TruncateUpTo(lsn)
// rewrites the WAL keeping only entries with LSN > lsn — the checkpoint
// primitive that lets a snapshot+truncate purge a deleted tenant's records
// without losing concurrent writers' entries. currentLSN is NOT reset
// (kept entries stay monotonic; new appends continue past them).

func appendN(t *testing.T, w *WAL, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if _, err := w.Append(OpCreateNode, fmt.Appendf(nil, "entry-%d", i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
}

func lsnsOf(entries []*Entry) []uint64 {
	out := make([]uint64, len(entries))
	for i, e := range entries {
		out[i] = e.LSN
	}
	return out
}

func TestWAL_TruncateUpTo_KeepsOnlySuffix(t *testing.T) {
	w, err := NewWAL(t.TempDir())
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()
	appendN(t, w, 5)

	if err := w.TruncateUpTo(3); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("expected LSNs [4 5], got %v", got)
	}
	if string(entries[0].Data) != "entry-4" {
		t.Fatalf("kept entry data corrupted: %q", entries[0].Data)
	}
	if lsn := w.GetCurrentLSN(); lsn != 5 {
		t.Fatalf("currentLSN must stay 5 after TruncateUpTo, got %d", lsn)
	}
	if lsn, _ := w.Append(OpCreateNode, []byte("after")); lsn != 6 {
		t.Fatalf("next append should be LSN 6, got %d", lsn)
	}
}

func TestWAL_TruncateUpTo_AllEntries(t *testing.T) {
	w, err := NewWAL(t.TempDir())
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()
	appendN(t, w, 5)

	if err := w.TruncateUpTo(5); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty WAL, got LSNs %v", lsnsOf(entries))
	}
	if lsn, _ := w.Append(OpCreateNode, []byte("after")); lsn != 6 {
		t.Fatalf("LSN must not reset after full TruncateUpTo: want 6, got %d", lsn)
	}
}

func TestWAL_TruncateUpTo_ZeroIsNoOp(t *testing.T) {
	w, err := NewWAL(t.TempDir())
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()
	appendN(t, w, 3)

	if err := w.TruncateUpTo(0); err != nil {
		t.Fatalf("TruncateUpTo(0): %v", err)
	}
	entries, _ := w.ReadAll()
	if len(entries) != 3 {
		t.Fatalf("TruncateUpTo(0) must keep all entries, got %d", len(entries))
	}
}

func TestWAL_TruncateUpTo_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	appendN(t, w, 5)
	if err := w.TruncateUpTo(3); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	entries, _ := reopened.ReadAll()
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("after reopen expected LSNs [4 5], got %v", got)
	}
	if lsn := reopened.GetCurrentLSN(); lsn != 5 {
		t.Fatalf("recovered LSN should be 5, got %d", lsn)
	}
}

// A crashed prior truncate can leave a stale wal.log.new behind (crash
// between creating it and the rename). It must not break the next
// TruncateUpTo, and the WAL content must be the rewrite's, not the stale
// file's.
func TestWAL_TruncateUpTo_OverwritesStaleNewFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	defer w.Close()
	appendN(t, w, 4)

	stale := filepath.Join(dir, "wal.log.new")
	if err := os.WriteFile(stale, []byte("garbage-from-crashed-truncate"), 0o600); err != nil {
		t.Fatalf("plant stale .new: %v", err)
	}

	if err := w.TruncateUpTo(2); err != nil {
		t.Fatalf("TruncateUpTo with stale .new: %v", err)
	}
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("expected LSNs [3 4], got %v", got)
	}
}

func TestBatchedWAL_TruncateUpTo_KeepsOnlySuffix(t *testing.T) {
	bw, err := NewBatchedWAL(t.TempDir(), 100, time.Millisecond)
	if err != nil {
		t.Fatalf("NewBatchedWAL: %v", err)
	}
	defer bw.Close()
	for i := 1; i <= 5; i++ {
		if _, err := bw.Append(OpCreateNode, fmt.Appendf(nil, "entry-%d", i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := bw.TruncateUpTo(3); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	entries, err := bw.wal.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("expected LSNs [4 5], got %v", got)
	}
}

// CheckpointLSN must drain every entry enqueued before the call and
// return a boundary covering them — including entries the background
// flusher has taken ownership of but not yet LSN-assigned (the in-flight
// flush window). Entries enqueued after the call get LSNs above the
// boundary.
func TestBatchedWAL_CheckpointLSN_CoversAllPriorEnqueues(t *testing.T) {
	// Long flush interval: nothing flushes unless we make it.
	bw, err := NewBatchedWAL(t.TempDir(), 1000, time.Hour)
	if err != nil {
		t.Fatalf("NewBatchedWAL: %v", err)
	}
	defer bw.Close()

	pendings := make([]*Pending, 0, 3)
	for i := 1; i <= 3; i++ {
		pendings = append(pendings, bw.Enqueue(OpCreateNode, fmt.Appendf(nil, "enqueued-%d", i)))
	}

	boundary := bw.CheckpointLSN()
	if boundary != 3 {
		t.Fatalf("boundary should cover the 3 enqueued entries, got %d", boundary)
	}
	// The drain made them durable: their Pendings resolve without another flush.
	for i, p := range pendings {
		if err := p.Wait(); err != nil {
			t.Fatalf("pending %d not durable after CheckpointLSN: %v", i+1, err)
		}
	}

	after := bw.Enqueue(OpCreateNode, []byte("post-boundary"))
	bw.flush()
	if err := after.Wait(); err != nil {
		t.Fatalf("post-boundary flush: %v", err)
	}
	entries, _ := bw.wal.ReadAll()
	last := entries[len(entries)-1]
	if last.LSN <= boundary {
		t.Fatalf("post-boundary entry must have LSN > %d, got %d", boundary, last.LSN)
	}
}

func TestCompressedWAL_TruncateUpTo_KeepsOnlySuffixAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	cw, err := NewCompressedWAL(dir)
	if err != nil {
		t.Fatalf("NewCompressedWAL: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := cw.Append(OpCreateNode, fmt.Appendf(nil, "compressed-entry-%d", i)); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := cw.TruncateUpTo(3); err != nil {
		t.Fatalf("TruncateUpTo: %v", err)
	}

	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("expected LSNs [4 5], got %v", got)
	}
	if string(entries[0].Data) != "compressed-entry-4" {
		t.Fatalf("kept entry should decompress to original data, got %q", entries[0].Data)
	}
	if lsn := cw.GetCurrentLSN(); lsn != 5 {
		t.Fatalf("currentLSN must stay 5, got %d", lsn)
	}

	if err := cw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewCompressedWAL(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	entries, err = reopened.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after reopen: %v", err)
	}
	if got := lsnsOf(entries); len(got) != 2 || got[0] != 4 || got[1] != 5 {
		t.Fatalf("after reopen expected LSNs [4 5], got %v", got)
	}
}
