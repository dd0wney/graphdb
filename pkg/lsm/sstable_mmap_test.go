package lsm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMappedSSTable_OpenAndClose tests opening and closing memory-mapped SSTable
func TestMappedSSTable_OpenAndClose(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a regular SSTable first
	entries := []*Entry{
		{Key: []byte("key1"), Value: []byte("value1"), Timestamp: 100},
		{Key: []byte("key2"), Value: []byte("value2"), Timestamp: 200},
		{Key: []byte("key3"), Value: []byte("value3"), Timestamp: 300},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, err := NewSSTable(sstPath, entries)
	if err != nil {
		t.Fatalf("Failed to create SSTable: %v", err)
	}
	sst.Close()

	// Open with mmap
	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	if mmapSST.entryCount != 3 {
		t.Errorf("Expected 3 entries, got %d", mmapSST.entryCount)
	}
}

// TestMappedSSTable_Get tests getting values from memory-mapped SSTable
func TestMappedSSTable_Get(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SSTable with test data
	entries := []*Entry{
		{Key: []byte("apple"), Value: []byte("red"), Timestamp: 100},
		{Key: []byte("banana"), Value: []byte("yellow"), Timestamp: 200},
		{Key: []byte("cherry"), Value: []byte("red"), Timestamp: 300},
		{Key: []byte("date"), Value: []byte("brown"), Timestamp: 400},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	// Open with mmap
	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	// Test existing keys
	entry, found := mmapSST.Get([]byte("banana"))
	if !found {
		t.Error("Expected to find 'banana'")
	}
	if string(entry.Value) != "yellow" {
		t.Errorf("Expected value 'yellow', got '%s'", string(entry.Value))
	}

	// Test non-existent key
	_, found = mmapSST.Get([]byte("grape"))
	if found {
		t.Error("Should not find 'grape'")
	}

	// Test first key
	entry, found = mmapSST.Get([]byte("apple"))
	if !found {
		t.Error("Expected to find 'apple'")
	}
	if string(entry.Value) != "red" {
		t.Errorf("Expected value 'red', got '%s'", string(entry.Value))
	}

	// Test last key
	entry, found = mmapSST.Get([]byte("date"))
	if !found {
		t.Error("Expected to find 'date'")
	}
	if string(entry.Value) != "brown" {
		t.Errorf("Expected value 'brown', got '%s'", string(entry.Value))
	}
}

// TestMappedSSTable_GetDeleted tests getting deleted entries
func TestMappedSSTable_GetDeleted(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SSTable with deleted entry
	entries := []*Entry{
		{Key: []byte("key1"), Value: []byte("value1"), Timestamp: 100, Deleted: false},
		{Key: []byte("key2"), Value: []byte("value2"), Timestamp: 200, Deleted: true},
		{Key: []byte("key3"), Value: []byte("value3"), Timestamp: 300, Deleted: false},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	// Open with mmap
	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	// key2 should not be found (deleted)
	_, found := mmapSST.Get([]byte("key2"))
	if found {
		t.Error("Should not find deleted key 'key2'")
	}

	// key1 and key3 should be found
	_, found = mmapSST.Get([]byte("key1"))
	if !found {
		t.Error("Expected to find 'key1'")
	}

	_, found = mmapSST.Get([]byte("key3"))
	if !found {
		t.Error("Expected to find 'key3'")
	}
}

// TestMappedSSTable_Scan tests range scans
func TestMappedSSTable_Scan(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SSTable with sequential keys
	entries := []*Entry{
		{Key: []byte("a"), Value: []byte("1"), Timestamp: 100},
		{Key: []byte("b"), Value: []byte("2"), Timestamp: 200},
		{Key: []byte("c"), Value: []byte("3"), Timestamp: 300},
		{Key: []byte("d"), Value: []byte("4"), Timestamp: 400},
		{Key: []byte("e"), Value: []byte("5"), Timestamp: 500},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	// Open with mmap
	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	// Scan [b, d)
	results, err := mmapSST.Scan([]byte("b"), []byte("d"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	if string(results[0].Key) != "b" || string(results[1].Key) != "c" {
		t.Error("Scan returned wrong keys")
	}

	// Scan entire range
	results, err = mmapSST.Scan([]byte("a"), []byte("z"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(results) != 5 {
		t.Errorf("Expected 5 results, got %d", len(results))
	}

	// Scan empty range
	results, err = mmapSST.Scan([]byte("f"), []byte("z"))
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

// TestMappedSSTable_ScanWithDeleted tests scan skips deleted entries
func TestMappedSSTable_ScanWithDeleted(t *testing.T) {
	tmpDir := t.TempDir()

	entries := []*Entry{
		{Key: []byte("a"), Value: []byte("1"), Timestamp: 100, Deleted: false},
		{Key: []byte("b"), Value: []byte("2"), Timestamp: 200, Deleted: true},
		{Key: []byte("c"), Value: []byte("3"), Timestamp: 300, Deleted: false},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	results, _ := mmapSST.Scan([]byte("a"), []byte("z"))

	// Should only get 2 results (a and c), skipping deleted b
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	if string(results[0].Key) != "a" || string(results[1].Key) != "c" {
		t.Error("Scan returned wrong keys or included deleted entry")
	}
}

// TestMappedSSTable_Iterator tests iterating all entries
func TestMappedSSTable_Iterator(t *testing.T) {
	tmpDir := t.TempDir()

	entries := []*Entry{
		{Key: []byte("key1"), Value: []byte("value1"), Timestamp: 100},
		{Key: []byte("key2"), Value: []byte("value2"), Timestamp: 200},
		{Key: []byte("key3"), Value: []byte("value3"), Timestamp: 300},
	}

	sstPath := filepath.Join(tmpDir, "test-000001.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	results, err := mmapSST.Iterator()
	if err != nil {
		t.Fatalf("Iterator failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify order and content
	expectedKeys := []string{"key1", "key2", "key3"}
	for i, entry := range results {
		if string(entry.Key) != expectedKeys[i] {
			t.Errorf("Expected key '%s', got '%s'", expectedKeys[i], string(entry.Key))
		}
	}
}

// TestMappedSSTable_InvalidFile tests opening invalid file
func TestMappedSSTable_InvalidFile(t *testing.T) {
	_, err := OpenMappedSSTable("/nonexistent/file.sst")
	if err == nil {
		t.Error("Expected error opening non-existent file")
	}
}

// TestMappedSSTable_InvalidMagic tests file with invalid magic number
func TestMappedSSTable_InvalidMagic(t *testing.T) {
	tmpDir := t.TempDir()
	badPath := filepath.Join(tmpDir, "bad.sst")

	// Create file with invalid header
	f, _ := os.Create(badPath)
	f.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0}) // Wrong magic
	f.Close()

	_, err := OpenMappedSSTable(badPath)
	if err == nil {
		t.Error("Expected error opening file with invalid magic")
	}
}

// TestMappedSSTable_LargeDataset tests with larger dataset
func TestMappedSSTable_LargeDataset(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 1000 entries
	entries := make([]*Entry, 1000)
	for i := 0; i < 1000; i++ {
		key := []byte(string(rune('a' + (i % 26))) + string(rune('a' + (i / 26))))
		entries[i] = &Entry{
			Key:       key,
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "test-large.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open large mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	// Test random access
	_, found := mmapSST.Get(entries[500].Key)
	if !found {
		t.Error("Failed to find entry in large dataset")
	}

	// Test scan
	results, _ := mmapSST.Scan(entries[100].Key, entries[200].Key)
	if len(results) < 90 { // Should find ~100 entries
		t.Errorf("Expected ~100 results, got %d", len(results))
	}

	// Test iterator
	allResults, _ := mmapSST.Iterator()
	if len(allResults) != 1000 {
		t.Errorf("Expected 1000 results, got %d", len(allResults))
	}
}

// TestMappedSSTable_EmptySSTable tests with empty SSTable
func TestMappedSSTable_EmptySSTable(t *testing.T) {
	tmpDir := t.TempDir()

	entries := []*Entry{}
	sstPath := filepath.Join(tmpDir, "test-empty.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, err := OpenMappedSSTable(sstPath)
	if err != nil {
		t.Fatalf("Failed to open empty mapped SSTable: %v", err)
	}
	defer mmapSST.Close()

	if mmapSST.entryCount != 0 {
		t.Errorf("Expected 0 entries, got %d", mmapSST.entryCount)
	}

	// Get should not find anything
	_, found := mmapSST.Get([]byte("any"))
	if found {
		t.Error("Should not find any entries in empty SSTable")
	}

	// Iterator should return empty
	results, _ := mmapSST.Iterator()
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

// TestMappedSSTable_BloomFilter tests bloom filter integration
func TestMappedSSTable_BloomFilter(t *testing.T) {
	tmpDir := t.TempDir()

	entries := []*Entry{
		{Key: []byte("exists1"), Value: []byte("value1"), Timestamp: 100},
		{Key: []byte("exists2"), Value: []byte("value2"), Timestamp: 200},
	}

	sstPath := filepath.Join(tmpDir, "test-bloom.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	// Bloom filter should correctly identify non-existent keys
	// (may have false positives, but never false negatives)
	_, found := mmapSST.Get([]byte("exists1"))
	if !found {
		t.Error("Bloom filter gave false negative for existing key")
	}

	// Test many non-existent keys - most should be filtered out
	falsePositives := 0
	for i := 0; i < 100; i++ {
		key := []byte(string(rune('z' + i)))
		_, found := mmapSST.Get(key)
		if found {
			falsePositives++
		}
	}

	// False positive rate should be reasonable (< 50%)
	if falsePositives > 50 {
		t.Errorf("Too many false positives: %d/100", falsePositives)
	}
}

// BenchmarkMappedSSTable_Open benchmarks opening memory-mapped SSTables
func BenchmarkMappedSSTable_Open(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with 1000 entries
	entries := make([]*Entry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &Entry{
			Key:       []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26)))),
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmapSST, _ := OpenMappedSSTable(sstPath)
		mmapSST.Close()
	}
}

// BenchmarkMappedSSTable_Get benchmarks Get operations on memory-mapped SSTables
func BenchmarkMappedSSTable_Get(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with 10000 entries
	entries := make([]*Entry, 10000)
	for i := 0; i < 10000; i++ {
		key := []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26))) + string(rune('a'+(i/676))))
		entries[i] = &Entry{
			Key:       key,
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmapSST.Get(entries[i%10000].Key)
	}
}

// BenchmarkMappedSSTable_GetMissing benchmarks Get operations for missing keys
func BenchmarkMappedSSTable_GetMissing(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with 1000 entries
	entries := make([]*Entry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &Entry{
			Key:       []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26)))),
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	// Create keys that don't exist
	missingKey := []byte("zzz")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmapSST.Get(missingKey)
	}
}

// BenchmarkMappedSSTable_Scan benchmarks range scan operations
func BenchmarkMappedSSTable_Scan(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with sequential keys
	entries := make([]*Entry, 1000)
	for i := 0; i < 1000; i++ {
		key := []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26))))
		entries[i] = &Entry{
			Key:       key,
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	startKey := entries[100].Key
	endKey := entries[200].Key

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmapSST.Scan(startKey, endKey)
	}
}

// BenchmarkMappedSSTable_Iterator benchmarks full iteration
func BenchmarkMappedSSTable_Iterator(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with 1000 entries
	entries := make([]*Entry, 1000)
	for i := 0; i < 1000; i++ {
		entries[i] = &Entry{
			Key:       []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26)))),
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")
	sst, _ := NewSSTable(sstPath, entries)
	sst.Close()

	mmapSST, _ := OpenMappedSSTable(sstPath)
	defer mmapSST.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mmapSST.Iterator()
	}
}

// BenchmarkMappedSSTable_vs_Regular_Get compares mmap vs regular SSTable Get performance
func BenchmarkMappedSSTable_vs_Regular_Get(b *testing.B) {
	tmpDir := b.TempDir()

	// Create SSTable with 10000 entries
	entries := make([]*Entry, 10000)
	for i := 0; i < 10000; i++ {
		key := []byte(string(rune('a'+(i%26))) + string(rune('a'+(i/26))) + string(rune('a'+(i/676))))
		entries[i] = &Entry{
			Key:       key,
			Value:     []byte("value"),
			Timestamp: int64(i),
		}
	}

	sstPath := filepath.Join(tmpDir, "bench.sst")

	b.Run("Regular", func(b *testing.B) {
		sst, _ := NewSSTable(sstPath, entries)
		defer sst.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sst.Get(entries[i%10000].Key)
		}
	})

	b.Run("Mmap", func(b *testing.B) {
		// Recreate SSTable for mmap test
		sst, _ := NewSSTable(sstPath, entries)
		sst.Close()

		mmapSST, _ := OpenMappedSSTable(sstPath)
		defer mmapSST.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mmapSST.Get(entries[i%10000].Key)
		}
	})
}
