package lsm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"sort"
)

// NewSSTable creates a new SSTable from MemTable entries
func NewSSTable(path string, entries []*Entry) (*SSTable, error) {
	// Ensure entries are sorted
	sort.Slice(entries, func(i, j int) bool {
		return EntryCompare(entries[i], entries[j]) < 0
	})

	// Create Bloom filter
	bloom := NewBloomFilter(len(entries), 0.01) // 1% false positive rate
	for _, entry := range entries {
		bloom.Add(entry.Key)
	}

	// Create file
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	// Note: bufio.NewWriter does not return an error - it always succeeds
	writer := bufio.NewWriter(file)

	// Write header (placeholder, update later)
	header := SSTableHeader{
		Magic:      SSTableMagic,
		Version:    SSTableVersion,
		EntryCount: uint64(len(entries)),
	}

	if err := binary.Write(writer, binary.LittleEndian, &header); err != nil {
		_ = file.Close()
		return nil, err
	}

	// Write entries and build sparse index
	index := make([]IndexEntry, 0)
	offset := uint64(binary.Size(header))

	for i, entry := range entries {
		// Add to sparse index
		if i%IndexInterval == 0 {
			index = append(index, IndexEntry{
				Key:    entry.Key,
				Offset: offset,
			})
		}

		// Write entry
		entrySize, err := writeEntry(writer, entry)
		if err != nil {
			_ = file.Close()
			return nil, err
		}

		// Check for offset overflow (extremely unlikely but defensive)
		newOffset := offset + uint64(entrySize)
		if newOffset < offset {
			_ = file.Close()
			return nil, fmt.Errorf("SSTable offset overflow: file too large")
		}
		offset = newOffset
	}

	// Store index offset
	indexOffset := offset
	header.IndexOffset = indexOffset

	// Write index
	if err := writeIndex(writer, index); err != nil {
		_ = file.Close()
		return nil, err
	}

	// Write Bloom filter
	bloomData := bloom.MarshalBinary()
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(bloomData))); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := writer.Write(bloomData); err != nil {
		_ = file.Close()
		return nil, err
	}

	// Write footer with CRC over bloom filter data for integrity verification
	crc := crc32.ChecksumIEEE(bloomData)
	if err := binary.Write(writer, binary.LittleEndian, crc); err != nil {
		_ = file.Close()
		return nil, err
	}

	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return nil, err
	}

	// Update header with index offset
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return nil, err
	}

	if err := binary.Write(file, binary.LittleEndian, &header); err != nil {
		_ = file.Close()
		return nil, err
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &SSTable{
		path:       path,
		file:       file,
		header:     header,
		index:      index,
		bloom:      bloom,
		entryCount: len(entries),
	}, nil
}
