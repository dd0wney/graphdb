package lsm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// SSTable represents a Sorted String Table on disk
// Format:
//   [Header: magic(4) | version(4) | entry_count(8) | index_offset(8)]
//   [Data Block: entries in sorted order]
//   [Index Block: sparse index every N keys]
//   [Footer: bloom_filter | crc32(4)]

const (
	SSTableMagic   = 0x53535442 // "SSTB"
	SSTableVersion = 1
	IndexInterval  = 128 // Create index entry every N keys
)

type SSTableHeader struct {
	Magic       uint32
	Version     uint32
	EntryCount  uint64
	IndexOffset uint64
}

type SSTable struct {
	path       string
	file       *os.File
	header     SSTableHeader
	index      []IndexEntry // Sparse index
	bloom      *BloomFilter // Bloom filter for fast negative lookups
	entryCount int
}

type IndexEntry struct {
	Key    []byte
	Offset uint64
}

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

	writer := bufio.NewWriter(file)

	// Write header (placeholder, update later)
	header := SSTableHeader{
		Magic:      SSTableMagic,
		Version:    SSTableVersion,
		EntryCount: uint64(len(entries)),
	}

	if err := binary.Write(writer, binary.LittleEndian, &header); err != nil {
		file.Close()
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
			file.Close()
			return nil, err
		}

		// Check for offset overflow (extremely unlikely but defensive)
		newOffset := offset + uint64(entrySize)
		if newOffset < offset {
			file.Close()
			return nil, fmt.Errorf("SSTable offset overflow: file too large")
		}
		offset = newOffset
	}

	// Store index offset
	indexOffset := offset
	header.IndexOffset = indexOffset

	// Write index
	if err := writeIndex(writer, index); err != nil {
		file.Close()
		return nil, err
	}

	// Write Bloom filter
	bloomData := bloom.MarshalBinary()
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(bloomData))); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := writer.Write(bloomData); err != nil {
		file.Close()
		return nil, err
	}

	// Write footer with CRC
	crc := crc32.ChecksumIEEE([]byte{}) // TODO: Calculate actual CRC
	if err := binary.Write(writer, binary.LittleEndian, crc); err != nil {
		file.Close()
		return nil, err
	}

	if err := writer.Flush(); err != nil {
		file.Close()
		return nil, err
	}

	// Update header with index offset
	if _, err := file.Seek(0, 0); err != nil {
		file.Close()
		return nil, err
	}

	if err := binary.Write(file, binary.LittleEndian, &header); err != nil {
		file.Close()
		return nil, err
	}

	if err := file.Sync(); err != nil {
		file.Close()
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

// OpenSSTable opens an existing SSTable
func OpenSSTable(path string) (*SSTable, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Read header
	var header SSTableHeader
	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		file.Close()
		return nil, err
	}

	if header.Magic != SSTableMagic {
		file.Close()
		return nil, fmt.Errorf("invalid SSTable magic: %x", header.Magic)
	}

	// Read index
	if _, err := file.Seek(int64(header.IndexOffset), 0); err != nil {
		file.Close()
		return nil, err
	}

	index, err := readIndex(file)
	if err != nil {
		file.Close()
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
		file.Close()
		return nil, err
	}

	bloom := NewBloomFilter(int(header.EntryCount), 0.01)
	if err := bloom.UnmarshalBinary(bloomData); err != nil {
		file.Close()
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

// Get retrieves a value by key
func (sst *SSTable) Get(key []byte) (*Entry, bool) {
	// Check Bloom filter first - fast negative lookup
	if sst.bloom != nil && !sst.bloom.MayContain(key) {
		return nil, false // Definitely not in this SSTable
	}

	// Open a new file handle for concurrent reads
	file, err := os.Open(sst.path)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	// Binary search in sparse index
	idx := sort.Search(len(sst.index), func(i int) bool {
		return string(sst.index[i].Key) >= string(key)
	})

	// Start from previous index entry
	startOffset := uint64(binary.Size(sst.header))
	maxEntries := sst.entryCount
	if idx > 0 {
		startOffset = sst.index[idx-1].Offset
		maxEntries = IndexInterval * 2 // Scan up to 2 index blocks
	}

	// Seek to start position
	if _, err := file.Seek(int64(startOffset), 0); err != nil {
		return nil, false
	}

	reader := bufio.NewReader(file)

	// Scan entries until we find the key or pass it
	for i := 0; i < maxEntries; i++ {
		entry, err := readEntry(reader)
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
	}

	return nil, false
}

// Scan returns entries in range [start, end)
func (sst *SSTable) Scan(start, end []byte) ([]*Entry, error) {
	// Find starting position
	idx := sort.Search(len(sst.index), func(i int) bool {
		return string(sst.index[i].Key) >= string(start)
	})

	startOffset := uint64(binary.Size(sst.header))
	if idx > 0 {
		startOffset = sst.index[idx-1].Offset
	}

	if _, err := sst.file.Seek(int64(startOffset), 0); err != nil {
		return nil, err
	}

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

// Close closes the SSTable file
func (sst *SSTable) Close() error {
	if sst.file != nil {
		return sst.file.Close()
	}
	return nil
}

// Delete removes the SSTable file
func (sst *SSTable) Delete() error {
	sst.Close()
	return os.Remove(sst.path)
}

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

		// Key
		key := make([]byte, keyLen)
		if _, err := r.Read(key); err != nil {
			return nil, err
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
