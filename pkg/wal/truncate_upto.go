package wal

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"

	"github.com/golang/snappy"
)

// TruncateUpTo rewrites the WAL keeping only entries with LSN > lsn — the
// checkpoint primitive behind GraphStorage.CompactWAL (M-1, WAL remanence:
// purge a deleted tenant's records mid-flight without losing concurrent
// writers' entries). The caller is responsible for having captured a
// snapshot whose state covers every entry with LSN ≤ lsn.
//
// currentLSN is intentionally NOT reset (unlike Truncate): kept entries
// retain their LSNs and new appends must continue past them to stay
// monotonic within the file.
//
// Crash safety reuses Truncate's pattern: the rewrite lands in wal.log.new,
// fsynced, then atomically renamed over wal.log. A crash before the rename
// leaves the original file intact (the purge simply didn't happen yet); a
// stale .new from such a crash is overwritten by the next rewrite.
func (w *WAL) TruncateUpTo(lsn uint64) error {
	if lsn == 0 {
		return nil // nothing can have LSN ≤ 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL before truncate: %w", err)
	}

	// ReadAll does not take w.mu (it repositions w.file, which is safe
	// here because we hold the lock and it seeks back to the end).
	entries, err := w.ReadAll()
	if err != nil {
		return fmt.Errorf("failed to read WAL for truncation: %w", err)
	}

	walPath := filepath.Join(w.dataDir, "wal.log")
	newFile, err := os.OpenFile(walPath+".new", os.O_RDWR|os.O_CREATE|os.O_TRUNC, walFilePerm)
	if err != nil {
		return fmt.Errorf("failed to create new WAL file: %w", err)
	}

	newWriter := bufio.NewWriter(newFile)
	for _, entry := range entries {
		if entry.LSN <= lsn {
			continue
		}
		if err := writeEntryTo(newWriter, entry); err != nil {
			newFile.Close()
			os.Remove(walPath + ".new")
			return fmt.Errorf("failed to rewrite WAL entry LSN=%d: %w", entry.LSN, err)
		}
	}
	if err := newWriter.Flush(); err != nil {
		newFile.Close()
		os.Remove(walPath + ".new")
		return fmt.Errorf("failed to flush rewritten WAL: %w", err)
	}
	if err := newFile.Sync(); err != nil {
		newFile.Close()
		os.Remove(walPath + ".new")
		return fmt.Errorf("failed to sync rewritten WAL: %w", err)
	}

	return w.swapInRewrittenFile(walPath, newFile)
}

// swapInRewrittenFile closes the live WAL file, renames path+".new" over
// it, and repoints the handles — the shared tail of Truncate/TruncateUpTo.
// On rename failure it reopens the original file so the WAL stays usable.
// Note newFile was opened without O_APPEND: its offset sits at EOF after
// the rewrite and persists through the rename (rename changes the name,
// not the open descriptor), so subsequent appends land correctly.
func (w *WAL) swapInRewrittenFile(walPath string, newFile *os.File) error {
	closeErr := w.file.Close()

	if err := os.Rename(walPath+".new", walPath); err != nil {
		newFile.Close()
		if oldFile, reopenErr := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm); reopenErr == nil {
			w.file = oldFile
			w.writer = bufio.NewWriter(oldFile)
		}
		return fmt.Errorf("failed to rename WAL file: %w (close error: %v)", err, closeErr)
	}

	w.file = newFile
	w.writer = bufio.NewWriter(newFile)

	if closeErr != nil {
		fmt.Printf("WARNING: failed to close old WAL file during truncate: %v\n", closeErr)
	}
	return nil
}

// CheckpointLSN drains every entry enqueued before the call — including
// any batch the background flusher has taken ownership of but not yet
// LSN-assigned — and returns the boundary LSN: all prior enqueues have
// LSN ≤ the returned value; anything enqueued after gets LSN > it
// (provided the caller serializes enqueuers against this call, as
// GraphStorage does via gs.mu). This is the batched-WAL boundary capture
// for the M-1 snapshot+TruncateUpTo checkpoint; a bare GetCurrentLSN
// would miss in-flight batches.
func (bw *BatchedWAL) CheckpointLSN() uint64 {
	bw.flushMu.Lock()
	defer bw.flushMu.Unlock()
	bw.flushLocked()
	return bw.wal.GetCurrentLSN()
}

// TruncateUpTo flushes pending entries, then delegates to the underlying
// WAL. Interleaving with a concurrent background flush is safe either
// way: AppendBatch holds the WAL's mutex for the whole batch, so an
// in-flight batch lands wholly before the rewrite (and is copied, its
// LSNs being > the checkpoint boundary) or wholly after (appended to the
// rewritten file).
func (bw *BatchedWAL) TruncateUpTo(lsn uint64) error {
	bw.flush()
	return bw.wal.TruncateUpTo(lsn)
}

// TruncateUpTo is the compressed-backend rewrite. Entries are re-encoded
// (snappy is deterministic, checksums recomputed over the compressed
// bytes) because readAllLocked returns decompressed payloads.
func (w *CompressedWAL) TruncateUpTo(lsn uint64) error {
	if lsn == 0 {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL before truncate: %w", err)
	}

	entries, err := w.readAllLocked()
	if err != nil {
		return fmt.Errorf("failed to read WAL for truncation: %w", err)
	}

	walPath := filepath.Join(w.dataDir, "wal_compressed.log")
	newFile, err := os.OpenFile(walPath+".new", os.O_RDWR|os.O_CREATE|os.O_TRUNC, walFilePerm)
	if err != nil {
		return fmt.Errorf("failed to create new WAL file: %w", err)
	}

	newWriter := bufio.NewWriter(newFile)
	for _, entry := range entries {
		if entry.LSN <= lsn {
			continue
		}
		compressed := snappy.Encode(nil, entry.Data)
		rewritten := Entry{
			LSN:       entry.LSN,
			OpType:    entry.OpType,
			Data:      compressed,
			Checksum:  crc32.ChecksumIEEE(compressed),
			Timestamp: entry.Timestamp,
		}
		if err := writeCompressedEntryTo(newWriter, &rewritten); err != nil {
			newFile.Close()
			os.Remove(walPath + ".new")
			return fmt.Errorf("failed to rewrite WAL entry LSN=%d: %w", entry.LSN, err)
		}
	}
	if err := newWriter.Flush(); err != nil {
		newFile.Close()
		os.Remove(walPath + ".new")
		return fmt.Errorf("failed to flush rewritten WAL: %w", err)
	}
	if err := newFile.Sync(); err != nil {
		newFile.Close()
		os.Remove(walPath + ".new")
		return fmt.Errorf("failed to sync rewritten WAL: %w", err)
	}

	closeErr := w.file.Close()
	if err := os.Rename(walPath+".new", walPath); err != nil {
		newFile.Close()
		if oldFile, reopenErr := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm); reopenErr == nil {
			w.file = oldFile
			w.writer = bufio.NewWriter(oldFile)
		}
		return fmt.Errorf("failed to rename WAL file: %w (close error: %v)", err, closeErr)
	}
	w.file = newFile
	w.writer = bufio.NewWriter(newFile)
	if closeErr != nil {
		fmt.Printf("WARNING: failed to close old compressed WAL file during truncate: %v\n", closeErr)
	}
	return nil
}
