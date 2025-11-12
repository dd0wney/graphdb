package storage

import (
	"testing"
)

// TestCompressedEdgeList_Size tests the Size method
func TestCompressedEdgeList_Size(t *testing.T) {
	tests := []struct {
		name    string
		nodeIDs []uint64
	}{
		{"empty list", []uint64{}},
		{"single node", []uint64{1}},
		{"two nodes", []uint64{1, 2}},
		{"many nodes", []uint64{1, 2, 3, 4, 5, 10, 20, 30, 40, 50}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cel := NewCompressedEdgeList(tt.nodeIDs)

			size := cel.Size()

			// Size should be: 8 (baseNodeID) + len(deltas) + 4 (count)
			expectedMinSize := 8 + 4 // Minimum size with no deltas
			if size < expectedMinSize && len(tt.nodeIDs) > 0 {
				t.Errorf("Size too small: got %d, expected at least %d", size, expectedMinSize)
			}

			// Size should be positive
			if size < 0 {
				t.Error("Size should not be negative")
			}
		})
	}
}

// TestCompressedEdgeList_UncompressedSize tests the UncompressedSize method
func TestCompressedEdgeList_UncompressedSize(t *testing.T) {
	tests := []struct {
		name         string
		nodeIDs      []uint64
		expectedSize int
	}{
		{"empty list", []uint64{}, 0},
		{"single node", []uint64{1}, 8},
		{"two nodes", []uint64{1, 2}, 16},
		{"five nodes", []uint64{1, 2, 3, 4, 5}, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cel := NewCompressedEdgeList(tt.nodeIDs)

			size := cel.UncompressedSize()

			if size != tt.expectedSize {
				t.Errorf("Expected uncompressed size %d, got %d", tt.expectedSize, size)
			}
		})
	}
}

// TestCompressedEdgeList_CompressionRatio tests the CompressionRatio method
func TestCompressedEdgeList_CompressionRatio(t *testing.T) {
	tests := []struct {
		name    string
		nodeIDs []uint64
	}{
		{"empty list", []uint64{}},
		{"single node", []uint64{1}},
		{"sequential nodes", []uint64{1, 2, 3, 4, 5}},
		{"sparse nodes", []uint64{1, 100, 200, 300}},
		{"dense nodes", []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cel := NewCompressedEdgeList(tt.nodeIDs)

			ratio := cel.CompressionRatio()

			// For empty list, ratio should be 0
			if len(tt.nodeIDs) == 0 {
				if ratio != 0 {
					t.Errorf("Expected ratio 0 for empty list, got %.2f", ratio)
				}
				return
			}

			// Ratio should be positive
			if ratio <= 0 {
				t.Errorf("Compression ratio should be positive, got %.2f", ratio)
			}

			// Ratio should match manual calculation
			expectedRatio := float64(cel.UncompressedSize()) / float64(cel.Size())
			if ratio != expectedRatio {
				t.Errorf("Expected ratio %.2f, got %.2f", expectedRatio, ratio)
			}

			// For sequential nodes, compression should be effective (ratio > 1)
			if len(tt.nodeIDs) > 3 && tt.name == "sequential nodes" {
				if ratio <= 1.0 {
					t.Errorf("Expected good compression for sequential nodes, got ratio %.2f", ratio)
				}
			}
		})
	}
}

// TestCompressedEdgeList_Add tests adding nodes to compressed list
func TestCompressedEdgeList_Add(t *testing.T) {
	// Start with empty list
	cel := NewCompressedEdgeList([]uint64{})

	// Add first node
	cel = cel.Add(10)
	if cel.Count() != 1 {
		t.Errorf("Expected count 1 after adding first node, got %d", cel.Count())
	}

	decompressed := cel.Decompress()
	if len(decompressed) != 1 || decompressed[0] != 10 {
		t.Errorf("Expected [10], got %v", decompressed)
	}

	// Add second node
	cel = cel.Add(5)
	if cel.Count() != 2 {
		t.Errorf("Expected count 2 after adding second node, got %d", cel.Count())
	}

	decompressed = cel.Decompress()
	// Should be sorted [5, 10]
	if len(decompressed) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(decompressed))
	}
	if decompressed[0] != 5 || decompressed[1] != 10 {
		t.Errorf("Expected [5, 10], got %v", decompressed)
	}

	// Add duplicate
	cel = cel.Add(5)
	if cel.Count() != 3 {
		t.Errorf("Expected count 3 after adding duplicate, got %d", cel.Count())
	}
}

// TestCompressedEdgeList_Remove tests removing nodes from compressed list
func TestCompressedEdgeList_Remove(t *testing.T) {
	// Create list with nodes
	cel := NewCompressedEdgeList([]uint64{1, 2, 3, 4, 5})

	// Remove middle node
	cel = cel.Remove(3)
	if cel.Count() != 4 {
		t.Errorf("Expected count 4 after removal, got %d", cel.Count())
	}

	decompressed := cel.Decompress()
	expected := []uint64{1, 2, 4, 5}
	if len(decompressed) != len(expected) {
		t.Fatalf("Expected %d nodes, got %d", len(expected), len(decompressed))
	}
	for i, v := range expected {
		if decompressed[i] != v {
			t.Errorf("At index %d: expected %d, got %d", i, v, decompressed[i])
		}
	}

	// Remove first node
	cel = cel.Remove(1)
	if cel.Count() != 3 {
		t.Errorf("Expected count 3 after second removal, got %d", cel.Count())
	}

	// Remove last node
	cel = cel.Remove(5)
	if cel.Count() != 2 {
		t.Errorf("Expected count 2 after third removal, got %d", cel.Count())
	}

	// Try to remove non-existent node
	cel = cel.Remove(999)
	if cel.Count() != 2 {
		t.Errorf("Count should not change when removing non-existent node, got %d", cel.Count())
	}
}

// TestCompressedEdgeList_RemoveAll tests removing all nodes
func TestCompressedEdgeList_RemoveAll(t *testing.T) {
	cel := NewCompressedEdgeList([]uint64{42})

	cel = cel.Remove(42)

	if cel.Count() != 0 {
		t.Errorf("Expected count 0 after removing only node, got %d", cel.Count())
	}

	decompressed := cel.Decompress()
	if len(decompressed) != 0 {
		t.Errorf("Expected empty list, got %v", decompressed)
	}
}

// TestCalculateCompressionStats tests compression statistics calculation
func TestCalculateCompressionStats(t *testing.T) {
	lists := []*CompressedEdgeList{
		NewCompressedEdgeList([]uint64{1, 2, 3, 4, 5}),
		NewCompressedEdgeList([]uint64{10, 20, 30}),
		NewCompressedEdgeList([]uint64{100, 200}),
	}

	stats := CalculateCompressionStats(lists)

	// Check total lists
	if stats.TotalLists != 3 {
		t.Errorf("Expected 3 total lists, got %d", stats.TotalLists)
	}

	// Check total edges
	expectedEdges := int64(5 + 3 + 2)
	if stats.TotalEdges != expectedEdges {
		t.Errorf("Expected %d total edges, got %d", expectedEdges, stats.TotalEdges)
	}

	// Check uncompressed bytes
	expectedUncompressed := int64((5 + 3 + 2) * 8)
	if stats.UncompressedBytes != expectedUncompressed {
		t.Errorf("Expected %d uncompressed bytes, got %d", expectedUncompressed, stats.UncompressedBytes)
	}

	// Check compressed bytes
	var expectedCompressed int64
	for _, list := range lists {
		expectedCompressed += int64(list.Size())
	}
	if stats.CompressedBytes != expectedCompressed {
		t.Errorf("Expected %d compressed bytes, got %d", expectedCompressed, stats.CompressedBytes)
	}

	// Check average ratio
	expectedAvgRatio := 0.0
	for _, list := range lists {
		expectedAvgRatio += list.CompressionRatio()
	}
	expectedAvgRatio /= float64(len(lists))

	if stats.AverageRatio != expectedAvgRatio {
		t.Errorf("Expected average ratio %.2f, got %.2f", expectedAvgRatio, stats.AverageRatio)
	}

	// Compression should be effective
	if stats.CompressedBytes >= stats.UncompressedBytes {
		t.Errorf("Expected compression to reduce size, but compressed (%d) >= uncompressed (%d)",
			stats.CompressedBytes, stats.UncompressedBytes)
	}
}

// TestCalculateCompressionStats_EmptyLists tests with empty input
func TestCalculateCompressionStats_EmptyLists(t *testing.T) {
	stats := CalculateCompressionStats([]*CompressedEdgeList{})

	if stats.TotalLists != 0 {
		t.Errorf("Expected 0 total lists, got %d", stats.TotalLists)
	}

	if stats.TotalEdges != 0 {
		t.Errorf("Expected 0 total edges, got %d", stats.TotalEdges)
	}

	if stats.AverageRatio != 0 {
		t.Errorf("Expected 0 average ratio, got %.2f", stats.AverageRatio)
	}
}

// TestCalculateCompressionStats_WithEmptyList tests with mix of empty and non-empty
func TestCalculateCompressionStats_WithEmptyList(t *testing.T) {
	lists := []*CompressedEdgeList{
		NewCompressedEdgeList([]uint64{1, 2, 3}),
		NewCompressedEdgeList([]uint64{}),
		NewCompressedEdgeList([]uint64{10, 20}),
	}

	stats := CalculateCompressionStats(lists)

	if stats.TotalLists != 3 {
		t.Errorf("Expected 3 total lists, got %d", stats.TotalLists)
	}

	expectedEdges := int64(3 + 0 + 2)
	if stats.TotalEdges != expectedEdges {
		t.Errorf("Expected %d total edges, got %d", expectedEdges, stats.TotalEdges)
	}
}

// TestCompressedEdgeList_AddRemoveSequence tests a sequence of add and remove operations
func TestCompressedEdgeList_AddRemoveSequence(t *testing.T) {
	cel := NewCompressedEdgeList([]uint64{1, 5, 10})

	// Add some nodes
	cel = cel.Add(3)
	cel = cel.Add(7)

	if cel.Count() != 5 {
		t.Errorf("Expected count 5, got %d", cel.Count())
	}

	// Verify sorted order
	decompressed := cel.Decompress()
	expected := []uint64{1, 3, 5, 7, 10}
	if len(decompressed) != len(expected) {
		t.Fatalf("Expected %d nodes, got %d", len(expected), len(decompressed))
	}
	for i, v := range expected {
		if decompressed[i] != v {
			t.Errorf("At index %d: expected %d, got %d", i, v, decompressed[i])
		}
	}

	// Remove some nodes
	cel = cel.Remove(5)
	cel = cel.Remove(1)

	if cel.Count() != 3 {
		t.Errorf("Expected count 3, got %d", cel.Count())
	}

	decompressed = cel.Decompress()
	expected = []uint64{3, 7, 10}
	if len(decompressed) != len(expected) {
		t.Fatalf("Expected %d nodes, got %d", len(expected), len(decompressed))
	}
	for i, v := range expected {
		if decompressed[i] != v {
			t.Errorf("At index %d: expected %d, got %d", i, v, decompressed[i])
		}
	}
}

// TestCompressedEdgeList_SizeComparison tests that compression actually saves space
func TestCompressedEdgeList_SizeComparison(t *testing.T) {
	// Sequential numbers compress well
	sequential := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		sequential[i] = uint64(i + 1)
	}

	cel := NewCompressedEdgeList(sequential)

	compressedSize := cel.Size()
	uncompressedSize := cel.UncompressedSize()

	if compressedSize >= uncompressedSize {
		t.Errorf("Expected compression to save space: compressed=%d, uncompressed=%d",
			compressedSize, uncompressedSize)
	}

	ratio := cel.CompressionRatio()
	if ratio <= 1.0 {
		t.Errorf("Expected compression ratio > 1.0 for sequential data, got %.2f", ratio)
	}

	t.Logf("Sequential 100 nodes: compressed=%d bytes, uncompressed=%d bytes, ratio=%.2f",
		compressedSize, uncompressedSize, ratio)
}

// TestCompressedEdgeList_LargeNumbers tests compression with large node IDs
func TestCompressedEdgeList_LargeNumbers(t *testing.T) {
	nodeIDs := []uint64{
		1000000000000,
		1000000000001,
		1000000000002,
		1000000000010,
		1000000000100,
	}

	cel := NewCompressedEdgeList(nodeIDs)

	decompressed := cel.Decompress()

	if len(decompressed) != len(nodeIDs) {
		t.Fatalf("Expected %d nodes, got %d", len(nodeIDs), len(decompressed))
	}

	for i, expected := range nodeIDs {
		if decompressed[i] != expected {
			t.Errorf("At index %d: expected %d, got %d", i, expected, decompressed[i])
		}
	}
}

// Benchmarks

// BenchmarkCompressedEdgeList_NewSequential benchmarks creating compressed edge lists with sequential IDs
func BenchmarkCompressedEdgeList_NewSequential(b *testing.B) {
	// Sequential IDs compress very well
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewCompressedEdgeList(nodeIDs)
	}
}

// BenchmarkCompressedEdgeList_NewSparse benchmarks creating compressed edge lists with sparse IDs
func BenchmarkCompressedEdgeList_NewSparse(b *testing.B) {
	// Sparse IDs compress poorly
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i * 1000000)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewCompressedEdgeList(nodeIDs)
	}
}

// BenchmarkCompressedEdgeList_Decompress benchmarks decompression
func BenchmarkCompressedEdgeList_Decompress(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i + 1)
	}
	cel := NewCompressedEdgeList(nodeIDs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cel.Decompress()
	}
}

// BenchmarkCompressedEdgeList_Add benchmarks adding nodes
func BenchmarkCompressedEdgeList_Add(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i * 2)
	}
	cel := NewCompressedEdgeList(nodeIDs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cel = cel.Add(uint64(i*2 + 1))
	}
}

// BenchmarkCompressedEdgeList_Remove benchmarks removing nodes
func BenchmarkCompressedEdgeList_Remove(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i + 1)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cel := NewCompressedEdgeList(nodeIDs)
		b.StartTimer()
		cel = cel.Remove(uint64(500))
	}
}

// BenchmarkCompressedEdgeList_CompressionRatio benchmarks calculating compression ratio
func BenchmarkCompressedEdgeList_CompressionRatio(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i + 1)
	}
	cel := NewCompressedEdgeList(nodeIDs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cel.CompressionRatio()
	}
}

// BenchmarkCalculateCompressionStats benchmarks calculating statistics across multiple lists
func BenchmarkCalculateCompressionStats(b *testing.B) {
	lists := make([]*CompressedEdgeList, 100)
	for i := 0; i < 100; i++ {
		nodeIDs := make([]uint64, 100)
		for j := 0; j < 100; j++ {
			nodeIDs[j] = uint64(i*100 + j)
		}
		lists[i] = NewCompressedEdgeList(nodeIDs)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateCompressionStats(lists)
	}
}
