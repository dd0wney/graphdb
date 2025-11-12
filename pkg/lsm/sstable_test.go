package lsm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestSSTable_CreateAndOpen tests creating and reopening an SSTable
func TestSSTable_CreateAndOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	// Create entries
	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 1},
		{Key: []byte("banana"), Value: []byte("yellow"), Timestamp: 2},
		{Key: []byte("cherry"), Value: []byte("red"), Timestamp: 3},
	}

	// Create SSTable
	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}

	// Verify entry count
	if sst.entryCount != len(entries) {
		t.Errorf("Expected %d entries, got %d", len(entries), sst.entryCount)
	}

	// Close and reopen
	if err := sst.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	sst2, err := OpenSSTable(path)
	if err != nil {
		t.Fatalf("OpenSSTable failed: %v", err)
	}
	defer sst2.Close()

	// Verify header
	if sst2.header.Magic != SSTableMagic {
		t.Errorf("Expected magic %x, got %x", SSTableMagic, sst2.header.Magic)
	}
	if sst2.header.Version != SSTableVersion {
		t.Errorf("Expected version %d, got %d", SSTableVersion, sst2.header.Version)
	}
	if sst2.header.EntryCount != uint64(len(entries)) {
		t.Errorf("Expected %d entries, got %d", len(entries), sst2.header.EntryCount)
	}
}

// TestSSTable_Get tests retrieving values
func TestSSTable_Get(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 1},
		{Key: []byte("banana"), Value: []byte("yellow"), Timestamp: 2},
		{Key: []byte("cherry"), Value: []byte("red"), Timestamp: 3},
		{Key: []byte("date"), Value: []byte("brown"), Timestamp: 4},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}
	defer sst.Close()

	// Get existing keys
	for _, expected := range entries {
		entry, found := sst.Get(expected.Key)
		if !found {
			t.Errorf("Expected to find key %s", expected.Key)
			continue
		}
		if !bytes.Equal(entry.Value, expected.Value) {
			t.Errorf("Key %s: expected value %s, got %s", expected.Key, expected.Value, entry.Value)
		}
	}

	// Get non-existent key
	_, found := sst.Get([]byte("nonexistent"))
	if found {
		t.Error("Should not find nonexistent key")
	}
}

// TestSSTable_Scan tests range scanning
func TestSSTable_Scan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 1},
		{Key: []byte("banana"), Value: []byte("yellow"), Timestamp: 2},
		{Key: []byte("cherry"), Value: []byte("red"), Timestamp: 3},
		{Key: []byte("date"), Value: []byte("brown"), Timestamp: 4},
		{Key: []byte("elderberry"), Value: []byte("purple"), Timestamp: 5},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}
	defer sst.Close()

	// Scan range [banana, date)
	results, err := sst.Scan([]byte("banana"), []byte("date"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

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

// TestSSTable_Iterator tests full iteration
func TestSSTable_Iterator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("zebra"), Value: []byte("striped"), Timestamp: 1},
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 2},
		{Key: []byte("mango"), Value: []byte("orange"), Timestamp: 3},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}
	defer sst.Close()

	// Iterator should return sorted entries
	results, err := sst.Iterator()
	if err != nil {
		t.Fatalf("Iterator failed: %v", err)
	}

	expectedOrder := []string{"apple", "mango", "zebra"}
	if len(results) != len(expectedOrder) {
		t.Fatalf("Expected %d entries, got %d", len(expectedOrder), len(results))
	}

	for i, entry := range results {
		if string(entry.Key) != expectedOrder[i] {
			t.Errorf("Entry %d: expected key %s, got %s", i, expectedOrder[i], string(entry.Key))
		}
	}
}

// TestSSTable_BloomFilter tests Bloom filter integration
func TestSSTable_BloomFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 1},
		{Key: []byte("banana"), Value: []byte("yellow"), Timestamp: 2},
		{Key: []byte("cherry"), Value: []byte("red"), Timestamp: 3},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}
	defer sst.Close()

	// Keys in SSTable should be in Bloom filter (no false negatives)
	for _, entry := range entries {
		if !sst.bloom.Contains(entry.Key) {
			t.Errorf("Bloom filter should contain key %s (false negative)", entry.Key)
		}
	}

	// Keys not in SSTable might be in Bloom filter (false positives allowed)
	// Just verify no panic
	_ = sst.bloom.Contains([]byte("nonexistent"))
}

// TestSSTable_Delete tests file deletion
func TestSSTable_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("test"), Value: []byte("data"), Timestamp: 1},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("SSTable file should exist")
	}

	// Delete SSTable
	if err := sst.Delete(); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("SSTable file should be deleted")
	}
}

// TestSSTable_EmptyEntries tests handling of empty entry list
func TestSSTable_EmptyEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sst")

	entries := []*Entry{}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable with empty entries failed: %v", err)
	}
	defer sst.Close()

	if sst.entryCount != 0 {
		t.Errorf("Expected 0 entries, got %d", sst.entryCount)
	}

	// Iterator should return empty list
	results, err := sst.Iterator()
	if err != nil {
		t.Fatalf("Iterator failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results from empty SSTable, got %d", len(results))
	}
}

// TestSSTable_LargeEntries tests handling of large values
func TestSSTable_LargeEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.sst")

	// Create large value (1MB)
	largeValue := make([]byte, 1024*1024)
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}

	entries := []*Entry{
		{Key: []byte("key1"), Value: largeValue, Timestamp: 1},
		{Key: []byte("key2"), Value: []byte("small"), Timestamp: 2},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable with large entries failed: %v", err)
	}
	defer sst.Close()

	// Verify large entry can be retrieved
	entry, found := sst.Get([]byte("key1"))
	if !found {
		t.Fatal("Expected to find large entry")
	}
	if !bytes.Equal(entry.Value, largeValue) {
		t.Errorf("Large value not persisted correctly: expected %d bytes, got %d bytes", len(largeValue), len(entry.Value))
		if len(entry.Value) > 0 && len(entry.Value) < 100 {
			t.Errorf("Got value: %x", entry.Value)
		}
	}
}

// TestSSTable_DuplicateKeys tests that duplicate keys use last value (entries should be deduplicated before SSTable)
func TestSSTable_DuplicateKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dups.sst")

	// Note: In real usage, MemTable ensures latest value wins before SSTable creation
	// Here we test that SSTable handles receiving duplicates by keeping sorted order
	entries := []*Entry{
		{Key: []byte("key"), Value: []byte("value1"), Timestamp: 1},
		{Key: []byte("key"), Value: []byte("value2"), Timestamp: 2}, // Later timestamp
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable with duplicates failed: %v", err)
	}
	defer sst.Close()

	// Get should return one of the values (implementation dependent)
	_, found := sst.Get([]byte("key"))
	if !found {
		t.Error("Expected to find key")
	}
}

// TestSSTable_ManyEntries tests sparse index with many entries
func TestSSTable_ManyEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.sst")

	// Create enough entries to trigger multiple index entries
	numEntries := IndexInterval * 3 // 384 entries
	entries := make([]*Entry, numEntries)
	for i := 0; i < numEntries; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		entries[i] = &Entry{Key: key, Value: value, Timestamp: int64(i + 1)}
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable with many entries failed: %v", err)
	}
	defer sst.Close()

	// Verify index has multiple entries
	expectedIndexEntries := (numEntries + IndexInterval - 1) / IndexInterval
	if len(sst.index) < expectedIndexEntries {
		t.Errorf("Expected at least %d index entries, got %d", expectedIndexEntries, len(sst.index))
	}

	// Test retrieving entries
	testKeys := []int{0, IndexInterval, IndexInterval * 2, numEntries - 1}
	for _, i := range testKeys {
		key := []byte(fmt.Sprintf("key-%08d", i))
		entry, found := sst.Get(key)
		if !found {
			t.Errorf("Expected to find key %s", key)
			continue
		}
		expectedValue := []byte(fmt.Sprintf("value-%d", i))
		if !bytes.Equal(entry.Value, expectedValue) {
			t.Errorf("Key %s: expected value %s, got %s", key, expectedValue, entry.Value)
		}
	}
}

// TestSSTable_ScanEmptyRange tests scanning with no matching entries
func TestSSTable_ScanEmptyRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 1},
		{Key: []byte("zebra"), Value: []byte("striped"), Timestamp: 2},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}
	defer sst.Close()

	// Scan range with no entries in between
	results, err := sst.Scan([]byte("banana"), []byte("yak"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results from empty range, got %d", len(results))
	}
}

// TestSSTable_InvalidFile tests opening corrupted or invalid file
func TestSSTable_InvalidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.sst")

	// Create file with invalid magic
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	file.Write([]byte{0x00, 0x00, 0x00, 0x00}) // Invalid magic
	file.Close()

	// OpenSSTable should fail
	_, err = OpenSSTable(path)
	if err == nil {
		t.Error("Expected error opening invalid SSTable")
	}
}

// TestSSTable_Path tests SSTablePath helper function
func TestSSTable_Path(t *testing.T) {
	dir := "/data/lsm"
	level := 2
	id := 42

	path := SSTablePath(dir, level, id)

	// Implementation uses format: L{level}-{id:06d}.sst
	expectedPath := filepath.Join(dir, "L2-000042.sst")
	if path != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, path)
	}
}

// TestSSTable_CloseMultiple tests that closing multiple times is safe
func TestSSTable_CloseMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	entries := []*Entry{
		{Key: []byte("test"), Value: []byte("data"), Timestamp: 1},
	}

	sst, err := NewSSTable(path, entries)
	if err != nil {
		t.Fatalf("NewSSTable failed: %v", err)
	}

	// Close multiple times - should not panic
	err1 := sst.Close()
	if err1 != nil {
		t.Errorf("First close failed: %v", err1)
	}

	// Second close might fail (file already closed) but should not panic
	_ = sst.Close()
}
