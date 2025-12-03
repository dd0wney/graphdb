package lsm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

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
		_ = reader.Close()
		return nil, err
	}

	var header SSTableHeader
	if err := binary.Read(bytes.NewReader(headerBuf), binary.LittleEndian, &header); err != nil {
		_ = reader.Close()
		return nil, err
	}

	if header.Magic != SSTableMagic {
		_ = reader.Close()
		return nil, fmt.Errorf("invalid SSTable magic: %x", header.Magic)
	}

	// Read index from mmap
	index, err := readIndexFromMmap(reader, int64(header.IndexOffset))
	if err != nil {
		_ = reader.Close()
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

	if err := binary.Read(bytes.NewReader(bloomSizeBuf), binary.LittleEndian, &bloomSize); err != nil {
		_ = reader.Close()
		return nil, fmt.Errorf("failed to read bloom size: %w", err)
	}

	bloomData := make([]byte, bloomSize)
	if _, err := reader.ReadAt(bloomData, bloomPos+4); err != nil {
		_ = reader.Close()
		return nil, err
	}

	bloom := NewBloomFilter(int(header.EntryCount), 0.01)
	if err := bloom.UnmarshalBinary(bloomData); err != nil {
		_ = reader.Close()
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

// Close closes the memory-mapped file
func (sst *MappedSSTable) Close() error {
	if sst.mmap != nil {
		return sst.mmap.Close()
	}
	return nil
}

// Ensure MappedSSTable implements common interface
var _ io.Closer = (*MappedSSTable)(nil)
