package lsm

import (
	"sync"
	"testing"
)

// TestNewBlockCache tests creating a new block cache
func TestNewBlockCache(t *testing.T) {
	cache := NewBlockCache(10)
	if cache == nil {
		t.Fatal("Expected non-nil cache")
	}

	if cache.capacity != 10 {
		t.Errorf("Expected capacity 10, got %d", cache.capacity)
	}

	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}
}

// TestBlockCache_PutGet tests basic put/get operations
func TestBlockCache_PutGet(t *testing.T) {
	cache := NewBlockCache(3)

	// Put and get
	cache.Put("key1", []byte("value1"))
	value, ok := cache.Get("key1")
	if !ok {
		t.Fatal("Expected key1 to be in cache")
	}

	if string(value) != "value1" {
		t.Errorf("Expected 'value1', got '%s'", string(value))
	}

	// Get non-existent key
	_, ok = cache.Get("key2")
	if ok {
		t.Error("Expected key2 to not be in cache")
	}
}

// TestBlockCache_Size tests the Size method
func TestBlockCache_Size(t *testing.T) {
	cache := NewBlockCache(5)

	// Empty cache
	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}

	// Add entries
	cache.Put("key1", []byte("value1"))
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	cache.Put("key2", []byte("value2"))
	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	cache.Put("key3", []byte("value3"))
	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}
}

// TestBlockCache_Eviction tests LRU eviction when capacity is exceeded
func TestBlockCache_Eviction(t *testing.T) {
	cache := NewBlockCache(3)

	// Fill cache to capacity
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Add one more - should evict key1 (least recently used)
	cache.Put("key4", []byte("value4"))

	if cache.Size() != 3 {
		t.Errorf("Expected size 3 after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be evicted")
	}

	// key2, key3, key4 should still be present
	_, ok = cache.Get("key2")
	if !ok {
		t.Error("Expected key2 to be in cache")
	}

	_, ok = cache.Get("key3")
	if !ok {
		t.Error("Expected key3 to be in cache")
	}

	_, ok = cache.Get("key4")
	if !ok {
		t.Error("Expected key4 to be in cache")
	}
}

// TestBlockCache_LRUOrdering tests that accessing entries updates their LRU position
func TestBlockCache_LRUOrdering(t *testing.T) {
	cache := NewBlockCache(3)

	// Fill cache
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Access key1 (makes it most recently used)
	cache.Get("key1")

	// Add key4 - should evict key2 (now least recently used)
	cache.Put("key4", []byte("value4"))

	// key2 should be evicted
	_, ok := cache.Get("key2")
	if ok {
		t.Error("Expected key2 to be evicted")
	}

	// key1 should still be present
	_, ok = cache.Get("key1")
	if !ok {
		t.Error("Expected key1 to be in cache")
	}
}

// TestBlockCache_Update tests updating an existing key
func TestBlockCache_Update(t *testing.T) {
	cache := NewBlockCache(3)

	// Put initial value
	cache.Put("key1", []byte("value1"))

	// Update with new value
	cache.Put("key1", []byte("value2"))

	// Should still have size 1
	if cache.Size() != 1 {
		t.Errorf("Expected size 1 after update, got %d", cache.Size())
	}

	// Should get updated value
	value, ok := cache.Get("key1")
	if !ok {
		t.Fatal("Expected key1 to be in cache")
	}

	if string(value) != "value2" {
		t.Errorf("Expected 'value2', got '%s'", string(value))
	}
}

// TestBlockCache_Clear tests clearing the cache
func TestBlockCache_Clear(t *testing.T) {
	cache := NewBlockCache(5)

	// Add entries
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	// Verify entries exist
	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Clear cache
	cache.Clear()

	// Verify cache is empty
	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", cache.Size())
	}

	// Verify entries are gone
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be cleared")
	}

	_, ok = cache.Get("key2")
	if ok {
		t.Error("Expected key2 to be cleared")
	}

	// Verify stats are reset
	hits, misses, _ := cache.Stats()
	if hits != 0 {
		t.Errorf("Expected 0 hits after clear, got %d", hits)
	}

	if misses != 2 { // The two Get calls above
		t.Errorf("Expected 2 misses after clear, got %d", misses)
	}
}

// TestBlockCache_Stats tests cache statistics tracking
func TestBlockCache_Stats(t *testing.T) {
	cache := NewBlockCache(5)

	// Initial stats
	hits, misses, hitRate := cache.Stats()
	if hits != 0 {
		t.Errorf("Expected 0 hits, got %d", hits)
	}
	if misses != 0 {
		t.Errorf("Expected 0 misses, got %d", misses)
	}
	if hitRate != 0 {
		t.Errorf("Expected 0 hit rate, got %f", hitRate)
	}

	// Add entry
	cache.Put("key1", []byte("value1"))

	// Miss
	cache.Get("key2")
	hits, misses, hitRate = cache.Stats()
	if hits != 0 {
		t.Errorf("Expected 0 hits, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
	if hitRate != 0 {
		t.Errorf("Expected 0 hit rate, got %f", hitRate)
	}

	// Hit
	cache.Get("key1")
	hits, misses, hitRate = cache.Stats()
	if hits != 1 {
		t.Errorf("Expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
	expectedRate := 1.0 / 2.0
	if hitRate != expectedRate {
		t.Errorf("Expected hit rate %f, got %f", expectedRate, hitRate)
	}

	// Multiple hits
	cache.Get("key1")
	cache.Get("key1")
	hits, misses, hitRate = cache.Stats()
	if hits != 3 {
		t.Errorf("Expected 3 hits, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
	expectedRate = 3.0 / 4.0
	if hitRate != expectedRate {
		t.Errorf("Expected hit rate %f, got %f", expectedRate, hitRate)
	}
}

// TestBlockCache_Delete tests deleting entries
func TestBlockCache_Delete(t *testing.T) {
	cache := NewBlockCache(5)

	// Add entries
	cache.Put("key1", []byte("value1"))
	cache.Put("key2", []byte("value2"))
	cache.Put("key3", []byte("value3"))

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Delete key2
	cache.Delete("key2")

	if cache.Size() != 2 {
		t.Errorf("Expected size 2 after delete, got %d", cache.Size())
	}

	// Verify key2 is gone
	_, ok := cache.Get("key2")
	if ok {
		t.Error("Expected key2 to be deleted")
	}

	// Verify other keys still exist
	_, ok = cache.Get("key1")
	if !ok {
		t.Error("Expected key1 to still be in cache")
	}

	_, ok = cache.Get("key3")
	if !ok {
		t.Error("Expected key3 to still be in cache")
	}

	// Delete non-existent key (should not error)
	cache.Delete("key999")

	if cache.Size() != 2 {
		t.Errorf("Expected size 2 after deleting non-existent key, got %d", cache.Size())
	}
}

// TestBlockCache_Concurrent tests concurrent access to the cache
func TestBlockCache_Concurrent(t *testing.T) {
	cache := NewBlockCache(100)

	var wg sync.WaitGroup
	numGoroutines := 10
	opsPerGoroutine := 100

	// Concurrent puts
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := string(rune('A' + id))
				value := []byte{byte(i)}
				cache.Put(key, value)
			}
		}(g)
	}

	// Concurrent gets
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := string(rune('A' + id))
				cache.Get(key)
			}
		}(g)
	}

	// Concurrent stats
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				cache.Stats()
				cache.Size()
			}
		}()
	}

	wg.Wait()

	// Verify cache is still functional
	cache.Put("test", []byte("value"))
	value, ok := cache.Get("test")
	if !ok {
		t.Fatal("Expected cache to be functional after concurrent access")
	}

	if string(value) != "value" {
		t.Errorf("Expected 'value', got '%s'", string(value))
	}
}

// TestBlockCache_EmptyCacheOperations tests operations on an empty cache
func TestBlockCache_EmptyCacheOperations(t *testing.T) {
	cache := NewBlockCache(5)

	// Get from empty cache
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected miss on empty cache")
	}

	// Delete from empty cache (should not panic)
	cache.Delete("key1")

	// Clear empty cache (should not panic)
	cache.Clear()

	// Stats after clear should be reset to 0
	hits, misses, hitRate := cache.Stats()
	if hits != 0 || misses != 0 || hitRate != 0 {
		t.Errorf("Unexpected stats after clear: hits=%d, misses=%d, hitRate=%f", hits, misses, hitRate)
	}

	// Size of empty cache
	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}
}

// TestBlockCache_CapacityOne tests cache with capacity of 1
func TestBlockCache_CapacityOne(t *testing.T) {
	cache := NewBlockCache(1)

	// Add first entry
	cache.Put("key1", []byte("value1"))
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	// Add second entry - should evict first
	cache.Put("key2", []byte("value2"))
	if cache.Size() != 1 {
		t.Errorf("Expected size 1 after eviction, got %d", cache.Size())
	}

	// key1 should be evicted
	_, ok := cache.Get("key1")
	if ok {
		t.Error("Expected key1 to be evicted")
	}

	// key2 should be present
	value, ok := cache.Get("key2")
	if !ok {
		t.Fatal("Expected key2 to be in cache")
	}

	if string(value) != "value2" {
		t.Errorf("Expected 'value2', got '%s'", string(value))
	}
}

// TestBlockCache_LargeValues tests caching large values
func TestBlockCache_LargeValues(t *testing.T) {
	cache := NewBlockCache(3)

	// Create large values
	largeValue1 := make([]byte, 10000)
	for i := range largeValue1 {
		largeValue1[i] = byte(i % 256)
	}

	largeValue2 := make([]byte, 20000)
	for i := range largeValue2 {
		largeValue2[i] = byte((i + 1) % 256)
	}

	// Put large values
	cache.Put("large1", largeValue1)
	cache.Put("large2", largeValue2)

	// Get and verify
	value1, ok := cache.Get("large1")
	if !ok {
		t.Fatal("Expected large1 to be in cache")
	}

	if len(value1) != len(largeValue1) {
		t.Errorf("Expected length %d, got %d", len(largeValue1), len(value1))
	}

	// Verify first and last bytes
	if value1[0] != largeValue1[0] || value1[len(value1)-1] != largeValue1[len(largeValue1)-1] {
		t.Error("Large value corruption detected")
	}
}
