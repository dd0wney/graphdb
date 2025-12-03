package lsm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

// OpenSSTable opens an existing SSTable
func OpenSSTable(path string) (*SSTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Read header
	var header SSTableHeader
	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		_ = file.Close()
		return nil, err
	}

	if header.Magic != SSTableMagic {
		_ = file.Close()
		return nil, fmt.Errorf("invalid SSTable magic: %x", header.Magic)
	}

	// Read index
	if _, err := file.Seek(int64(header.IndexOffset), 0); err != nil {
		_ = file.Close()
		return nil, err
	}

	index, err := readIndex(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	// Read Bloom filter
	var bloomSize uint32
	if err := binary.Read(file, binary.LittleEndian, &bloomSize); err != nil {
		// Old format without bloom filter - create empty one
		bloom := NewBloomFilter(int(header.EntryCount), 0.01)
		return &SSTable{
			path:       path,
			file:       file,
			header:     header,
			index:      index,
			bloom:      bloom,
			entryCount: int(header.EntryCount),
		}, nil
	}

	bloomData := make([]byte, bloomSize)
	if _, err := file.Read(bloomData); err != nil {
		_ = file.Close()
		return nil, err
	}

	// Read and verify CRC
	var storedCRC uint32
	if err := binary.Read(file, binary.LittleEndian, &storedCRC); err != nil {
		// Old format without CRC - skip verification
	} else {
		// Verify CRC
		calculatedCRC := crc32.ChecksumIEEE(bloomData)
		if storedCRC != calculatedCRC {
			_ = file.Close()
			return nil, fmt.Errorf("bloom filter CRC mismatch: stored %x, calculated %x", storedCRC, calculatedCRC)
		}
	}

	bloom := NewBloomFilter(int(header.EntryCount), 0.01)
	if err := bloom.UnmarshalBinary(bloomData); err != nil {
		_ = file.Close()
		return nil, err
	}

	return &SSTable{
		path:       path,
		file:       file,
		header:     header,
		index:      index,
		bloom:      bloom,
		entryCount: int(header.EntryCount),
	}, nil
}

// Scan returns entries in range [start, end)
func (sst *SSTable) Scan(start, end []byte) ([]*Entry, error) {
	// Find starting position
	idx := sst.findIndexPosition(start)

	startOffset := uint64(binary.Size(sst.header))
	if idx > 0 {
		startOffset = sst.index[idx-1].Offset
	}

	if _, err := sst.file.Seek(int64(startOffset), 0); err != nil {
		return nil, err
	}

	// Note: bufio.NewReader does not return an error - it always succeeds
	reader := bufio.NewReader(sst.file)
	results := make([]*Entry, 0)

	for {
		entry, err := readEntry(reader)
		if err != nil {
			break
		}

		keyStr := string(entry.Key)
		if keyStr >= string(start) && keyStr < string(end) {
			if !entry.Deleted {
				results = append(results, entry)
			}
		}

		if keyStr >= string(end) {
			break
		}
	}

	return results, nil
}

// Iterator returns all entries
func (sst *SSTable) Iterator() ([]*Entry, error) {
	if _, err := sst.file.Seek(int64(binary.Size(sst.header)), 0); err != nil {
		return nil, err
	}

	// Note: bufio.NewReader does not return an error - it always succeeds
	reader := bufio.NewReader(sst.file)
	entries := make([]*Entry, 0, sst.entryCount)

	for i := 0; i < sst.entryCount; i++ {
		entry, err := readEntry(reader)
		if err != nil {
			break
		}
		entries = append(entries, entry)
	}

	return entries, nil
}
