package lsm

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/exp/mmap"
)

// readEntryFromMmap reads an entry from memory-mapped file
// Returns entry, bytes read, and error
func readEntryFromMmap(r *mmap.ReaderAt, offset int64) (*Entry, int, error) {
	bytesRead := 0

	// Read key length
	keyLenBuf := make([]byte, 4)
	if _, err := r.ReadAt(keyLenBuf, offset); err != nil {
		return nil, 0, err
	}
	var keyLen uint32
	if err := binary.Read(bytes.NewReader(keyLenBuf), binary.LittleEndian, &keyLen); err != nil {
		return nil, 0, fmt.Errorf("failed to read key length: %w", err)
	}
	offset += 4
	bytesRead += 4

	// Read key
	key := make([]byte, keyLen)
	if _, err := r.ReadAt(key, offset); err != nil {
		return nil, 0, err
	}
	offset += int64(keyLen)
	bytesRead += int(keyLen)

	// Read value length
	valueLenBuf := make([]byte, 4)
	if _, err := r.ReadAt(valueLenBuf, offset); err != nil {
		return nil, 0, err
	}
	var valueLen uint32
	if err := binary.Read(bytes.NewReader(valueLenBuf), binary.LittleEndian, &valueLen); err != nil {
		return nil, 0, fmt.Errorf("failed to read value length: %w", err)
	}
	offset += 4
	bytesRead += 4

	// Read value
	value := make([]byte, valueLen)
	if _, err := r.ReadAt(value, offset); err != nil {
		return nil, 0, err
	}
	offset += int64(valueLen)
	bytesRead += int(valueLen)

	// Read timestamp
	timestampBuf := make([]byte, 8)
	if _, err := r.ReadAt(timestampBuf, offset); err != nil {
		return nil, 0, err
	}
	var timestamp int64
	if err := binary.Read(bytes.NewReader(timestampBuf), binary.LittleEndian, &timestamp); err != nil {
		return nil, 0, fmt.Errorf("failed to read timestamp: %w", err)
	}
	offset += 8
	bytesRead += 8

	// Read deleted flag
	deletedBuf := make([]byte, 1)
	if _, err := r.ReadAt(deletedBuf, offset); err != nil {
		return nil, 0, err
	}
	bytesRead += 1

	return &Entry{
		Key:       key,
		Value:     value,
		Timestamp: timestamp,
		Deleted:   deletedBuf[0] == 1,
	}, bytesRead, nil
}

// readIndexFromMmap reads the sparse index from memory-mapped file
func readIndexFromMmap(r *mmap.ReaderAt, offset int64) ([]IndexEntry, error) {
	// Read index entry count
	countBuf := make([]byte, 4)
	if _, err := r.ReadAt(countBuf, offset); err != nil {
		return nil, err
	}
	var count uint32
	if err := binary.Read(bytes.NewReader(countBuf), binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("failed to read index count: %w", err)
	}
	offset += 4

	index := make([]IndexEntry, count)

	for i := uint32(0); i < count; i++ {
		// Read key length
		keyLenBuf := make([]byte, 4)
		if _, err := r.ReadAt(keyLenBuf, offset); err != nil {
			return nil, err
		}
		var keyLen uint32
		if err := binary.Read(bytes.NewReader(keyLenBuf), binary.LittleEndian, &keyLen); err != nil {
			return nil, fmt.Errorf("failed to read index key length: %w", err)
		}
		offset += 4

		// Read key
		key := make([]byte, keyLen)
		if _, err := r.ReadAt(key, offset); err != nil {
			return nil, err
		}
		offset += int64(keyLen)

		// Read offset
		offsetBuf := make([]byte, 8)
		if _, err := r.ReadAt(offsetBuf, offset); err != nil {
			return nil, err
		}
		var entryOffset uint64
		if err := binary.Read(bytes.NewReader(offsetBuf), binary.LittleEndian, &entryOffset); err != nil {
			return nil, fmt.Errorf("failed to read index offset: %w", err)
		}
		offset += 8

		index[i] = IndexEntry{
			Key:    key,
			Offset: entryOffset,
		}
	}

	return index, nil
}
