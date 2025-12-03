package lsm

import (
	"bytes"
	"encoding/binary"
	"sort"
)

// Get retrieves a value by key using memory-mapped I/O
func (sst *MappedSSTable) Get(key []byte) (*Entry, bool) {
	// Check Bloom filter first - fast negative lookup
	if sst.bloom != nil && !sst.bloom.MayContain(key) {
		return nil, false // Definitely not in this SSTable
	}

	// Binary search in sparse index (zero-allocation)
	idx := sort.Search(len(sst.index), func(i int) bool {
		return bytes.Compare(sst.index[i].Key, key) >= 0
	})

	// Start from previous index entry
	startOffset := int64(binary.Size(sst.header))
	maxEntries := IndexInterval // Never scan more than one index block
	if idx > 0 {
		startOffset = int64(sst.index[idx-1].Offset)
		maxEntries = IndexInterval // Scan one index block (100 entries max)
	}

	// Scan entries directly from mmap
	offset := startOffset
	for i := 0; i < maxEntries; i++ {
		entry, bytesRead, err := readEntryFromMmap(sst.mmap, offset)
		if err != nil {
			return nil, false
		}

		// Use byte comparison to avoid allocations (critical for performance)
		cmp := bytes.Compare(entry.Key, key)
		if cmp == 0 {
			if entry.Deleted {
				return nil, false
			}
			return entry, true
		}

		if cmp > 0 {
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
