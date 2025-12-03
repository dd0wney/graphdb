package wal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// NewCompressedWAL creates a new compressed Write-Ahead Log
func NewCompressedWAL(dataDir string) (*CompressedWAL, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal_compressed.log")

	// Open or create WAL file
	file, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wal := &CompressedWAL{
		file:    file,
		writer:  bufio.NewWriter(file),
		dataDir: dataDir,
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
func (w *CompressedWAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}

	if err := w.file.Sync(); err != nil {
		return err
	}

	return w.file.Close()
}

// Truncate truncates the WAL (used after successful snapshot)
func (w *CompressedWAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	walPath := filepath.Join(w.dataDir, "wal_compressed.log")

	// Flush any buffered data before truncating
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL before truncate: %w", err)
	}

	// Create the new file BEFORE closing the old one to ensure we have a valid handle
	newFile, err := os.OpenFile(walPath+".new", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create new WAL file: %w", err)
	}

	// Close current file
	closeErr := w.file.Close()

	// Rename new file to replace old file (atomic on POSIX)
	if err := os.Rename(walPath+".new", walPath); err != nil {
		// Failed to rename - close new file and return error
		newFile.Close()
		// Try to reopen old file to maintain consistent state
		if oldFile, reopenErr := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644); reopenErr == nil {
			w.file = oldFile
			w.writer = bufio.NewWriter(oldFile)
		}
		return fmt.Errorf("failed to rename WAL file: %w (close error: %v)", err, closeErr)
	}

	// Update state with new file
	w.file = newFile
	w.writer = bufio.NewWriter(newFile)
	w.currentLSN = 0

	// Return close error if rename succeeded but close failed (non-fatal but worth logging)
	if closeErr != nil {
		// Log but don't fail - we successfully truncated
		fmt.Printf("WARNING: failed to close old compressed WAL file during truncate: %v\n", closeErr)
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

// GetCurrentLSN returns the current LSN
func (w *CompressedWAL) GetCurrentLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLSN
}
