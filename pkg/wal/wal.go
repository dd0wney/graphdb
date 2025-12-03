package wal

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// WAL is a Write-Ahead Log for durability
type WAL struct {
	file       *os.File
	writer     *bufio.Writer
	currentLSN uint64
	dataDir    string
	mu         sync.Mutex
}

// NewWAL creates a new Write-Ahead Log
func NewWAL(dataDir string) (*WAL, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal.log")

	// Open or create WAL file
	file, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	wal := &WAL{
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

// Append appends a new entry to the WAL
func (w *WAL) Append(opType OpType, data []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check for LSN overflow (CRITICAL: prevents wraparound)
	if w.currentLSN == ^uint64(0) { // MaxUint64
		return 0, fmt.Errorf("WAL LSN space exhausted - require WAL rotation")
	}

	w.currentLSN++
	lsn := w.currentLSN

	entry := Entry{
		LSN:       lsn,
		OpType:    opType,
		Data:      data,
		Checksum:  crc32.ChecksumIEEE(data),
		Timestamp: 0, // Will be set during write
	}

	if err := w.writeEntry(&entry); err != nil {
		w.currentLSN-- // Rollback LSN on error
		return 0, err
	}

	// Flush to disk for durability
	if err := w.writer.Flush(); err != nil {
		return 0, fmt.Errorf("failed to flush WAL: %w", err)
	}

	if err := w.file.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync WAL: %w", err)
	}

	return lsn, nil
}

// ReadAll reads all entries from the WAL
// Returns all valid entries read before any corruption is detected.
// Corruption is logged but does not return an error to allow partial recovery.
func (w *WAL) ReadAll() ([]*Entry, error) {
	// Seek to beginning
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, err
	}

	// Note: bufio.NewReader does not return an error - it always succeeds
	reader := bufio.NewReader(w.file)
	entries := make([]*Entry, 0)
	var entriesRead int

	for {
		entry, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Log corruption details for debugging
			log.Printf("WARNING: WAL corruption detected after %d entries: read error: %v", entriesRead, err)
			log.Printf("WARNING: WAL recovery stopped, %d entries recovered successfully", entriesRead)
			break
		}

		// Verify checksum
		expectedChecksum := crc32.ChecksumIEEE(entry.Data)
		if expectedChecksum != entry.Checksum {
			// Log checksum mismatch with details
			log.Printf("WARNING: WAL checksum mismatch at LSN %d (entry %d): expected %08x, got %08x",
				entry.LSN, entriesRead, expectedChecksum, entry.Checksum)
			log.Printf("WARNING: WAL recovery stopped, %d entries recovered successfully", entriesRead)
			break
		}

		entries = append(entries, entry)
		entriesRead++
	}

	// Seek back to end for appending
	if _, err := w.file.Seek(0, 2); err != nil {
		return nil, err
	}

	return entries, nil
}

// recoverLSN recovers the current LSN from existing WAL entries
func (w *WAL) recoverLSN() error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		w.currentLSN = entries[len(entries)-1].LSN
	}

	return nil
}

// Truncate truncates the WAL (used after snapshot)
func (w *WAL) Truncate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	walPath := filepath.Join(w.dataDir, "wal.log")

	// Flush any pending writes before truncating
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
		fmt.Printf("WARNING: failed to close old WAL file during truncate: %v\n", closeErr)
	}

	return nil
}

// GetCurrentLSN returns the current LSN
func (w *WAL) GetCurrentLSN() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentLSN
}

// Close closes the WAL
func (w *WAL) Close() error {
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

// Replay replays WAL entries to reconstruct state
func (w *WAL) Replay(handler func(*Entry) error) error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := handler(entry); err != nil {
			return fmt.Errorf("failed to replay entry LSN=%d: %w", entry.LSN, err)
		}
	}

	return nil
}
