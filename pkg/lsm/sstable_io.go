package lsm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeEntry writes an entry to the writer
// Format: keyLen(4) | key | valueLen(4) | value | timestamp(8) | deleted(1)
func writeEntry(w *bufio.Writer, entry *Entry) (int, error) {
	size := 0

	// Key length
	if err := binary.Write(w, binary.LittleEndian, uint32(len(entry.Key))); err != nil {
		return 0, err
	}
	size += 4

	// Key
	n, err := w.Write(entry.Key)
	if err != nil {
		return 0, err
	}
	size += n

	// Value length
	if err := binary.Write(w, binary.LittleEndian, uint32(len(entry.Value))); err != nil {
		return 0, err
	}
	size += 4

	// Value
	n, err = w.Write(entry.Value)
	if err != nil {
		return 0, err
	}
	size += n

	// Timestamp
	if err := binary.Write(w, binary.LittleEndian, entry.Timestamp); err != nil {
		return 0, err
	}
	size += 8

	// Deleted flag
	deleted := byte(0)
	if entry.Deleted {
		deleted = 1
	}
	if err := w.WriteByte(deleted); err != nil {
		return 0, err
	}
	size += 1

	return size, nil
}

// readEntry reads an entry from the reader
func readEntry(r *bufio.Reader) (*Entry, error) {
	// Key length
	var keyLen uint32
	if err := binary.Read(r, binary.LittleEndian, &keyLen); err != nil {
		return nil, err
	}

	// Key
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}

	// Value length
	var valueLen uint32
	if err := binary.Read(r, binary.LittleEndian, &valueLen); err != nil {
		return nil, err
	}

	// Value
	value := make([]byte, valueLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	// Timestamp
	var timestamp int64
	if err := binary.Read(r, binary.LittleEndian, &timestamp); err != nil {
		return nil, err
	}

	// Deleted flag
	deletedByte, err := r.ReadByte()
	if err != nil {
		return nil, err
	}

	return &Entry{
		Key:       key,
		Value:     value,
		Timestamp: timestamp,
		Deleted:   deletedByte == 1,
	}, nil
}

// writeIndex writes the sparse index
func writeIndex(w *bufio.Writer, index []IndexEntry) error {
	// Index entry count
	if err := binary.Write(w, binary.LittleEndian, uint32(len(index))); err != nil {
		return err
	}

	for _, entry := range index {
		// Key length
		if err := binary.Write(w, binary.LittleEndian, uint32(len(entry.Key))); err != nil {
			return err
		}

		// Key
		if _, err := w.Write(entry.Key); err != nil {
			return err
		}

		// Offset
		if err := binary.Write(w, binary.LittleEndian, entry.Offset); err != nil {
			return err
		}
	}

	return nil
}

// readIndex reads the sparse index
func readIndex(r *os.File) ([]IndexEntry, error) {
	// Index entry count
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}

	index := make([]IndexEntry, count)

	for i := uint32(0); i < count; i++ {
		// Key length
		var keyLen uint32
		if err := binary.Read(r, binary.LittleEndian, &keyLen); err != nil {
			return nil, err
		}

		// Key - use io.ReadFull to ensure complete read
		key := make([]byte, keyLen)
		if _, err := io.ReadFull(r, key); err != nil {
			return nil, fmt.Errorf("failed to read index key: %w", err)
		}

		// Offset
		var offset uint64
		if err := binary.Read(r, binary.LittleEndian, &offset); err != nil {
			return nil, err
		}

		index[i] = IndexEntry{
			Key:    key,
			Offset: offset,
		}
	}

	return index, nil
}

// SSTablePath generates a path for a new SSTable
func SSTablePath(dir string, level int, id int) string {
	return filepath.Join(dir, fmt.Sprintf("L%d-%06d.sst", level, id))
}
