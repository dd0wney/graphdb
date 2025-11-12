package lsm

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// Helper function to create a test LSM storage
func newTestLSM(t *testing.T) *LSMStorage {
	tmpDir := t.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = false // Disable for simpler testing
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	return lsm
}

// Helper function to create a test LSM storage with autocompaction
func newTestLSMWithCompaction(t *testing.T) *LSMStorage {
	tmpDir := t.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = true
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	return lsm
}

// TestLSMBasicOperations tests basic Put/Get/Delete operations
func TestLSMBasicOperations(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	// Test Put
	key := []byte("test-key")
	value := []byte("test-value")

	if err := lsm.Put(key, value); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Test Get
	retrieved, found := lsm.Get(key)
	if !found {
		t.Fatal("Key not found after Put")
	}
	if !bytes.Equal(retrieved, value) {
		t.Errorf("Retrieved value mismatch: got %s, want %s", retrieved, value)
	}

	// Test Delete
	if err := lsm.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, found = lsm.Get(key)
	if found {
		t.Error("Key still found after Delete - BUG: cache not invalidated on delete")
	}
}

// TestLSMConcurrentReads tests that concurrent reads work correctly
func TestLSMConcurrentReads(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	// Insert test data
	numKeys := 100
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		if err := lsm.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Concurrent reads
	var wg sync.WaitGroup
	numReaders := 10
	readsPerReader := 50

	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < readsPerReader; i++ {
				keyNum := i % numKeys
				key := []byte(fmt.Sprintf("key-%d", keyNum))
				expectedValue := []byte(fmt.Sprintf("value-%d", keyNum))

				value, found := lsm.Get(key)
				if !found {
					t.Errorf("Reader %d: Key %s not found", readerID, key)
					return
				}
				if !bytes.Equal(value, expectedValue) {
					t.Errorf("Reader %d: Value mismatch for %s", readerID, key)
					return
				}
			}
		}(r)
	}

	wg.Wait()
}

// TestLSMConcurrentWrites tests concurrent write operations
func TestLSMConcurrentWrites(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	var wg sync.WaitGroup
	numWriters := 5
	writesPerWriter := 20

	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				key := []byte(fmt.Sprintf("writer-%d-key-%d", writerID, i))
				value := []byte(fmt.Sprintf("writer-%d-value-%d", writerID, i))
				if err := lsm.Put(key, value); err != nil {
					t.Errorf("Writer %d: Put failed: %v", writerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify all writes
	for w := 0; w < numWriters; w++ {
		for i := 0; i < writesPerWriter; i++ {
			key := []byte(fmt.Sprintf("writer-%d-key-%d", w, i))
			expectedValue := []byte(fmt.Sprintf("writer-%d-value-%d", w, i))

			value, found := lsm.Get(key)
			if !found {
				t.Errorf("Key %s not found after concurrent writes", key)
			}
			if !bytes.Equal(value, expectedValue) {
				t.Errorf("Value mismatch for %s", key)
			}
		}
	}
}

// TestLSMCompactionRaceFix tests that compaction doesn't race with concurrent reads
// This test specifically validates the copy-on-write fix we implemented
func TestLSMCompactionRaceFix(t *testing.T) {
	lsm := newTestLSMWithCompaction(t)
	defer lsm.Close()

	// Insert enough data to trigger compaction
	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		value := []byte(fmt.Sprintf("value-%06d", i))
		if err := lsm.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Start concurrent readers while compaction might be happening
	stopReaders := make(chan struct{})
	var readerWg sync.WaitGroup
	numReaders := 10
	errors := make(chan error, numReaders*100)

	for r := 0; r < numReaders; r++ {
		readerWg.Add(1)
		go func(readerID int) {
			defer readerWg.Done()
			readCount := 0
			for {
				select {
				case <-stopReaders:
					return
				default:
					keyNum := readCount % numKeys
					key := []byte(fmt.Sprintf("key-%06d", keyNum))
					expectedValue := []byte(fmt.Sprintf("value-%06d", keyNum))

					value, found := lsm.Get(key)
					if !found {
						errors <- fmt.Errorf("reader %d: key %s not found", readerID, key)
						return
					}
					if !bytes.Equal(value, expectedValue) {
						errors <- fmt.Errorf("reader %d: value mismatch for %s", readerID, key)
						return
					}
					readCount++
				}
			}
		}(r)
	}

	// Let readers run while compaction might occur
	time.Sleep(200 * time.Millisecond)

	// Stop readers
	close(stopReaders)
	readerWg.Wait()

	// Check for errors
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

// TestLSMScan tests the Scan functionality
func TestLSMScan(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	// Insert sequential keys
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key-%03d", i))
		value := []byte(fmt.Sprintf("value-%03d", i))
		if err := lsm.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Scan a range
	start := []byte("key-010")
	end := []byte("key-020")

	results, err := lsm.Scan(start, end)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	// Verify we got the right keys
	expectedCount := 10 // key-010 through key-019
	if len(results) != expectedCount {
		t.Errorf("Scan returned %d results, expected %d", len(results), expectedCount)
	}

	// Verify values
	for i := 10; i < 20; i++ {
		key := fmt.Sprintf("key-%03d", i)
		expectedValue := []byte(fmt.Sprintf("value-%03d", i))

		value, exists := results[key]
		if !exists {
			t.Errorf("Key %s not in scan results", key)
			continue
		}
		if !bytes.Equal(value, expectedValue) {
			t.Errorf("Value mismatch for %s", key)
		}
	}
}

// TestLSMUpdateValue tests updating existing keys
func TestLSMUpdateValue(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	key := []byte("update-key")

	// Initial value
	value1 := []byte("value-1")
	if err := lsm.Put(key, value1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify initial value
	retrieved, found := lsm.Get(key)
	if !found {
		t.Fatal("Key not found")
	}
	if !bytes.Equal(retrieved, value1) {
		t.Error("Initial value mismatch")
	}

	// Update value
	value2 := []byte("value-2-updated")
	if err := lsm.Put(key, value2); err != nil {
		t.Fatalf("Update Put failed: %v", err)
	}

	// Verify updated value
	retrieved, found = lsm.Get(key)
	if !found {
		t.Fatal("Key not found after update")
	}
	if !bytes.Equal(retrieved, value2) {
		t.Errorf("Updated value mismatch: got %s, want %s - BUG: cache not invalidated on update", retrieved, value2)
	}
}

// TestLSMStatistics tests that statistics are tracked correctly
func TestLSMStatistics(t *testing.T) {
	lsm := newTestLSM(t)
	defer lsm.Close()

	// Initial stats
	stats := lsm.GetStats()
	initialWrites := stats.WriteCount

	// Insert some data
	numKeys := 10
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("stats-key-%d", i))
		value := []byte(fmt.Sprintf("stats-value-%d", i))
		if err := lsm.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Stats should reflect the writes
	stats = lsm.GetStats()
	if stats.WriteCount != initialWrites+int64(numKeys) {
		t.Errorf("WriteCount should be %d, got %d", initialWrites+int64(numKeys), stats.WriteCount)
	}
}

// Benchmark concurrent reads with compaction
func BenchmarkLSMConcurrentReadsWithCompaction(b *testing.B) {
	tmpDir := b.TempDir()
	defer os.RemoveAll(tmpDir)

	opts := DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = true
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Prepopulate with data
	numKeys := 10000
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("bench-key-%06d", i))
		value := []byte(fmt.Sprintf("bench-value-%06d", i))
		lsm.Put(key, value)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			keyNum := i % numKeys
			key := []byte(fmt.Sprintf("bench-key-%06d", keyNum))
			lsm.Get(key)
			i++
		}
	})
}

// BenchmarkLSM_SequentialWrites benchmarks sequential writes
func BenchmarkLSM_SequentialWrites(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	value := make([]byte, 1024)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		if err := lsm.Put(key, value); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLSM_RandomReads benchmarks random reads
func BenchmarkLSM_RandomReads(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Pre-populate
	numKeys := 10000
	value := make([]byte, 1024)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		lsm.Put(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % numKeys
		key := []byte(fmt.Sprintf("key-%08d", idx))
		lsm.Get(key)
	}
}

// BenchmarkLSM_RangeScans benchmarks range scans
func BenchmarkLSM_RangeScans(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Pre-populate with 10k entries
	value := make([]byte, 1024)
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		lsm.Put(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startIdx := (i * 1000) % 9000
		startKey := []byte(fmt.Sprintf("key-%08d", startIdx))
		endKey := []byte(fmt.Sprintf("key-%08d", startIdx+1000))
		lsm.Scan(startKey, endKey)
	}
}

// BenchmarkLSM_Updates benchmarks random updates
func BenchmarkLSM_Updates(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Pre-populate
	numKeys := 10000
	value := make([]byte, 1024)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		lsm.Put(key, value)
	}

	newValue := make([]byte, 1024)
	for i := range newValue {
		newValue[i] = 0xFF
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx := i % numKeys
		key := []byte(fmt.Sprintf("key-%08d", idx))
		lsm.Put(key, newValue)
	}
}

// BenchmarkLSM_Deletions benchmarks deletions
func BenchmarkLSM_Deletions(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Pre-populate
	value := make([]byte, 1024)
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		lsm.Put(key, value)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key-%08d", i))
		lsm.Delete(key)
	}
}

// BenchmarkLSM_Put benchmarks single put operations
func BenchmarkLSM_Put(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	key := []byte("benchmark-key")
	value := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lsm.Put(key, value)
	}
}

// BenchmarkLSM_Get benchmarks single get operations
func BenchmarkLSM_Get(b *testing.B) {
	tmpDir := b.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		b.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	key := []byte("benchmark-key")
	value := make([]byte, 1024)
	lsm.Put(key, value)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lsm.Get(key)
	}
}

// TestLSM_PrintStats tests printing statistics (just ensures it doesn't panic)
func TestLSM_PrintStats(t *testing.T) {
	tmpDir := t.TempDir()
	opts := DefaultLSMOptions(tmpDir)
	lsm, err := NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsm.Close()

	// Perform some operations to populate stats
	key := []byte("test-key")
	value := []byte("test-value")

	err = lsm.Put(key, value)
	if err != nil {
		t.Fatalf("Failed to put: %v", err)
	}

	_, found := lsm.Get(key)
	if !found {
		t.Fatal("Failed to get key")
	}

	// Call PrintStats (should not panic)
	// Note: This prints to stdout, but we're just testing that it works
	lsm.PrintStats()

	// Verify stats are accessible through GetStats as well
	stats := lsm.GetStats()
	if stats.WriteCount == 0 {
		t.Error("Expected non-zero write count")
	}
	if stats.ReadCount == 0 {
		t.Error("Expected non-zero read count")
	}
}
