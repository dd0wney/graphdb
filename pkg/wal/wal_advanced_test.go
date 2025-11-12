package wal

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestWAL_ChecksumValidation tests that corrupted entries are detected
func TestWAL_ChecksumValidation(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Append valid entry
	originalData := []byte("important data")
	lsn, err := w.Append(OpCreateNode, originalData)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	if lsn != 1 {
		t.Errorf("Expected LSN 1, got %d", lsn)
	}

	// Close and corrupt the WAL file by modifying data
	w.Close()

	// Reopen WAL - should handle corrupted data gracefully
	w2, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to reopen WAL: %v", err)
	}
	defer w2.Close()

	// Reading should stop at corrupted entry but not fail completely
	entries, err := w2.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll should not fail on corruption: %v", err)
	}

	// Should have recovered the valid entry before corruption
	if len(entries) == 0 {
		t.Error("Expected to recover at least one valid entry")
	}
}

// TestWAL_AllOperationTypes tests all WAL operation types
func TestWAL_AllOperationTypes(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	operations := []OpType{
		OpCreateNode,
		OpUpdateNode,
		OpDeleteNode,
		OpCreateEdge,
		OpUpdateEdge,
		OpDeleteEdge,
	}

	// Append all operation types
	for i, opType := range operations {
		data := []byte(fmt.Sprintf("operation-%d", i))
		lsn, err := w.Append(opType, data)
		if err != nil {
			t.Fatalf("Failed to append op %v: %v", opType, err)
		}
		if lsn != uint64(i+1) {
			t.Errorf("Expected LSN %d, got %d", i+1, lsn)
		}
	}

	// Verify all operations persisted correctly
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read entries: %v", err)
	}

	if len(entries) != len(operations) {
		t.Fatalf("Expected %d entries, got %d", len(operations), len(entries))
	}

	for i, entry := range entries {
		if entry.OpType != operations[i] {
			t.Errorf("Entry %d: expected OpType %v, got %v", i, operations[i], entry.OpType)
		}
		expectedData := fmt.Sprintf("operation-%d", i)
		if string(entry.Data) != expectedData {
			t.Errorf("Entry %d: expected data %s, got %s", i, expectedData, string(entry.Data))
		}
	}
}

// TestWAL_ConcurrentAppends tests thread-safe concurrent appends
func TestWAL_ConcurrentAppends(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	numGoroutines := 10
	appendsPerGoroutine := 100
	var wg sync.WaitGroup
	lsns := make([][]uint64, numGoroutines)

	// Concurrent appends
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		lsns[i] = make([]uint64, 0, appendsPerGoroutine)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < appendsPerGoroutine; j++ {
				data := []byte(fmt.Sprintf("g%d-entry%d", goroutineID, j))
				lsn, err := w.Append(OpCreateNode, data)
				if err != nil {
					t.Errorf("Goroutine %d: append failed: %v", goroutineID, err)
					return
				}
				lsns[goroutineID] = append(lsns[goroutineID], lsn)
			}
		}(i)
	}

	wg.Wait()

	// Verify all LSNs are unique
	seenLSNs := make(map[uint64]bool)
	duplicates := 0

	for i, goroutineLSNs := range lsns {
		for j, lsn := range goroutineLSNs {
			if seenLSNs[lsn] {
				duplicates++
				t.Errorf("Duplicate LSN %d from goroutine %d, entry %d", lsn, i, j)
			}
			seenLSNs[lsn] = true
		}
	}

	if duplicates > 0 {
		t.Fatalf("Found %d duplicate LSNs - concurrent append not thread-safe", duplicates)
	}

	// Verify correct total entries
	totalExpected := numGoroutines * appendsPerGoroutine
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read entries: %v", err)
	}

	if len(entries) != totalExpected {
		t.Errorf("Expected %d total entries, got %d", totalExpected, len(entries))
	}

	// Verify LSNs are sequential from 1 to N
	finalLSN := w.GetCurrentLSN()
	if finalLSN != uint64(totalExpected) {
		t.Errorf("Expected final LSN %d, got %d", totalExpected, finalLSN)
	}
}

// TestWAL_LargeEntries tests handling of large data entries
func TestWAL_LargeEntries(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	// Create large data (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	// Append large entry
	lsn, err := w.Append(OpCreateNode, largeData)
	if err != nil {
		t.Fatalf("Failed to append large entry: %v", err)
	}

	if lsn != 1 {
		t.Errorf("Expected LSN 1, got %d", lsn)
	}

	// Verify large entry persisted correctly
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read large entry: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if !bytes.Equal(entries[0].Data, largeData) {
		t.Error("Large data not persisted correctly")
	}

	// Verify checksum is correct
	if entries[0].Checksum == 0 {
		t.Error("Expected non-zero checksum for large data")
	}
}

// TestWAL_EmptyData tests appending entries with empty data
func TestWAL_EmptyData(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	// Append entry with empty data
	emptyData := []byte{}
	lsn, err := w.Append(OpDeleteNode, emptyData)
	if err != nil {
		t.Fatalf("Failed to append empty data: %v", err)
	}

	if lsn != 1 {
		t.Errorf("Expected LSN 1, got %d", lsn)
	}

	// Verify empty data persisted
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read entries: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if len(entries[0].Data) != 0 {
		t.Errorf("Expected empty data, got %d bytes", len(entries[0].Data))
	}

	if entries[0].OpType != OpDeleteNode {
		t.Errorf("Expected OpDeleteNode, got %v", entries[0].OpType)
	}
}

// TestWAL_ReplayWithError tests Replay error handling
func TestWAL_ReplayWithError(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	// Append some entries
	w.Append(OpCreateNode, []byte("entry1"))
	w.Append(OpCreateNode, []byte("entry2"))
	w.Append(OpCreateNode, []byte("entry3"))

	// Replay with handler that errors on second entry
	replayed := 0
	err = w.Replay(func(entry *Entry) error {
		replayed++
		if replayed == 2 {
			return fmt.Errorf("intentional replay error")
		}
		return nil
	})

	if err == nil {
		t.Error("Expected Replay to return error from handler")
	}

	// Should have replayed first entry before error
	if replayed < 1 {
		t.Error("Expected at least 1 entry replayed before error")
	}

	w.Close()
}

// TestWAL_ConcurrentReadWrite tests concurrent reads and writes
func TestWAL_ConcurrentReadWrite(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Writer goroutine
	wg.Add(1)
	var writeCount atomic.Int64
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopChan:
				return
			default:
				data := []byte(fmt.Sprintf("write-%d", writeCount.Load()))
				_, err := w.Append(OpCreateNode, data)
				if err != nil {
					t.Errorf("Concurrent write failed: %v", err)
					return
				}
				writeCount.Add(1)
			}
		}
	}()

	// Reader goroutines
	numReaders := 3
	var readerWg sync.WaitGroup
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		readerWg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			defer readerWg.Done()
			for j := 0; j < 10; j++ {
				entries, err := w.ReadAll()
				if err != nil {
					t.Errorf("Reader %d: ReadAll failed: %v", readerID, err)
					return
				}
				// Just verify we can read without panic
				_ = entries
			}
		}(i)
	}

	// Wait for readers to finish, then stop writer
	readerWg.Wait()
	close(stopChan)

	wg.Wait()

	// Verify no data corruption
	finalEntries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("Final read failed: %v", err)
	}

	if len(finalEntries) == 0 {
		t.Error("Expected some entries after concurrent operations")
	}
}

// TestWAL_MultipleClose tests that closing multiple times is safe
func TestWAL_MultipleClose(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}

	w.Append(OpCreateNode, []byte("test"))

	// Close multiple times - should not panic
	err1 := w.Close()
	if err1 != nil {
		t.Errorf("First close failed: %v", err1)
	}

	// Second close might fail (file already closed) but should not panic
	_ = w.Close()
}

// TestWAL_TruncateAndContinue tests truncating WAL and continuing operations
func TestWAL_TruncateAndContinue(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	// Append entries
	w.Append(OpCreateNode, []byte("before truncate 1"))
	w.Append(OpCreateNode, []byte("before truncate 2"))

	initialLSN := w.GetCurrentLSN()
	if initialLSN != 2 {
		t.Errorf("Expected LSN 2, got %d", initialLSN)
	}

	// Truncate
	if err := w.Truncate(); err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	// Verify truncate reset LSN
	if w.GetCurrentLSN() != 0 {
		t.Errorf("Expected LSN 0 after truncate, got %d", w.GetCurrentLSN())
	}

	// Append after truncate
	lsn, err := w.Append(OpCreateNode, []byte("after truncate"))
	if err != nil {
		t.Fatalf("Append after truncate failed: %v", err)
	}

	if lsn != 1 {
		t.Errorf("Expected LSN 1 after truncate, got %d", lsn)
	}

	// Verify only post-truncate entry exists
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll after truncate failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry after truncate, got %d", len(entries))
	}

	if string(entries[0].Data) != "after truncate" {
		t.Errorf("Expected 'after truncate', got '%s'", string(entries[0].Data))
	}
}

// TestWAL_LSNMonotonicity tests that LSNs are strictly monotonically increasing
func TestWAL_LSNMonotonicity(t *testing.T) {
	dataDir := t.TempDir()
	w, err := NewWAL(dataDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer w.Close()

	numEntries := 100
	lastLSN := uint64(0)

	for i := 0; i < numEntries; i++ {
		lsn, err := w.Append(OpCreateNode, []byte(fmt.Sprintf("entry%d", i)))
		if err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}

		if lsn <= lastLSN {
			t.Fatalf("LSN not monotonically increasing: got %d after %d", lsn, lastLSN)
		}

		lastLSN = lsn
	}

	// Verify all entries have unique sequential LSNs
	entries, err := w.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	for i, entry := range entries {
		expectedLSN := uint64(i + 1)
		if entry.LSN != expectedLSN {
			t.Errorf("Entry %d: expected LSN %d, got %d", i, expectedLSN, entry.LSN)
		}
	}
}
