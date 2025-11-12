package lsm

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
)

// TestMemTable_BasicOperations tests basic Put/Get/Delete
func TestMemTable_BasicOperations(t *testing.T) {
	mt := NewMemTable(1024)

	// Put a value
	key := []byte("testkey")
	value := []byte("testvalue")
	err := mt.Put(key, value)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get the value
	entry, found := mt.Get(key)
	if !found {
		t.Fatal("Expected to find key")
	}
	if !bytes.Equal(entry.Value, value) {
		t.Errorf("Expected value %s, got %s", value, entry.Value)
	}

	// Delete the key
	err = mt.Delete(key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Get should return not found after delete
	_, found = mt.Get(key)
	if found {
		t.Error("Expected key to be deleted")
	}
}

// TestMemTable_UpdateValue tests updating existing keys
func TestMemTable_UpdateValue(t *testing.T) {
	mt := NewMemTable(1024)

	key := []byte("key")
	value1 := []byte("value1")
	value2 := []byte("value2-longer")

	// Put initial value
	mt.Put(key, value1)

	// Update with new value
	mt.Put(key, value2)

	// Should get latest value
	entry, found := mt.Get(key)
	if !found {
		t.Fatal("Expected to find key")
	}
	if !bytes.Equal(entry.Value, value2) {
		t.Errorf("Expected updated value %s, got %s", value2, entry.Value)
	}

	// Verify size calculation handles updates correctly
	size := mt.Size()
	expectedSize := len(key) + len(value2)
	if size < expectedSize {
		t.Errorf("Size underflow: expected at least %d, got %d", expectedSize, size)
	}
}

// TestMemTable_SizeTracking tests size tracking with underflow protection
func TestMemTable_SizeTracking(t *testing.T) {
	mt := NewMemTable(1024)

	key := []byte("key")
	largeValue := make([]byte, 100)
	smallValue := []byte("small")

	// Put large value
	mt.Put(key, largeValue)
	initialSize := mt.Size()

	// Update with smaller value
	mt.Put(key, smallValue)
	updatedSize := mt.Size()

	// Size should decrease but never go negative
	if updatedSize > initialSize {
		t.Errorf("Size should decrease after updating with smaller value")
	}
	if updatedSize < 0 {
		t.Error("Size should never be negative (underflow protection failed)")
	}
}

// TestMemTable_IsFull tests max size detection
func TestMemTable_IsFull(t *testing.T) {
	maxSize := 100
	mt := NewMemTable(maxSize)

	// Add data until full
	for i := 0; i < 20; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte("value")
		mt.Put(key, value)

		if mt.IsFull() {
			// Should trigger when size >= maxSize
			if mt.Size() < maxSize {
				t.Errorf("IsFull() returned true but size %d < maxSize %d", mt.Size(), maxSize)
			}
			return
		}
	}

	// If we added enough data, it should have become full
	if mt.Size() >= maxSize && !mt.IsFull() {
		t.Error("Expected MemTable to be full")
	}
}

// TestMemTable_Iterator tests sorted iteration
func TestMemTable_Iterator(t *testing.T) {
	mt := NewMemTable(1024)

	// Add keys in random order
	keys := []string{"zebra", "apple", "mango", "banana"}
	for _, k := range keys {
		mt.Put([]byte(k), []byte("value"))
	}

	// Iterator should return sorted entries
	entries := mt.Iterator()

	expectedOrder := []string{"apple", "banana", "mango", "zebra"}
	if len(entries) != len(expectedOrder) {
		t.Fatalf("Expected %d entries, got %d", len(expectedOrder), len(entries))
	}

	for i, entry := range entries {
		if string(entry.Key) != expectedOrder[i] {
			t.Errorf("Entry %d: expected key %s, got %s", i, expectedOrder[i], string(entry.Key))
		}
	}
}

// TestMemTable_Scan tests range scanning
func TestMemTable_Scan(t *testing.T) {
	mt := NewMemTable(1024)

	// Add keys
	keys := []string{"apple", "banana", "cherry", "date", "elderberry"}
	for _, k := range keys {
		mt.Put([]byte(k), []byte(k+"-value"))
	}

	// Scan range [banana, date)
	start := []byte("banana")
	end := []byte("date")
	results := mt.Scan(start, end)

	// Should include banana and cherry, exclude date
	expectedKeys := []string{"banana", "cherry"}
	if len(results) != len(expectedKeys) {
		t.Fatalf("Expected %d results, got %d", len(expectedKeys), len(results))
	}

	for i, entry := range results {
		if string(entry.Key) != expectedKeys[i] {
			t.Errorf("Result %d: expected key %s, got %s", i, expectedKeys[i], string(entry.Key))
		}
	}
}

// TestMemTable_DeletedEntriesInScan tests that deleted entries are excluded from scans
func TestMemTable_DeletedEntriesInScan(t *testing.T) {
	mt := NewMemTable(1024)

	// Add keys
	mt.Put([]byte("a"), []byte("value-a"))
	mt.Put([]byte("b"), []byte("value-b"))
	mt.Put([]byte("c"), []byte("value-c"))

	// Delete middle key
	mt.Delete([]byte("b"))

	// Scan should exclude deleted entry
	results := mt.Scan([]byte("a"), []byte("d"))

	// Should only have a and c
	if len(results) != 2 {
		t.Fatalf("Expected 2 results (excluding deleted), got %d", len(results))
	}

	for _, entry := range results {
		if string(entry.Key) == "b" {
			t.Error("Scan should not return deleted entry 'b'")
		}
	}
}

// TestMemTable_ConcurrentReadWrite tests thread-safe concurrent access
func TestMemTable_ConcurrentReadWrite(t *testing.T) {
	mt := NewMemTable(10000)

	var wg sync.WaitGroup
	numWriters := 5
	numReaders := 5
	opsPerGoroutine := 100

	// Concurrent writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("w%d-k%d", writerID, i))
				value := []byte(fmt.Sprintf("v%d", i))
				mt.Put(key, value)
			}
		}(w)
	}

	// Concurrent readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				// Try to read keys that might or might not exist
				key := []byte(fmt.Sprintf("w0-k%d", i))
				mt.Get(key) // Just verify no panic
			}
		}(r)
	}

	wg.Wait()

	// Verify data integrity
	for w := 0; w < numWriters; w++ {
		for i := 0; i < opsPerGoroutine; i++ {
			key := []byte(fmt.Sprintf("w%d-k%d", w, i))
			entry, found := mt.Get(key)
			if !found {
				t.Errorf("Expected to find key %s", key)
			}
			expectedValue := []byte(fmt.Sprintf("v%d", i))
			if !bytes.Equal(entry.Value, expectedValue) {
				t.Errorf("Value mismatch for key %s", key)
			}
		}
	}
}

// TestMemTable_EmptyScans tests edge cases for scanning
func TestMemTable_EmptyScans(t *testing.T) {
	mt := NewMemTable(1024)

	// Scan empty memtable
	results := mt.Scan([]byte("a"), []byte("z"))
	if len(results) != 0 {
		t.Errorf("Expected 0 results from empty memtable, got %d", len(results))
	}

	// Add one key
	mt.Put([]byte("m"), []byte("middle"))

	// Scan before the key
	results = mt.Scan([]byte("a"), []byte("l"))
	if len(results) != 0 {
		t.Errorf("Expected 0 results before key, got %d", len(results))
	}

	// Scan after the key
	results = mt.Scan([]byte("n"), []byte("z"))
	if len(results) != 0 {
		t.Errorf("Expected 0 results after key, got %d", len(results))
	}

	// Scan including the key
	results = mt.Scan([]byte("a"), []byte("z"))
	if len(results) != 1 {
		t.Errorf("Expected 1 result including key, got %d", len(results))
	}
}

// TestMemTable_Timestamp tests that timestamps are set
func TestMemTable_Timestamp(t *testing.T) {
	mt := NewMemTable(1024)

	key := []byte("key")
	value := []byte("value")

	mt.Put(key, value)

	entry, found := mt.Get(key)
	if !found {
		t.Fatal("Expected to find key")
	}

	if entry.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	// Update and verify timestamp changes or stays same (timing dependent)
	oldTimestamp := entry.Timestamp
	mt.Put(key, []byte("newvalue"))

	entry2, _ := mt.Get(key)
	// Timestamp should be >= (not strictly >) because nanosecond timing may be identical
	if entry2.Timestamp < oldTimestamp {
		t.Error("Expected timestamp to not decrease on update")
	}
}

// TestMemTable_Clear tests clearing the memtable
func TestMemTable_Clear(t *testing.T) {
	mt := NewMemTable(1024)

	// Add some entries
	entries := map[string][]byte{
		"key1": []byte("value1"),
		"key2": []byte("value2"),
		"key3": []byte("value3"),
	}

	for key, value := range entries {
		mt.Put([]byte(key), value)
	}

	// Verify entries exist
	for key := range entries {
		_, found := mt.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s before clear", key)
		}
	}

	// Verify size is non-zero
	if mt.Size() == 0 {
		t.Error("Expected non-zero size before clear")
	}

	// Clear the memtable
	mt.Clear()

	// Verify all entries are gone
	for key := range entries {
		_, found := mt.Get([]byte(key))
		if found {
			t.Errorf("Key %s should not exist after clear", key)
		}
	}

	// Verify size is zero
	if mt.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", mt.Size())
	}

	// Verify memtable is still usable
	mt.Put([]byte("newkey"), []byte("newvalue"))
	entry, found := mt.Get([]byte("newkey"))
	if !found {
		t.Fatal("Memtable should work after clear")
	}
	if string(entry.Value) != "newvalue" {
		t.Errorf("Expected 'newvalue', got '%s'", string(entry.Value))
	}
}

// TestMemTable_ClearEmpty tests clearing an empty memtable
func TestMemTable_ClearEmpty(t *testing.T) {
	mt := NewMemTable(1024)

	// Clear empty memtable (should not panic)
	mt.Clear()

	// Verify size is still zero
	if mt.Size() != 0 {
		t.Errorf("Expected size 0 after clearing empty memtable, got %d", mt.Size())
	}

	// Memtable should still be usable
	mt.Put([]byte("key"), []byte("value"))
	_, found := mt.Get([]byte("key"))
	if !found {
		t.Error("Memtable should work after clearing empty table")
	}
}
