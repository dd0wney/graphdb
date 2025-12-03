package lsm

import (
	"os"
)

// SSTable format:
//   [Header: magic(4) | version(4) | entry_count(8) | index_offset(8)]
//   [Data Block: entries in sorted order]
//   [Index Block: sparse index every N keys]
//   [Footer: bloom_filter | crc32(4)]

const (
	SSTableMagic   = 0x53535442 // "SSTB"
	SSTableVersion = 1
	IndexInterval  = 128 // Create index entry every N keys
)

// SSTableHeader represents the header of an SSTable file
type SSTableHeader struct {
	Magic       uint32
	Version     uint32
	EntryCount  uint64
	IndexOffset uint64
}

// SSTable represents a Sorted String Table on disk
type SSTable struct {
	path       string
	file       *os.File
	header     SSTableHeader
	index      []IndexEntry // Sparse index
	bloom      *BloomFilter // Bloom filter for fast negative lookups
	entryCount int
}

// IndexEntry represents an entry in the sparse index
type IndexEntry struct {
	Key    []byte
	Offset uint64
}
