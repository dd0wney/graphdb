package lsm

import (
	"github.com/dd0wney/graphdb/pkg/vfs"
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

	// MaxEntryFieldSize bounds a key or value length read from a file.
	//
	// The format stores each length as a uint32, so a corrupt or truncated
	// SSTable can ask for up to 4 GiB per field, and readEntry passed that
	// number straight to make(). This is the defect #477 fixed for the mmap
	// snapshot decoders; the SSTable decoder had it too. The bound is far
	// above any legitimate entry and far below what an allocator will accept
	// without complaint.
	MaxEntryFieldSize = 64 << 20
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
	// fs is the filesystem driver this table was opened on. It travels with
	// the table so a later Close, Remove or Stat uses the same one.
	fs         vfs.FileSystem
	path       string
	file       vfs.File
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
