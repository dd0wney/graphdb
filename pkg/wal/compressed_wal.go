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
	"time"

	"github.com/golang/snappy"
)

// CompressedWAL is a Write-Ahead Log with snappy compression
type CompressedWAL struct {
	file       *os.File
	writer     *bufio.Writer
	currentLSN uint64
	dataDir    string
	mu         sync.Mutex

	// Statistics
	totalWrites       uint64
	bytesUncompressed uint64
	bytesCompressed   uint64
}

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
		return nil, fmt.Errorf("failed to recover LSN: %w", err)
	}

	return wal, nil
}

// Append appends a new entry to the compressed WAL
func (w *CompressedWAL) Append(opType OpType, data []byte) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.currentLSN++
	lsn := w.currentLSN

	// Compress data
	compressedData := snappy.Encode(nil, data)

	entry := Entry{
		LSN:       lsn,
		OpType:    opType,
		Data:      compressedData,
		Checksum:  crc32.ChecksumIEEE(compressedData),
		Timestamp: time.Now().Unix(),
	}

	// Track statistics
	w.totalWrites++
	w.bytesUncompressed += uint64(len(data))
	w.bytesCompressed += uint64(len(compressedData))

	return lsn, w.writeEntry(&entry)
}

// writeEntry writes an entry to disk (same format as regular WAL)
func (w *CompressedWAL) writeEntry(entry *Entry) error {
	// Format: [LSN:8][OpType:1][DataLen:4][Data:N][Checksum:4][Timestamp:8]

	// LSN
	if err := binary.Write(w.writer, binary.BigEndian, entry.LSN); err != nil {
		return err
	}

	// OpType
	if err := w.writer.WriteByte(byte(entry.OpType)); err != nil {
		return err
	}

	// Data length
	dataLen := uint32(len(entry.Data))
	if err := binary.Write(w.writer, binary.BigEndian, dataLen); err != nil {
		return err
	}

	// Data
	if _, err := w.writer.Write(entry.Data); err != nil {
		return err
	}

	// Checksum
	if err := binary.Write(w.writer, binary.BigEndian, entry.Checksum); err != nil {
		return err
	}

	// Timestamp
	if err := binary.Write(w.writer, binary.BigEndian, entry.Timestamp); err != nil {
		return err
	}

	return w.writer.Flush()
}

// ReadAll reads all entries from the WAL (decompressing data)
func (w *CompressedWAL) ReadAll() ([]*Entry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Open file for reading
	file, err := os.Open(filepath.Join(w.dataDir, "wal_compressed.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	entries := make([]*Entry, 0)

	for {
		entry := &Entry{}

		// LSN
		if err := binary.Read(reader, binary.BigEndian, &entry.LSN); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		// OpType
		opTypeByte, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		entry.OpType = OpType(opTypeByte)

		// Data length
		var dataLen uint32
		if err := binary.Read(reader, binary.BigEndian, &dataLen); err != nil {
			return nil, err
		}

		// Data (compressed)
		compressedData := make([]byte, dataLen)
		if _, err := io.ReadFull(reader, compressedData); err != nil {
			return nil, err
		}

		// Decompress data
		decompressedData, err := snappy.Decode(nil, compressedData)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress WAL entry: %w", err)
		}
		entry.Data = decompressedData

		// Checksum
		if err := binary.Read(reader, binary.BigEndian, &entry.Checksum); err != nil {
			return nil, err
		}

		// Verify checksum (on compressed data)
		if crc32.ChecksumIEEE(compressedData) != entry.Checksum {
			return nil, fmt.Errorf("checksum mismatch for entry %d", entry.LSN)
		}

		// Timestamp
		if err := binary.Read(reader, binary.BigEndian, &entry.Timestamp); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, nil
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

	w.writer.Flush()
	w.file.Close()

	walPath := filepath.Join(w.dataDir, "wal_compressed.log")

	// Remove old file
	if err := os.Remove(walPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Create new file
	file, err := os.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	w.file = file
	w.writer = bufio.NewWriter(file)
	w.currentLSN = 0

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

// CompressedWALStats holds compression statistics
type CompressedWALStats struct {
	TotalWrites       uint64
	BytesUncompressed uint64
	BytesCompressed   uint64
	CompressionRatio  float64 // e.g., 0.75 = 75% compression
	SpaceSavings      float64 // MB saved
}
