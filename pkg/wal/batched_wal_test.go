package wal

import (
	"os"
	"testing"
	"time"
)

// TestNewBatchedWAL tests creating a batched WAL
func TestNewBatchedWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	if bw == nil {
		t.Fatal("Expected non-nil batched WAL")
	}

	if bw.batchSize != 10 {
		t.Errorf("Expected batch size 10, got %d", bw.batchSize)
	}

	if bw.flushInterval != 100*time.Millisecond {
		t.Errorf("Expected flush interval 100ms, got %v", bw.flushInterval)
	}
}

// TestBatchedWAL_Append tests appending entries
func TestBatchedWAL_Append(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Append a single entry
	data := []byte("test data")
	lsn, err := bw.Append(OpCreateNode, data)

	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	if lsn == 0 {
		t.Error("Expected non-zero LSN")
	}
}

// TestBatchedWAL_BatchFlush tests automatic flush when batch is full
func TestBatchedWAL_BatchFlush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Small batch size to trigger flush quickly
	bw, err := NewBatchedWAL(tmpDir, 3, 1*time.Second)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Append entries to fill the batch
	for i := 0; i < 3; i++ {
		data := []byte("test data")
		_, err := bw.Append(OpCreateNode, data)
		if err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}
	}

	// Verify entries were flushed (buffer should be empty)
	bw.mu.Lock()
	bufLen := len(bw.buffer)
	bw.mu.Unlock()

	// Buffer should be cleared after flush
	if bufLen >= 3 {
		t.Errorf("Expected buffer to be flushed, got %d entries", bufLen)
	}
}

// TestBatchedWAL_TimedFlush tests automatic flush based on time interval
func TestBatchedWAL_TimedFlush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Very short flush interval
	bw, err := NewBatchedWAL(tmpDir, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Append one entry (not enough to trigger batch flush)
	data := []byte("test data")
	_, err = bw.Append(OpCreateNode, data)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// Wait for timed flush
	time.Sleep(150 * time.Millisecond)

	// Buffer should be empty after timed flush
	bw.mu.Lock()
	bufLen := len(bw.buffer)
	bw.mu.Unlock()

	if bufLen != 0 {
		t.Errorf("Expected buffer to be flushed by timer, got %d entries", bufLen)
	}
}

// TestBatchedWAL_Replay tests replaying entries
func TestBatchedWAL_Replay(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write some entries
	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}

	expectedData := [][]byte{
		[]byte("entry1"),
		[]byte("entry2"),
		[]byte("entry3"),
	}

	for _, data := range expectedData {
		_, err := bw.Append(OpCreateNode, data)
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Ensure flush before close
	bw.flush()
	bw.Close()

	// Reopen and replay
	bw2, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to reopen batched WAL: %v", err)
	}
	defer bw2.Close()

	replayedEntries := make([]*Entry, 0)
	err = bw2.Replay(func(entry *Entry) error {
		replayedEntries = append(replayedEntries, entry)
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if len(replayedEntries) != len(expectedData) {
		t.Errorf("Expected %d replayed entries, got %d", len(expectedData), len(replayedEntries))
	}

	for i, entry := range replayedEntries {
		if string(entry.Data) != string(expectedData[i]) {
			t.Errorf("Entry %d: expected %s, got %s", i, expectedData[i], entry.Data)
		}
	}
}

// TestBatchedWAL_Truncate tests truncating the WAL
func TestBatchedWAL_Truncate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Write some entries
	for i := 0; i < 5; i++ {
		_, err := bw.Append(OpCreateNode, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Truncate
	err = bw.Truncate()
	if err != nil {
		t.Fatalf("Failed to truncate: %v", err)
	}

	// LSN should be reset
	lsn := bw.GetCurrentLSN()
	if lsn != 0 {
		t.Errorf("Expected LSN 0 after truncate, got %d", lsn)
	}

	// Replay should find no entries
	replayCount := 0
	err = bw.Replay(func(entry *Entry) error {
		replayCount++
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay after truncate: %v", err)
	}

	if replayCount != 0 {
		t.Errorf("Expected 0 entries after truncate, got %d", replayCount)
	}
}

// TestBatchedWAL_GetCurrentLSN tests getting current LSN
func TestBatchedWAL_GetCurrentLSN(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	initialLSN := bw.GetCurrentLSN()

	// Append some entries
	for i := 0; i < 3; i++ {
		_, err := bw.Append(OpCreateNode, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	currentLSN := bw.GetCurrentLSN()

	// LSN should have increased
	if currentLSN <= initialLSN {
		t.Errorf("Expected LSN to increase from %d, got %d", initialLSN, currentLSN)
	}
}

// TestBatchedWAL_ConcurrentAppends tests concurrent appends
func TestBatchedWAL_ConcurrentAppends(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Spawn multiple goroutines appending concurrently
	numGoroutines := 5
	appendsPerGoroutine := 10

	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < appendsPerGoroutine; j++ {
				_, err := bw.Append(OpCreateNode, []byte("concurrent"))
				if err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		err := <-done
		if err != nil {
			t.Fatalf("Concurrent append failed: %v", err)
		}
	}

	// Ensure all entries are flushed
	bw.flush()

	// Verify all entries were written
	replayCount := 0
	err = bw.Replay(func(entry *Entry) error {
		replayCount++
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	expected := numGoroutines * appendsPerGoroutine
	if replayCount != expected {
		t.Errorf("Expected %d entries, got %d", expected, replayCount)
	}
}

// TestBatchedWAL_Close tests closing the WAL
func TestBatchedWAL_Close(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}

	// Append some entries
	_, err = bw.Append(OpCreateNode, []byte("test"))
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// Close should flush remaining entries
	err = bw.Close()
	if err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	// Close should be idempotent - calling twice should not panic
	err = bw.Close()
	if err != nil {
		t.Fatalf("Failed on second close: %v", err)
	}
}

// TestBatchedWAL_EmptyFlush tests flushing with empty buffer
func TestBatchedWAL_EmptyFlush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "batched-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	bw, err := NewBatchedWAL(tmpDir, 10, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create batched WAL: %v", err)
	}
	defer bw.Close()

	// Flush with empty buffer should not error
	bw.flush()

	// Multiple empty flushes should be safe
	bw.flush()
	bw.flush()
}

// TestWAL_AppendBatch tests the batch append method
func TestWAL_AppendBatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wal-batch-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wal, err := NewWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	// Create batch entries
	entries := []*pendingEntry{
		{opType: OpCreateNode, data: []byte("entry1"), doneCh: make(chan error, 1)},
		{opType: OpUpdateNode, data: []byte("entry2"), doneCh: make(chan error, 1)},
		{opType: OpDeleteNode, data: []byte("entry3"), doneCh: make(chan error, 1)},
	}

	// Append batch
	err = wal.AppendBatch(entries)
	if err != nil {
		t.Fatalf("Failed to append batch: %v", err)
	}

	// Verify entries were written
	replayCount := 0
	err = wal.Replay(func(entry *Entry) error {
		replayCount++
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to replay: %v", err)
	}

	if replayCount != 3 {
		t.Errorf("Expected 3 entries, got %d", replayCount)
	}
}

// TestWAL_AppendBatch_EmptyBatch tests appending empty batch
func TestWAL_AppendBatch_EmptyBatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wal-batch-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wal, err := NewWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create WAL: %v", err)
	}
	defer wal.Close()

	// Append empty batch should not error
	err = wal.AppendBatch([]*pendingEntry{})
	if err != nil {
		t.Errorf("Expected no error for empty batch, got %v", err)
	}
}

// TestEntry_calculateChecksum tests checksum calculation
func TestEntry_calculateChecksum(t *testing.T) {
	entry := &Entry{
		LSN:    1,
		OpType: OpCreateNode,
		Data:   []byte("test data"),
	}

	checksum := entry.calculateChecksum()

	if checksum == 0 {
		t.Error("Expected non-zero checksum")
	}

	// Same data should produce same checksum
	checksum2 := entry.calculateChecksum()
	if checksum != checksum2 {
		t.Errorf("Expected consistent checksum, got %d and %d", checksum, checksum2)
	}

	// Different data should produce different checksum
	entry.Data = []byte("different data")
	checksum3 := entry.calculateChecksum()
	if checksum == checksum3 {
		t.Error("Expected different checksum for different data")
	}
}
