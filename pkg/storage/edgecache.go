package storage

import (
	"container/list"
	"sync"
)

// EdgeCache implements an LRU cache for CompressedEdgeList
type EdgeCache struct {
	maxSize int
	cache   map[string]*cacheEntry
	lru     *list.List
	mu      sync.RWMutex
	hits    uint64
	misses  uint64
}

// cacheEntry represents a single cache entry
type cacheEntry struct {
	key     string
	value   *CompressedEdgeList
	element *list.Element
}

// NewEdgeCache creates a new LRU cache with the specified maximum size
func NewEdgeCache(maxSize int) *EdgeCache {
	return &EdgeCache{
		maxSize: maxSize,
		cache:   make(map[string]*cacheEntry),
		lru:     list.New(),
	}
}

// Get retrieves a value from the cache
// Returns nil if not found
func (ec *EdgeCache) Get(key string) *CompressedEdgeList {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	entry, exists := ec.cache[key]
	if !exists {
		ec.misses++
		return nil
	}

	// Move to front of LRU list (most recently used)
	ec.lru.MoveToFront(entry.element)
	ec.hits++

	return entry.value
}

// Put adds or updates a value in the cache
func (ec *EdgeCache) Put(key string, value *CompressedEdgeList) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	// Check if key already exists
	if entry, exists := ec.cache[key]; exists {
		// Update existing entry
		entry.value = value
		ec.lru.MoveToFront(entry.element)
		return
	}

	// Add new entry
	entry := &cacheEntry{
		key:   key,
		value: value,
	}

	entry.element = ec.lru.PushFront(entry)
	ec.cache[key] = entry

	// Evict if over capacity
	if ec.lru.Len() > ec.maxSize {
		ec.evictOldest()
	}
}

// evictOldest removes the least recently used entry
func (ec *EdgeCache) evictOldest() {
	oldest := ec.lru.Back()
	if oldest == nil {
		return
	}

	entry := oldest.Value.(*cacheEntry)
	ec.lru.Remove(oldest)
	delete(ec.cache, entry.key)
}

// Clear removes all entries from the cache
func (ec *EdgeCache) Clear() {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	ec.cache = make(map[string]*cacheEntry)
	ec.lru = list.New()
}

// Size returns the current number of entries in the cache
func (ec *EdgeCache) Size() int {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	return ec.lru.Len()
}

// HitRate returns the cache hit rate (0.0 - 1.0)
func (ec *EdgeCache) HitRate() float64 {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	total := ec.hits + ec.misses
	if total == 0 {
		return 0.0
	}

	return float64(ec.hits) / float64(total)
}

// Stats returns cache statistics
func (ec *EdgeCache) Stats() (hits, misses uint64, hitRate float64) {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	return ec.hits, ec.misses, ec.HitRate()
}

// ResetStats resets hit/miss counters
func (ec *EdgeCache) ResetStats() {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	ec.hits = 0
	ec.misses = 0
}
