package wal

import (
	"os"
	"strings"
	"testing"
)

// TestNewCompressedWAL tests creating a compressed WAL
func TestNewCompressedWAL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	if cw == nil {
		t.Fatal("Expected non-nil compressed WAL")
	}

	if cw.currentLSN != 0 {
		t.Errorf("Expected initial LSN 0, got %d", cw.currentLSN)
	}
}

// TestCompressedWAL_Append tests appending entries
func TestCompressedWAL_Append(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Append a single entry
	data := []byte("test data for compression")
	lsn, err := cw.Append(OpCreateNode, data)

	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	if lsn != 1 {
		t.Errorf("Expected LSN 1, got %d", lsn)
	}
}

// TestCompressedWAL_ReadAll tests reading all entries
func TestCompressedWAL_ReadAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}

	// Write multiple entries
	expectedData := []string{"entry1", "entry2", "entry3"}
	for _, data := range expectedData {
		_, err := cw.Append(OpCreateNode, []byte(data))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Flush to ensure data is written
	if err := cw.Flush(); err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}

	// Read all entries
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read all: %v", err)
	}

	if len(entries) != len(expectedData) {
		t.Errorf("Expected %d entries, got %d", len(expectedData), len(entries))
	}

	// Verify data was decompressed correctly
	for i, entry := range entries {
		if string(entry.Data) != expectedData[i] {
			t.Errorf("Entry %d: expected %s, got %s", i, expectedData[i], string(entry.Data))
		}
	}

	cw.Close()
}

// TestCompressedWAL_Compression tests that compression actually happens
func TestCompressedWAL_Compression(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Create highly compressible data (repeated pattern)
	data := []byte(strings.Repeat("AAAAAAAAAA", 100)) // 1000 bytes of 'A'

	_, err = cw.Append(OpCreateNode, data)
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// Check compression statistics
	stats := cw.GetStatistics()

	if stats.TotalWrites != 1 {
		t.Errorf("Expected 1 write, got %d", stats.TotalWrites)
	}

	if stats.BytesUncompressed != uint64(len(data)) {
		t.Errorf("Expected %d uncompressed bytes, got %d", len(data), stats.BytesUncompressed)
	}

	// Snappy should compress repeated data significantly
	if stats.BytesCompressed >= stats.BytesUncompressed {
		t.Errorf("Expected compression, but compressed size (%d) >= uncompressed size (%d)",
			stats.BytesCompressed, stats.BytesUncompressed)
	}

	// Check compression ratio
	if stats.CompressionRatio <= 0 {
		t.Errorf("Expected positive compression ratio, got %f", stats.CompressionRatio)
	}
}

// TestCompressedWAL_GetStatistics tests statistics collection
func TestCompressedWAL_GetStatistics(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Initial statistics should be zero
	stats := cw.GetStatistics()
	if stats.TotalWrites != 0 {
		t.Errorf("Expected 0 writes initially, got %d", stats.TotalWrites)
	}

	// Write some entries
	for i := 0; i < 5; i++ {
		_, err := cw.Append(OpCreateNode, []byte("test data"))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Check updated statistics
	stats = cw.GetStatistics()
	if stats.TotalWrites != 5 {
		t.Errorf("Expected 5 writes, got %d", stats.TotalWrites)
	}

	if stats.BytesUncompressed == 0 {
		t.Error("Expected non-zero uncompressed bytes")
	}

	if stats.BytesCompressed == 0 {
		t.Error("Expected non-zero compressed bytes")
	}
}

// TestCompressedWAL_Truncate tests truncating the WAL
func TestCompressedWAL_Truncate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Write some entries
	for i := 0; i < 5; i++ {
		_, err := cw.Append(OpCreateNode, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	// Truncate
	err = cw.Truncate()
	if err != nil {
		t.Fatalf("Failed to truncate: %v", err)
	}

	// LSN should be reset
	if cw.currentLSN != 0 {
		t.Errorf("Expected LSN 0 after truncate, got %d", cw.currentLSN)
	}

	// Reading should return no entries
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read after truncate: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 entries after truncate, got %d", len(entries))
	}
}

// TestCompressedWAL_Flush tests flushing to disk
func TestCompressedWAL_Flush(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Append entry
	_, err = cw.Append(OpCreateNode, []byte("test"))
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// Flush should not error
	err = cw.Flush()
	if err != nil {
		t.Fatalf("Failed to flush: %v", err)
	}
}

// TestCompressedWAL_Close tests closing the WAL
func TestCompressedWAL_Close(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}

	// Append entry
	_, err = cw.Append(OpCreateNode, []byte("test"))
	if err != nil {
		t.Fatalf("Failed to append: %v", err)
	}

	// Close should flush and sync
	err = cw.Close()
	if err != nil {
		t.Fatalf("Failed to close: %v", err)
	}
}

// TestCompressedWAL_RecoverLSN tests LSN recovery on restart
func TestCompressedWAL_RecoverLSN(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create WAL and write entries
	cw1, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := cw1.Append(OpCreateNode, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append: %v", err)
		}
	}

	cw1.Flush()
	expectedLSN := cw1.currentLSN
	cw1.Close()

	// Reopen and verify LSN was recovered
	cw2, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to reopen compressed WAL: %v", err)
	}
	defer cw2.Close()

	if cw2.currentLSN != expectedLSN {
		t.Errorf("Expected recovered LSN %d, got %d", expectedLSN, cw2.currentLSN)
	}
}

// TestCompressedWAL_EmptyReadAll tests reading from empty WAL
func TestCompressedWAL_EmptyReadAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Reading empty WAL should return empty slice
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read empty WAL: %v", err)
	}

	if entries == nil {
		t.Error("Expected empty slice, got nil")
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

// TestCompressedWAL_DifferentOpTypes tests different operation types
func TestCompressedWAL_DifferentOpTypes(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}

	// Test different operation types
	ops := []OpType{OpCreateNode, OpUpdateNode, OpDeleteNode, OpCreateEdge, OpUpdateEdge, OpDeleteEdge}

	for _, opType := range ops {
		_, err := cw.Append(opType, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append op %v: %v", opType, err)
		}
	}

	cw.Flush()

	// Read and verify
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(entries) != len(ops) {
		t.Errorf("Expected %d entries, got %d", len(ops), len(entries))
	}

	for i, entry := range entries {
		if entry.OpType != ops[i] {
			t.Errorf("Entry %d: expected OpType %v, got %v", i, ops[i], entry.OpType)
		}
	}

	cw.Close()
}

// TestCompressedWAL_LargeData tests compressing large data
func TestCompressedWAL_LargeData(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Create 1MB of data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	_, err = cw.Append(OpCreateNode, largeData)
	if err != nil {
		t.Fatalf("Failed to append large data: %v", err)
	}

	cw.Flush()

	// Read and verify
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}

	if len(entries[0].Data) != len(largeData) {
		t.Errorf("Expected data length %d, got %d", len(largeData), len(entries[0].Data))
	}

	// Verify data integrity
	for i := range largeData {
		if entries[0].Data[i] != largeData[i] {
			t.Errorf("Data mismatch at byte %d", i)
			break
		}
	}
}

// TestCompressedWAL_MultipleAppends tests multiple sequential appends
func TestCompressedWAL_MultipleAppends(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compressed-wal-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cw, err := NewCompressedWAL(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create compressed WAL: %v", err)
	}
	defer cw.Close()

	// Append 100 entries
	numEntries := 100
	for i := 0; i < numEntries; i++ {
		lsn, err := cw.Append(OpCreateNode, []byte("test"))
		if err != nil {
			t.Fatalf("Failed to append entry %d: %v", i, err)
		}

		if lsn != uint64(i+1) {
			t.Errorf("Entry %d: expected LSN %d, got %d", i, i+1, lsn)
		}
	}

	cw.Flush()

	// Verify all entries
	entries, err := cw.ReadAll()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if len(entries) != numEntries {
		t.Errorf("Expected %d entries, got %d", numEntries, len(entries))
	}

	// Verify LSN sequence
	for i, entry := range entries {
		if entry.LSN != uint64(i+1) {
			t.Errorf("Entry %d: expected LSN %d, got %d", i, i+1, entry.LSN)
		}
	}
}
