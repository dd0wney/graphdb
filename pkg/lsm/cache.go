package lsm

import (
	"container/list"
	"sync"
)

// BlockCache is an LRU cache for frequently accessed data blocks
type BlockCache struct {
	mu       sync.RWMutex
	capacity int
	cache    map[string]*list.Element
	lru      *list.List

	// Statistics
	hits   int64
	misses int64
}

type cacheEntry struct {
	key   string
	value []byte
}

// NewBlockCache creates a new LRU block cache
func NewBlockCache(capacity int) *BlockCache {
	return &BlockCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		lru:      list.New(),
	}
}

// Get retrieves a value from the cache
func (bc *BlockCache) Get(key string) ([]byte, bool) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if elem, ok := bc.cache[key]; ok {
		// Move to front (most recently used)
		bc.lru.MoveToFront(elem)
		bc.hits++
		return elem.Value.(*cacheEntry).value, true
	}

	bc.misses++
	return nil, false
}

// Put adds a value to the cache
func (bc *BlockCache) Put(key string, value []byte) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Check if key already exists
	if elem, ok := bc.cache[key]; ok {
		// Update value and move to front
		bc.lru.MoveToFront(elem)
		elem.Value.(*cacheEntry).value = value
		return
	}

	// Add new entry
	entry := &cacheEntry{
		key:   key,
		value: value,
	}
	elem := bc.lru.PushFront(entry)
	bc.cache[key] = elem

	// Evict if over capacity
	if bc.lru.Len() > bc.capacity {
		bc.evict()
	}
}

// evict removes the least recently used entry
func (bc *BlockCache) evict() {
	elem := bc.lru.Back()
	if elem != nil {
		bc.lru.Remove(elem)
		entry := elem.Value.(*cacheEntry)
		delete(bc.cache, entry.key)
	}
}

// Clear removes all entries from the cache
func (bc *BlockCache) Clear() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.cache = make(map[string]*list.Element)
	bc.lru = list.New()
	bc.hits = 0
	bc.misses = 0
}

// Stats returns cache statistics
func (bc *BlockCache) Stats() (hits, misses int64, hitRate float64) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	hits = bc.hits
	misses = bc.misses
	total := hits + misses
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}
	return
}

// Size returns the current number of entries
func (bc *BlockCache) Size() int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.lru.Len()
}

// Delete removes an entry from the cache
func (bc *BlockCache) Delete(key string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if elem, ok := bc.cache[key]; ok {
		bc.lru.Remove(elem)
		delete(bc.cache, key)
	}
}
