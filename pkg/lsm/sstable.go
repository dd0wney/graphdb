package lsm

import (
	"bufio"
	"encoding/binary"
	"os"
	"sort"
)

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
	idx := sst.findIndexPosition(key)

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

	// Note: bufio.NewReader does not return an error - it always succeeds
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

// findIndexPosition returns the index position for binary search
func (sst *SSTable) findIndexPosition(key []byte) int {
	return sort.Search(len(sst.index), func(i int) bool {
		return string(sst.index[i].Key) >= string(key)
	})
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
	_ = sst.Close()
	return os.Remove(sst.path)
}
