package wal

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// NewCompressedWAL creates a new compressed Write-Ahead Log
func NewCompressedWAL(dataDir string) (*CompressedWAL, error) {
	return NewCompressedWALWithFS(dataDir, nil)
}

// NewCompressedWALWithFS is NewCompressedWAL on a filesystem driver. A nil fs
// means vfs.Default(), which is what ships.
func NewCompressedWALWithFS(dataDir string, fs vfs.FileSystem) (*CompressedWAL, error) {
	if fs == nil {
		fs = vfs.Default()
	}
	if err := fs.MkdirAll(dataDir, walDirPerm); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal_compressed.log")

	// Open or create WAL file
	file, err := fs.Open(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wal := &CompressedWAL{
		file:    file,
		writer:  bufio.NewWriter(file),
		dataDir: dataDir,
		fs:      fs,
	}

	// Read existing entries to set currentLSN
	if err := wal.recoverLSN(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to recover LSN: %w", err)
	}

	return wal, nil
}

// Flush flushes the WAL to disk
func (w *CompressedWAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close closes the WAL
// Close flushes, syncs and closes, and reports every failure among them.
//
// All three run unconditionally. Returning early on the flush or the sync
// skipped file.Close and leaked the handle, which a sweep measured at 9 of 24
// injected failure points — every one of them a WAL that had opened and
// appended successfully and been closed by its owner.
//
// WAL.Close had this right and this did not: two implementations of one idea
// that had come apart. persistence.go states the same rule for the store's own
// Close, "The Close runs either way", after the identical defect was fixed
// there.
//
// The descriptor is what cannot be recovered. A flush error is reported to a
// caller that can act on it; a leaked handle is unreclaimable for the life of
// the process, and on Windows it blocks removal of the directory holding it.
func (w *CompressedWAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	flushErr := w.writer.Flush()
	syncErr := w.file.Sync()
	closeErr := w.file.Close()

	return errors.Join(flushErr, syncErr, closeErr)
}

// Truncate truncates the WAL (used after successful snapshot). It does not
// reset currentLSN: the LSN is monotonic for the life of the data directory,
// so that the WAL boundary LSN a snapshot records keeps meaning after the
// truncate that normally follows writing it. RaiseLSNTo restores the counter
// on the next open. See WAL.Truncate.
func (w *CompressedWAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	walPath := filepath.Join(w.dataDir, "wal_compressed.log")

	// Discard, do not flush: the buffer is empty on the success path, and
	// after a failed append it holds only a sticky error and the tail of
	// the entry that did not land. See WAL.Truncate.
	w.writer.Reset(w.file)

	// Create the new file BEFORE closing the old one to ensure we have a valid handle
	newFile, err := w.fs.Open(walPath+".new", os.O_RDWR|os.O_CREATE|os.O_TRUNC, walFilePerm)
	if err != nil {
		return fmt.Errorf("failed to create new WAL file: %w", err)
	}

	// Close current file
	closeErr := w.file.Close()

	// Rename new file to replace old file (atomic on POSIX)
	if err := w.fs.Rename(walPath+".new", walPath); err != nil {
		// Failed to rename - close new file and return error
		newFile.Close()
		// Try to reopen old file to maintain consistent state
		if oldFile, reopenErr := w.fs.Open(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, walFilePerm); reopenErr == nil {
			w.file = oldFile
			w.writer = bufio.NewWriter(oldFile)
		}
		return fmt.Errorf("failed to rename WAL file: %w (close error: %v)", err, closeErr)
	}

	// Update state with new file. currentLSN is deliberately left alone —
	// see the Truncate doc comment.
	w.file = newFile
	w.writer = bufio.NewWriter(newFile)

	// Return close error if rename succeeded but close failed (non-fatal but worth logging)
	if closeErr != nil {
		// Log but don't fail - we successfully truncated
		fmt.Printf("WARNING: failed to close old compressed WAL file during truncate: %v\n", closeErr)
	}

	// Publish the new name. See WAL.Truncate for why the rename alone is not
	// enough, and why this runs after the handles are repointed.
	if err := vfs.SyncParentDir(w.fs, walPath); err != nil {
		return fmt.Errorf("failed to sync the WAL directory after rotation: %w", err)
	}

	return nil
}

// recoverLSN recovers the current LSN by reading all entries
func (w *CompressedWAL) recoverLSN() error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		w.currentLSN = entries[len(entries)-1].LSN
	}

	return nil
}

// GetStatistics returns compression statistics
func (w *CompressedWAL) GetStatistics() CompressedWALStats {
	w.mu.Lock()
	defer w.mu.Unlock()

	compressionRatio := 0.0
	if w.bytesUncompressed > 0 {
		compressionRatio = 1.0 - (float64(w.bytesCompressed) / float64(w.bytesUncompressed))
	}

	return CompressedWALStats{
		TotalWrites:       w.totalWrites,
		BytesUncompressed: w.bytesUncompressed,
		BytesCompressed:   w.bytesCompressed,
		CompressionRatio:  compressionRatio,
		SpaceSavings:      float64(w.bytesUncompressed-w.bytesCompressed) / 1024 / 1024, // MB
	}
}

// GetCurrentLSN returns the current LSN. It is monotonic for the life of the
// data directory: Truncate empties the WAL file but never lowers this
// counter, so a snapshot's recorded WAL boundary LSN stays meaningful across
// the truncate that normally follows writing it.
func (w *CompressedWAL) GetCurrentLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLSN
}

// RaiseLSNTo sets the LSN counter to lsn when the counter is currently lower,
// and otherwise leaves it unchanged. See WAL.RaiseLSNTo.
func (w *CompressedWAL) RaiseLSNTo(lsn uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.currentLSN < lsn {
		w.currentLSN = lsn
	}
}
