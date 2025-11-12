package lsm

import (
	"bytes"
	"sort"
	"sync"
	"time"
)

// Entry represents a key-value pair with metadata
type Entry struct {
	Key       []byte
	Value     []byte
	Timestamp int64
	Deleted   bool // Tombstone for deletions
}

// MemTable is an in-memory write buffer using a sorted map
type MemTable struct {
	mu      sync.RWMutex
	data    map[string]*Entry // Key -> Entry
	keys    []string          // Sorted keys for iteration
	size    int               // Approximate size in bytes
	maxSize int               // Max size before flush
	sorted  bool              // Whether keys are sorted
}

// NewMemTable creates a new MemTable
func NewMemTable(maxSize int) *MemTable {
	return &MemTable{
		data:    make(map[string]*Entry),
		keys:    make([]string, 0),
		maxSize: maxSize,
		sorted:  true,
	}
}

// Put adds or updates a key-value pair
func (mt *MemTable) Put(key, value []byte) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	keyStr := string(key)

	// Update size estimate (with underflow protection)
	if existing, exists := mt.data[keyStr]; exists {
		// Subtract old value size carefully to prevent underflow
		oldSize := len(existing.Value)
		if mt.size >= oldSize {
			mt.size -= oldSize
		} else {
			mt.size = 0 // Reset if inconsistent
		}
	} else {
		mt.keys = append(mt.keys, keyStr)
		mt.sorted = false
		mt.size += len(key)
	}

	mt.size += len(value)

	mt.data[keyStr] = &Entry{
		Key:       key,
		Value:     value,
		Timestamp: time.Now().UnixNano(),
		Deleted:   false,
	}

	return nil
}

// Get retrieves a value by key
func (mt *MemTable) Get(key []byte) (*Entry, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	entry, exists := mt.data[string(key)]
	if !exists || entry.Deleted {
		return nil, false
	}

	return entry, true
}

// Delete marks a key as deleted (tombstone)
func (mt *MemTable) Delete(key []byte) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	keyStr := string(key)

	if existing, exists := mt.data[keyStr]; exists {
		existing.Deleted = true
		existing.Timestamp = time.Now().UnixNano()
	} else {
		// Create tombstone
		mt.keys = append(mt.keys, keyStr)
		mt.sorted = false
		mt.data[keyStr] = &Entry{
			Key:       key,
			Timestamp: time.Now().UnixNano(),
			Deleted:   true,
		}
	}

	return nil
}

// Size returns the approximate size in bytes
func (mt *MemTable) Size() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.size
}

// IsFull returns true if MemTable should be flushed
func (mt *MemTable) IsFull() bool {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.size >= mt.maxSize
}

// Iterator returns all entries in sorted order
func (mt *MemTable) Iterator() []*Entry {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Sort keys if needed
	if !mt.sorted {
		sort.Strings(mt.keys)
		mt.sorted = true
	}

	entries := make([]*Entry, 0, len(mt.keys))
	for _, key := range mt.keys {
		entries = append(entries, mt.data[key])
	}

	return entries
}

// Scan returns entries in range [start, end)
func (mt *MemTable) Scan(start, end []byte) []*Entry {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	if !mt.sorted {
		sort.Strings(mt.keys)
		mt.sorted = true
	}

	startStr := string(start)
	endStr := string(end)

	results := make([]*Entry, 0)
	for _, key := range mt.keys {
		if key >= startStr && key < endStr {
			entry := mt.data[key]
			if !entry.Deleted {
				results = append(results, entry)
			}
		}
		if key >= endStr {
			break
		}
	}

	return results
}

// Clear removes all entries (used after flush)
func (mt *MemTable) Clear() {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.data = make(map[string]*Entry)
	mt.keys = make([]string, 0)
	mt.size = 0
	mt.sorted = true
}

// EntryCompare compares two entries by key
func EntryCompare(a, b *Entry) int {
	return bytes.Compare(a.Key, b.Key)
}
