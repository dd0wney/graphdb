package wal

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/snappy"
)

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

	if err := w.writeEntry(&entry); err != nil {
		w.currentLSN-- // Rollback LSN on error
		return 0, fmt.Errorf("failed to write WAL entry: %w", err)
	}

	// Flush to disk for durability
	if err := w.writer.Flush(); err != nil {
		return 0, fmt.Errorf("failed to flush WAL: %w", err)
	}

	// Sync to ensure durability
	if err := w.file.Sync(); err != nil {
		return 0, fmt.Errorf("failed to sync WAL: %w", err)
	}

	return lsn, nil
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

	return nil
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

	// Note: bufio.NewReader does not return an error - it always succeeds
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

// Replay iterates through all WAL entries and calls the handler for each.
// This is used for recovery after restart.
func (w *CompressedWAL) Replay(handler func(*Entry) error) error {
	entries, err := w.ReadAll()
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := handler(entry); err != nil {
			return err
		}
	}

	return nil
}
