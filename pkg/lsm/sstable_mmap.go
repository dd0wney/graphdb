package lsm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"

	"golang.org/x/exp/mmap"
)

// MappedSSTable is a memory-mapped SSTable for fast reads
type MappedSSTable struct {
	path       string
	mmap       *mmap.ReaderAt
	header     SSTableHeader
	index      []IndexEntry
	bloom      *BloomFilter
	entryCount int
}

// OpenMappedSSTable opens an SSTable using memory-mapped I/O
func OpenMappedSSTable(path string) (*MappedSSTable, error) {
	// Open file with mmap
	reader, err := mmap.Open(path)
	if err != nil {
		return nil, err
	}

	// Read header from mmap
	headerSize := binary.Size(SSTableHeader{})
	headerBuf := make([]byte, headerSize)
	if _, err := reader.ReadAt(headerBuf, 0); err != nil {
		reader.Close()
		return nil, err
	}

	var header SSTableHeader
	if err := binary.Read(bytes.NewReader(headerBuf), binary.LittleEndian, &header); err != nil {
		reader.Close()
		return nil, err
	}

	if header.Magic != SSTableMagic {
		reader.Close()
		return nil, fmt.Errorf("invalid SSTable magic: %x", header.Magic)
	}

	// Read index from mmap
	index, err := readIndexFromMmap(reader, int64(header.IndexOffset))
	if err != nil {
		reader.Close()
		return nil, err
	}

	// Read Bloom filter from mmap
	// Position after index
	bloomPos := int64(header.IndexOffset)
	bloomPos += 4 // index count
	for _, ie := range index {
		bloomPos += 4 + int64(len(ie.Key)) + 8 // keyLen + key + offset
	}

	var bloomSize uint32
	bloomSizeBuf := make([]byte, 4)
	if _, err := reader.ReadAt(bloomSizeBuf, bloomPos); err != nil {
		// Old format without bloom filter - create empty one
		bloom := NewBloomFilter(int(header.EntryCount), 0.01)
		return &MappedSSTable{
			path:       path,
			mmap:       reader,
			header:     header,
			index:      index,
			bloom:      bloom,
			entryCount: int(header.EntryCount),
		}, nil
	}

	binary.Read(bytes.NewReader(bloomSizeBuf), binary.LittleEndian, &bloomSize)

	bloomData := make([]byte, bloomSize)
	if _, err := reader.ReadAt(bloomData, bloomPos+4); err != nil {
		reader.Close()
		return nil, err
	}

	bloom := NewBloomFilter(int(header.EntryCount), 0.01)
	if err := bloom.UnmarshalBinary(bloomData); err != nil {
		reader.Close()
		return nil, err
	}

	return &MappedSSTable{
		path:       path,
		mmap:       reader,
		header:     header,
		index:      index,
		bloom:      bloom,
		entryCount: int(header.EntryCount),
	}, nil
}

// Get retrieves a value by key using memory-mapped I/O
func (sst *MappedSSTable) Get(key []byte) (*Entry, bool) {
	// Check Bloom filter first - fast negative lookup
	if sst.bloom != nil && !sst.bloom.MayContain(key) {
		return nil, false // Definitely not in this SSTable
	}

	// Binary search in sparse index
	idx := sort.Search(len(sst.index), func(i int) bool {
		return string(sst.index[i].Key) >= string(key)
	})

	// Start from previous index entry
	startOffset := int64(binary.Size(sst.header))
	maxEntries := sst.entryCount
	if idx > 0 {
		startOffset = int64(sst.index[idx-1].Offset)
		maxEntries = IndexInterval * 2 // Scan up to 2 index blocks
	}

	// Scan entries directly from mmap
	offset := startOffset
	for i := 0; i < maxEntries; i++ {
		entry, bytesRead, err := readEntryFromMmap(sst.mmap, offset)
		if err != nil {
			return nil, false
		}

		cmp := string(entry.Key)
		if cmp == string(key) {
			if entry.Deleted {
				return nil, false
			}
			return entry, true
		}

		if cmp > string(key) {
			return nil, false
		}

		offset += int64(bytesRead)
	}

	return nil, false
}

// Scan returns entries in range [start, end)
func (sst *MappedSSTable) Scan(start, end []byte) ([]*Entry, error) {
	// Find starting position
	idx := sort.Search(len(sst.index), func(i int) bool {
		return string(sst.index[i].Key) >= string(start)
	})

	startOffset := int64(binary.Size(sst.header))
	if idx > 0 {
		startOffset = int64(sst.index[idx-1].Offset)
	}

	results := make([]*Entry, 0)
	offset := startOffset

	for {
		entry, bytesRead, err := readEntryFromMmap(sst.mmap, offset)
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

		offset += int64(bytesRead)
	}

	return results, nil
}

// Iterator returns all entries
func (sst *MappedSSTable) Iterator() ([]*Entry, error) {
	entries := make([]*Entry, 0, sst.entryCount)
	offset := int64(binary.Size(sst.header))

	for i := 0; i < sst.entryCount; i++ {
		entry, bytesRead, err := readEntryFromMmap(sst.mmap, offset)
		if err != nil {
			break
		}
		entries = append(entries, entry)
		offset += int64(bytesRead)
	}

	return entries, nil
}

// Close closes the memory-mapped file
func (sst *MappedSSTable) Close() error {
	if sst.mmap != nil {
		return sst.mmap.Close()
	}
	return nil
}

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
	binary.Read(bytes.NewReader(keyLenBuf), binary.LittleEndian, &keyLen)
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
	binary.Read(bytes.NewReader(valueLenBuf), binary.LittleEndian, &valueLen)
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
	binary.Read(bytes.NewReader(timestampBuf), binary.LittleEndian, &timestamp)
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
	binary.Read(bytes.NewReader(countBuf), binary.LittleEndian, &count)
	offset += 4

	index := make([]IndexEntry, count)

	for i := uint32(0); i < count; i++ {
		// Read key length
		keyLenBuf := make([]byte, 4)
		if _, err := r.ReadAt(keyLenBuf, offset); err != nil {
			return nil, err
		}
		var keyLen uint32
		binary.Read(bytes.NewReader(keyLenBuf), binary.LittleEndian, &keyLen)
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
		binary.Read(bytes.NewReader(offsetBuf), binary.LittleEndian, &entryOffset)
		offset += 8

		index[i] = IndexEntry{
			Key:    key,
			Offset: entryOffset,
		}
	}

	return index, nil
}

// Ensure MappedSSTable implements common interface
var _ io.Closer = (*MappedSSTable)(nil)
