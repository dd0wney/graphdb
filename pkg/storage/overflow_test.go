package storage

import (
	"math"
	"os"
	"testing"
)

// TestIDGenerationOverflow tests that we detect ID space exhaustion
func TestNodeIDGenerationOverflow(t *testing.T) {
	// Clean up
	os.RemoveAll("./data/test-overflow-node")
	defer os.RemoveAll("./data/test-overflow-node")

	graph, err := NewGraphStorage("./data/test-overflow-node")
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Set nextNodeID to max value - 1
	graph.nextNodeID = math.MaxUint64 - 1

	// This should succeed
	node1, err := graph.CreateNode([]string{"Test"}, nil)
	if err != nil {
		t.Fatalf("Expected first node creation to succeed: %v", err)
	}
	expectedID := uint64(math.MaxUint64 - 1)
	if node1.ID != expectedID {
		t.Errorf("Expected node ID %d, got %d", expectedID, node1.ID)
	}

	// nextNodeID should now be at max
	if graph.nextNodeID != math.MaxUint64 {
		t.Errorf("Expected nextNodeID to be MaxUint64, got %d", graph.nextNodeID)
	}

	// This should FAIL - ID space exhausted
	_, err = graph.CreateNode([]string{"Test"}, nil)
	if err == nil {
		t.Fatal("Expected error for ID space exhaustion")
	}
	if err.Error() != "node ID space exhausted" {
		t.Errorf("Expected 'node ID space exhausted', got: %v", err)
	}
}

func TestEdgeIDGenerationOverflow(t *testing.T) {
	os.RemoveAll("./data/test-overflow-edge")
	defer os.RemoveAll("./data/test-overflow-edge")

	graph, err := NewGraphStorage("./data/test-overflow-edge")
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create two nodes
	node1, _ := graph.CreateNode([]string{"Test"}, nil)
	node2, _ := graph.CreateNode([]string{"Test"}, nil)

	// Set nextEdgeID to max value - 1
	graph.nextEdgeID = math.MaxUint64 - 1

	// This should succeed
	edge1, err := graph.CreateEdge(node1.ID, node2.ID, "TEST", nil, 1.0)
	if err != nil {
		t.Fatalf("Expected first edge creation to succeed: %v", err)
	}
	expectedID := uint64(math.MaxUint64 - 1)
	if edge1.ID != expectedID {
		t.Errorf("Expected edge ID %d, got %d", expectedID, edge1.ID)
	}

	// This should FAIL - ID space exhausted
	_, err = graph.CreateEdge(node1.ID, node2.ID, "TEST", nil, 1.0)
	if err == nil {
		t.Fatal("Expected error for ID space exhaustion")
	}
	if err.Error() != "edge ID space exhausted" {
		t.Errorf("Expected 'edge ID space exhausted', got: %v", err)
	}
}

// TestCompressionOverflow tests delta encoding with edge cases
func TestCompressionDeltaUnderflow(t *testing.T) {
	// This test verifies that compression properly sorts and doesn't underflow
	nodeIDs := []uint64{100, 50, 150, 75} // Unsorted

	// Should not panic - sorts internally
	compressed := NewCompressedEdgeList(nodeIDs)

	// Decompress and verify correct order
	decompressed := compressed.Decompress()

	// Should be sorted: 50, 75, 100, 150
	expected := []uint64{50, 75, 100, 150}
	if len(decompressed) != len(expected) {
		t.Fatalf("Expected %d elements, got %d", len(expected), len(decompressed))
	}

	for i, val := range expected {
		if decompressed[i] != val {
			t.Errorf("At index %d: expected %d, got %d", i, val, decompressed[i])
		}
	}
}

func TestCompressionDecompressionOverflow(t *testing.T) {
	// Create a compressed list
	nodeIDs := []uint64{1, 2, 3}
	compressed := NewCompressedEdgeList(nodeIDs)

	// Manually corrupt the deltas to cause overflow
	// This would happen if disk corruption occurred
	compressed.deltas = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}

	// Decompression should handle overflow gracefully
	result := compressed.Decompress()

	// Should stop early when overflow detected
	if len(result) == 0 {
		t.Error("Expected at least base value in result")
	}

	// First value should still be base
	if result[0] != compressed.baseNodeID {
		t.Errorf("Expected base %d, got %d", compressed.baseNodeID, result[0])
	}
}

func TestCompressionBinarySearchOverflow(t *testing.T) {
	// Create a very large list to test binary search
	// Use values near MaxInt to test overflow in mid calculation
	size := 10000
	nodeIDs := make([]uint64, size)
	for i := 0; i < size; i++ {
		nodeIDs[i] = uint64(i * 1000)
	}

	compressed := NewCompressedEdgeList(nodeIDs)

	// Test Contains with various values
	testCases := []struct {
		value    uint64
		expected bool
	}{
		{0, true},         // First
		{4999000, true},   // Middle
		{9999000, true},   // Last
		{5, false},        // Not present
		{10000000, false}, // Beyond end
	}

	for _, tc := range testCases {
		result := compressed.Contains(tc.value)
		if result != tc.expected {
			t.Errorf("Contains(%d): expected %v, got %v", tc.value, tc.expected, result)
		}
	}
}

func TestCompressionEmptyList(t *testing.T) {
	// Test edge case: empty list
	compressed := NewCompressedEdgeList([]uint64{})

	if compressed.Count() != 0 {
		t.Errorf("Expected count 0, got %d", compressed.Count())
	}

	decompressed := compressed.Decompress()
	if len(decompressed) != 0 {
		t.Errorf("Expected empty slice, got length %d", len(decompressed))
	}

	// Contains should always return false
	if compressed.Contains(123) {
		t.Error("Expected Contains to return false for empty list")
	}
}

func TestCompressionSingleElement(t *testing.T) {
	// Test edge case: single element
	compressed := NewCompressedEdgeList([]uint64{42})

	if compressed.Count() != 1 {
		t.Errorf("Expected count 1, got %d", compressed.Count())
	}

	if compressed.baseNodeID != 42 {
		t.Errorf("Expected base 42, got %d", compressed.baseNodeID)
	}

	decompressed := compressed.Decompress()
	if len(decompressed) != 1 || decompressed[0] != 42 {
		t.Errorf("Expected [42], got %v", decompressed)
	}

	// Test Contains
	if !compressed.Contains(42) {
		t.Error("Expected Contains(42) to return true")
	}
	if compressed.Contains(43) {
		t.Error("Expected Contains(43) to return false")
	}
}

func TestCompressionMaxValues(t *testing.T) {
	// Test with values near MaxUint64
	nodeIDs := []uint64{
		math.MaxUint64 - 100,
		math.MaxUint64 - 50,
		math.MaxUint64 - 10,
		math.MaxUint64 - 1,
	}

	compressed := NewCompressedEdgeList(nodeIDs)
	decompressed := compressed.Decompress()

	if len(decompressed) != len(nodeIDs) {
		t.Errorf("Expected %d elements, got %d", len(nodeIDs), len(decompressed))
	}

	for i, expected := range nodeIDs {
		if decompressed[i] != expected {
			t.Errorf("At index %d: expected %d, got %d", i, expected, decompressed[i])
		}
	}
}

func TestCompressionDuplicates(t *testing.T) {
	// Test with duplicate values
	nodeIDs := []uint64{10, 10, 20, 20, 30}

	compressed := NewCompressedEdgeList(nodeIDs)
	decompressed := compressed.Decompress()

	// After sorting, duplicates should remain
	// Expected: [10, 10, 20, 20, 30]
	expected := []uint64{10, 10, 20, 20, 30}
	if len(decompressed) != len(expected) {
		t.Fatalf("Expected %d elements, got %d", len(expected), len(decompressed))
	}

	for i, val := range expected {
		if decompressed[i] != val {
			t.Errorf("At index %d: expected %d, got %d", i, val, decompressed[i])
		}
	}
}

// Benchmark compression/decompression
func BenchmarkCompression(b *testing.B) {
	// Create a realistic edge list
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i * 10) // Sparse IDs
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewCompressedEdgeList(nodeIDs)
	}
}

func BenchmarkDecompression(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i * 10)
	}
	compressed := NewCompressedEdgeList(nodeIDs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed.Decompress()
	}
}

func BenchmarkContains(b *testing.B) {
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		nodeIDs[i] = uint64(i * 10)
	}
	compressed := NewCompressedEdgeList(nodeIDs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed.Contains(5000) // Middle value
	}
}
