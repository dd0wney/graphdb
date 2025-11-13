package storage

import (
	"fmt"
	"sync"
	"testing"
)

// TestEdgeCache_BasicLRU tests basic LRU eviction behavior
func TestEdgeCache_BasicLRU(t *testing.T) {
	cache := NewEdgeCache(3) // Max 3 entries

	edges1 := NewCompressedEdgeList([]uint64{1, 2, 3})
	edges2 := NewCompressedEdgeList([]uint64{4, 5, 6})
	edges3 := NewCompressedEdgeList([]uint64{7, 8, 9})
	edges4 := NewCompressedEdgeList([]uint64{10, 11, 12})

	// Add 3 entries (cache full)
	cache.Put("key1", edges1)
	cache.Put("key2", edges2)
	cache.Put("key3", edges3)

	if cache.Size() != 3 {
		t.Errorf("Cache size = %d, want 3", cache.Size())
	}

	// Add 4th entry - should evict oldest (key1)
	cache.Put("key4", edges4)

	if cache.Size() != 3 {
		t.Errorf("After eviction, cache size = %d, want 3", cache.Size())
	}

	// key1 should be evicted
	if val := cache.Get("key1"); val != nil {
		t.Error("key1 should have been evicted")
	}

	// key2, key3, key4 should still exist
	if val := cache.Get("key2"); val == nil {
		t.Error("key2 should exist")
	}
	if val := cache.Get("key3"); val == nil {
		t.Error("key3 should exist")
	}
	if val := cache.Get("key4"); val == nil {
		t.Error("key4 should exist")
	}
}

// TestEdgeCache_LRUOrdering tests that least recently used items are evicted
func TestEdgeCache_LRUOrdering(t *testing.T) {
	cache := NewEdgeCache(3)

	edges1 := NewCompressedEdgeList([]uint64{1})
	edges2 := NewCompressedEdgeList([]uint64{2})
	edges3 := NewCompressedEdgeList([]uint64{3})
	edges4 := NewCompressedEdgeList([]uint64{4})

	// Add 3 entries
	cache.Put("key1", edges1)
	cache.Put("key2", edges2)
	cache.Put("key3", edges3)

	// Access key1 (moves it to front of LRU)
	cache.Get("key1")

	// Now order is: key1 (most recent), key3, key2 (least recent)
	// Add key4 - should evict key2 (least recently used)
	cache.Put("key4", edges4)

	// key2 should be evicted
	if val := cache.Get("key2"); val != nil {
		t.Error("key2 should have been evicted (was least recently used)")
	}

	// key1, key3, key4 should exist
	if val := cache.Get("key1"); val == nil {
		t.Error("key1 should exist (was recently accessed)")
	}
	if val := cache.Get("key3"); val == nil {
		t.Error("key3 should exist")
	}
	if val := cache.Get("key4"); val == nil {
		t.Error("key4 should exist (just added)")
	}
}

// TestEdgeCache_Update tests updating existing keys
func TestEdgeCache_Update(t *testing.T) {
	cache := NewEdgeCache(3)

	edges1 := NewCompressedEdgeList([]uint64{1, 2, 3})
	edges2 := NewCompressedEdgeList([]uint64{4, 5, 6})

	cache.Put("key1", edges1)

	// Update key1
	cache.Put("key1", edges2)

	// Should still have 1 entry
	if cache.Size() != 1 {
		t.Errorf("Cache size = %d, want 1 (update shouldn't add entry)", cache.Size())
	}

	// Should get updated value
	retrieved := cache.Get("key1")
	if retrieved == nil {
		t.Fatal("key1 should exist")
	}

	decompressed := retrieved.Decompress()
	if len(decompressed) != 3 || decompressed[0] != 4 {
		t.Errorf("Updated value incorrect: %v", decompressed)
	}
}

// TestEdgeCache_HitRate tests cache hit/miss tracking
func TestEdgeCache_HitRate(t *testing.T) {
	cache := NewEdgeCache(10)

	edges := NewCompressedEdgeList([]uint64{1, 2, 3})
	cache.Put("key1", edges)

	// Reset stats
	cache.ResetStats()

	// 5 hits
	for i := 0; i < 5; i++ {
		cache.Get("key1")
	}

	// 3 misses
	for i := 0; i < 3; i++ {
		cache.Get("nonexistent")
	}

	hits, misses, hitRate := cache.Stats()

	if hits != 5 {
		t.Errorf("Hits = %d, want 5", hits)
	}

	if misses != 3 {
		t.Errorf("Misses = %d, want 3", misses)
	}

	expectedRate := 5.0 / 8.0 // 5 hits out of 8 total
	if hitRate < expectedRate-0.01 || hitRate > expectedRate+0.01 {
		t.Errorf("Hit rate = %.3f, want %.3f", hitRate, expectedRate)
	}
}

// TestEdgeCache_Clear tests clearing the cache
func TestEdgeCache_Clear(t *testing.T) {
	cache := NewEdgeCache(10)

	// Add some entries
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		edges := NewCompressedEdgeList([]uint64{uint64(i)})
		cache.Put(key, edges)
	}

	if cache.Size() != 5 {
		t.Errorf("Before clear, size = %d, want 5", cache.Size())
	}

	// Clear
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("After clear, size = %d, want 0", cache.Size())
	}

	// All keys should be gone
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("key%d", i)
		if val := cache.Get(key); val != nil {
			t.Errorf("%s should not exist after clear", key)
		}
	}
}

// TestEdgeCache_Concurrent tests thread-safe operations
func TestEdgeCache_Concurrent(t *testing.T) {
	cache := NewEdgeCache(100)

	const numGoroutines = 10
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent writers
	for i := 0; i < numGoroutines/2; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				edges := NewCompressedEdgeList([]uint64{uint64(j)})
				cache.Put(key, edges)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines/2; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id%2, j%10)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// If we got here without panics, concurrency is working
}

// TestEdgeCache_MaxSize tests cache respects max size limit
func TestEdgeCache_MaxSize(t *testing.T) {
	maxSize := 10
	cache := NewEdgeCache(maxSize)

	// Add more than max size
	for i := 0; i < maxSize*2; i++ {
		key := fmt.Sprintf("key%d", i)
		edges := NewCompressedEdgeList([]uint64{uint64(i)})
		cache.Put(key, edges)
	}

	// Cache should never exceed max size
	if cache.Size() > maxSize {
		t.Errorf("Cache size = %d, exceeds max size %d", cache.Size(), maxSize)
	}

	// Should be exactly max size
	if cache.Size() != maxSize {
		t.Errorf("Cache size = %d, want %d", cache.Size(), maxSize)
	}
}

// TestEdgeCache_EmptyCache tests operations on empty cache
func TestEdgeCache_EmptyCache(t *testing.T) {
	cache := NewEdgeCache(10)

	// Get from empty cache
	val := cache.Get("nonexistent")
	if val != nil {
		t.Error("Get on empty cache should return nil")
	}

	// Size should be 0
	if cache.Size() != 0 {
		t.Errorf("Empty cache size = %d, want 0", cache.Size())
	}

	// Hit rate should be 0
	_, _, hitRate := cache.Stats()
	if hitRate != 0.0 {
		t.Errorf("Empty cache hit rate = %.3f, want 0.0", hitRate)
	}
}

// TestEdgeCache_SingleEntry tests cache with capacity of 1
func TestEdgeCache_SingleEntry(t *testing.T) {
	cache := NewEdgeCache(1)

	edges1 := NewCompressedEdgeList([]uint64{1})
	edges2 := NewCompressedEdgeList([]uint64{2})

	cache.Put("key1", edges1)
	if cache.Size() != 1 {
		t.Errorf("Size = %d, want 1", cache.Size())
	}

	// Add second entry - should evict first
	cache.Put("key2", edges2)
	if cache.Size() != 1 {
		t.Errorf("Size = %d, want 1", cache.Size())
	}

	// key1 should be gone
	if val := cache.Get("key1"); val != nil {
		t.Error("key1 should have been evicted")
	}

	// key2 should exist
	if val := cache.Get("key2"); val == nil {
		t.Error("key2 should exist")
	}
}

// TestEdgeCache_ZeroSize tests cache with size 0 (no caching)
func TestEdgeCache_ZeroSize(t *testing.T) {
	cache := NewEdgeCache(0)

	edges := NewCompressedEdgeList([]uint64{1, 2, 3})
	cache.Put("key1", edges)

	// Should not store anything
	if val := cache.Get("key1"); val != nil {
		t.Error("Cache with size 0 should not store anything")
	}

	if cache.Size() != 0 {
		t.Errorf("Zero-size cache has size %d, want 0", cache.Size())
	}
}
