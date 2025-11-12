package wal

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// OpType represents the type of operation in the WAL
type OpType uint8

const (
	OpCreateNode OpType = iota
	OpUpdateNode
	OpDeleteNode
	OpCreateEdge
	OpUpdateEdge
	OpDeleteEdge
)

// Entry represents a single WAL entry
type Entry struct {
	LSN       uint64 // Log Sequence Number
	OpType    OpType
	Data      []byte
	Checksum  uint32
	Timestamp int64
}

// WAL is a Write-Ahead Log for durability
type WAL struct {
	file      *os.File
	writer    *bufio.Writer
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

// writeEntry writes a single entry to the WAL
// Format: [LSN:8][OpType:1][DataLen:4][Data:N][Checksum:4][Timestamp:8]
func (w *WAL) writeEntry(entry *Entry) error {
	// Write LSN
	if err := binary.Write(w.writer, binary.LittleEndian, entry.LSN); err != nil {
		return err
	}

	// Write OpType
	if err := w.writer.WriteByte(byte(entry.OpType)); err != nil {
		return err
	}

	// Write data length
	dataLen := uint32(len(entry.Data))
	if err := binary.Write(w.writer, binary.LittleEndian, dataLen); err != nil {
		return err
	}

	// Write data
	if _, err := w.writer.Write(entry.Data); err != nil {
		return err
	}

	// Write checksum
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Checksum); err != nil {
		return err
	}

	// Write timestamp
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Timestamp); err != nil {
		return err
	}

	return nil
}

// ReadAll reads all entries from the WAL
func (w *WAL) ReadAll() ([]*Entry, error) {
	// Seek to beginning
	if _, err := w.file.Seek(0, 0); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(w.file)
	entries := make([]*Entry, 0)

	for {
		entry, err := w.readEntry(reader)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Corrupted entry, stop reading
			break
		}

		// Verify checksum
		if crc32.ChecksumIEEE(entry.Data) != entry.Checksum {
			// Corrupted entry, stop reading
			break
		}

		entries = append(entries, entry)
	}

	// Seek back to end for appending
	if _, err := w.file.Seek(0, 2); err != nil {
		return nil, err
	}

	return entries, nil
}

// readEntry reads a single entry from the reader
func (w *WAL) readEntry(reader *bufio.Reader) (*Entry, error) {
	entry := &Entry{}

	// Read LSN
	if err := binary.Read(reader, binary.LittleEndian, &entry.LSN); err != nil {
		return nil, err
	}

	// Read OpType
	opTypeByte, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	entry.OpType = OpType(opTypeByte)

	// Read data length
	var dataLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &dataLen); err != nil {
		return nil, err
	}

	// Read data
	entry.Data = make([]byte, dataLen)
	if _, err := io.ReadFull(reader, entry.Data); err != nil {
		return nil, err
	}

	// Read checksum
	if err := binary.Read(reader, binary.LittleEndian, &entry.Checksum); err != nil {
		return nil, err
	}

	// Read timestamp
	if err := binary.Read(reader, binary.LittleEndian, &entry.Timestamp); err != nil {
		return nil, err
	}

	return entry, nil
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

	// Close current file
	if err := w.file.Close(); err != nil {
		return err
	}

	// Truncate file
	walPath := filepath.Join(w.dataDir, "wal.log")
	if err := os.Truncate(walPath, 0); err != nil {
		return err
	}

	// Reopen file
	file, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	w.file = file
	w.writer = bufio.NewWriter(file)
	w.currentLSN = 0

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
