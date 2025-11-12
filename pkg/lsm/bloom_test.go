package lsm

import (
	"fmt"
	"testing"
)

// TestBloomFilter_BasicOperations tests basic Add/Contains
func TestBloomFilter_BasicOperations(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Add keys
	keys := [][]byte{
		[]byte("apple"),
		[]byte("banana"),
		[]byte("cherry"),
	}

	for _, key := range keys {
		bf.Add(key)
	}

	// All added keys should be found (no false negatives)
	for _, key := range keys {
		if !bf.Contains(key) {
			t.Errorf("Expected to find key %s (false negative)", key)
		}
	}

	// Keys not added might return false positives, but shouldn't crash
	notAdded := [][]byte{
		[]byte("dog"),
		[]byte("elephant"),
		[]byte("fox"),
	}

	for _, key := range notAdded {
		_ = bf.Contains(key) // Just verify no panic
	}
}

// TestBloomFilter_NoFalseNegatives tests that false negatives are impossible
func TestBloomFilter_NoFalseNegatives(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	// Add many keys
	numKeys := 500
	addedKeys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		addedKeys[i] = key
		bf.Add(key)
	}

	// Verify all added keys are found (no false negatives)
	falseNegatives := 0
	for i, key := range addedKeys {
		if !bf.Contains(key) {
			falseNegatives++
			t.Errorf("False negative for key %d: %s", i, key)
		}
	}

	if falseNegatives > 0 {
		t.Fatalf("Found %d false negatives - Bloom filter broken!", falseNegatives)
	}
}

// TestBloomFilter_FalsePositiveRate tests that false positive rate is within acceptable bounds
func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	expectedItems := 1000
	targetFPRate := 0.01 // 1%
	bf := NewBloomFilter(expectedItems, targetFPRate)

	// Add expected number of items
	for i := 0; i < expectedItems; i++ {
		key := []byte(fmt.Sprintf("added-%d", i))
		bf.Add(key)
	}

	// Test keys that were NOT added
	numTests := 10000
	falsePositives := 0
	for i := 0; i < numTests; i++ {
		key := []byte(fmt.Sprintf("notadded-%d", i))
		if bf.Contains(key) {
			falsePositives++
		}
	}

	actualFPRate := float64(falsePositives) / float64(numTests)

	// Allow some margin (3x target rate) due to randomness
	maxAcceptableFPRate := targetFPRate * 3
	if actualFPRate > maxAcceptableFPRate {
		t.Errorf("False positive rate %.4f exceeds acceptable %.4f (target: %.4f)",
			actualFPRate, maxAcceptableFPRate, targetFPRate)
	}

	t.Logf("False positive rate: %.4f (target: %.4f)", actualFPRate, targetFPRate)
}

// TestBloomFilter_EmptyFilter tests behavior with no items added
func TestBloomFilter_EmptyFilter(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Empty filter should not contain any keys
	testKeys := [][]byte{
		[]byte("test1"),
		[]byte("test2"),
		[]byte("test3"),
	}

	for _, key := range testKeys {
		// Contains might return true (false positive) but shouldn't crash
		_ = bf.Contains(key)
	}
}

// TestBloomFilter_InvalidParameters tests handling of edge case parameters
func TestBloomFilter_InvalidParameters(t *testing.T) {
	// Zero expected items - should handle gracefully
	bf1 := NewBloomFilter(0, 0.01)
	if bf1 == nil {
		t.Error("NewBloomFilter should handle zero items")
	}
	bf1.Add([]byte("test"))
	if !bf1.Contains([]byte("test")) {
		t.Error("Bloom filter with adjusted parameters should still work")
	}

	// Negative expected items - should handle gracefully
	bf2 := NewBloomFilter(-10, 0.01)
	if bf2 == nil {
		t.Error("NewBloomFilter should handle negative items")
	}

	// Invalid false positive rate (0) - should use default
	bf3 := NewBloomFilter(100, 0)
	if bf3 == nil {
		t.Error("NewBloomFilter should handle zero FP rate")
	}

	// Invalid false positive rate (1) - should use default
	bf4 := NewBloomFilter(100, 1.0)
	if bf4 == nil {
		t.Error("NewBloomFilter should handle 1.0 FP rate")
	}

	// Invalid false positive rate (>1) - should use default
	bf5 := NewBloomFilter(100, 2.0)
	if bf5 == nil {
		t.Error("NewBloomFilter should handle >1.0 FP rate")
	}
}

// TestBloomFilter_LargeKeys tests handling of large keys
func TestBloomFilter_LargeKeys(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Add very large key
	largeKey := make([]byte, 10000)
	for i := range largeKey {
		largeKey[i] = byte(i % 256)
	}

	bf.Add(largeKey)

	if !bf.Contains(largeKey) {
		t.Error("Expected to find large key")
	}
}

// TestBloomFilter_DuplicateAdds tests adding the same key multiple times
func TestBloomFilter_DuplicateAdds(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	key := []byte("duplicate-key")

	// Add same key multiple times
	for i := 0; i < 10; i++ {
		bf.Add(key)
	}

	// Should still be found
	if !bf.Contains(key) {
		t.Error("Expected to find key after duplicate adds")
	}
}

// TestBloomFilter_SizeCalculation tests that size is calculated reasonably
func TestBloomFilter_SizeCalculation(t *testing.T) {
	// Small filter
	bf1 := NewBloomFilter(10, 0.01)
	if bf1.size <= 0 {
		t.Error("Expected positive size for small filter")
	}
	if bf1.hashCount <= 0 {
		t.Error("Expected positive hash count for small filter")
	}

	// Large filter
	bf2 := NewBloomFilter(10000, 0.001)
	if bf2.size <= bf1.size {
		t.Error("Expected larger filter for more items")
	}

	// Verify size doesn't overflow to negative
	if bf2.size < 0 {
		t.Error("Filter size should never be negative")
	}

	t.Logf("Small filter: size=%d, hashCount=%d", bf1.size, bf1.hashCount)
	t.Logf("Large filter: size=%d, hashCount=%d", bf2.size, bf2.hashCount)
}

// TestBloomFilter_MaxSizeProtection tests that extremely large parameters don't cause OOM
func TestBloomFilter_MaxSizeProtection(t *testing.T) {
	// Try to create filter with huge expected items
	// Should cap at reasonable size to prevent OOM
	bf := NewBloomFilter(1000000000, 0.0000001)

	if bf == nil {
		t.Fatal("NewBloomFilter should not return nil even for huge parameters")
	}

	// Should have capped the size
	const maxSize = 1000000000 // From implementation
	if bf.size > maxSize {
		t.Errorf("Filter size %d exceeds maximum %d", bf.size, maxSize)
	}

	// Should still be usable
	bf.Add([]byte("test"))
	if !bf.Contains([]byte("test")) {
		t.Error("Capped filter should still work")
	}
}

// TestBloomFilter_EmptyKey tests handling of empty keys
func TestBloomFilter_EmptyKey(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	emptyKey := []byte{}

	// Should handle empty key without panic
	bf.Add(emptyKey)

	if !bf.Contains(emptyKey) {
		t.Error("Expected to find empty key")
	}
}

// TestBloomFilter_Size tests the Size method
func TestBloomFilter_Size(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	size := bf.Size()
	if size <= 0 {
		t.Errorf("Expected positive size, got %d", size)
	}

	// Size should match internal size field
	if size != bf.size {
		t.Errorf("Size() returned %d but internal size is %d", size, bf.size)
	}
}

// TestBloomFilter_HashCount tests the HashCount method
func TestBloomFilter_HashCount(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	hashCount := bf.HashCount()
	if hashCount <= 0 {
		t.Errorf("Expected positive hash count, got %d", hashCount)
	}

	// HashCount should match internal hashCount field
	if hashCount != bf.hashCount {
		t.Errorf("HashCount() returned %d but internal hashCount is %d", hashCount, bf.hashCount)
	}
}

// TestBloomFilter_EstimateFalsePositiveRate tests the false positive rate estimation
func TestBloomFilter_EstimateFalsePositiveRate(t *testing.T) {
	expectedItems := 1000
	targetFPRate := 0.01
	bf := NewBloomFilter(expectedItems, targetFPRate)

	// Estimate rate with no items
	rate0 := bf.EstimateFalsePositiveRate(0)
	if rate0 != 0 {
		t.Errorf("Expected 0 FP rate with 0 items, got %f", rate0)
	}

	// Estimate rate with expected number of items
	rateExpected := bf.EstimateFalsePositiveRate(expectedItems)
	// Should be close to target (within 10x for rough estimate)
	if rateExpected > targetFPRate*10 {
		t.Errorf("Estimated FP rate %f too high compared to target %f", rateExpected, targetFPRate)
	}

	// Estimate rate with more items (should increase)
	rateMore := bf.EstimateFalsePositiveRate(expectedItems * 2)
	if rateMore <= rateExpected {
		t.Error("Expected FP rate to increase with more items")
	}

	t.Logf("FP rates - 0 items: %f, expected items: %f, 2x items: %f", rate0, rateExpected, rateMore)
}

// TestBloomFilter_Reset tests clearing the filter
func TestBloomFilter_Reset(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Add some keys
	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}

	for _, key := range keys {
		bf.Add(key)
	}

	// Verify keys are present
	for _, key := range keys {
		if !bf.Contains(key) {
			t.Errorf("Expected to find key %s before reset", key)
		}
	}

	// Reset the filter
	bf.Reset()

	// All keys should be gone (no false positives expected after reset)
	for _, key := range keys {
		if bf.Contains(key) {
			t.Errorf("Key %s still found after reset", key)
		}
	}

	// Filter should still be usable
	bf.Add([]byte("newkey"))
	if !bf.Contains([]byte("newkey")) {
		t.Error("Filter should work after reset")
	}
}

// TestBloomFilter_Merge tests merging two filters
func TestBloomFilter_Merge(t *testing.T) {
	// Create two filters with same parameters
	bf1 := NewBloomFilter(100, 0.01)
	bf2 := NewBloomFilter(100, 0.01)

	// Add different keys to each filter
	keys1 := [][]byte{[]byte("key1"), []byte("key2")}
	keys2 := [][]byte{[]byte("key3"), []byte("key4")}

	for _, key := range keys1 {
		bf1.Add(key)
	}

	for _, key := range keys2 {
		bf2.Add(key)
	}

	// Merge bf2 into bf1
	err := bf1.Merge(bf2)
	if err != nil {
		t.Fatalf("Failed to merge filters: %v", err)
	}

	// bf1 should now contain all keys
	for _, key := range keys1 {
		if !bf1.Contains(key) {
			t.Errorf("Expected bf1 to contain key %s after merge", key)
		}
	}

	for _, key := range keys2 {
		if !bf1.Contains(key) {
			t.Errorf("Expected bf1 to contain key %s from bf2 after merge", key)
		}
	}

	// bf2 should still only contain its original keys
	for _, key := range keys2 {
		if !bf2.Contains(key) {
			t.Errorf("Expected bf2 to still contain key %s", key)
		}
	}
}

// TestBloomFilter_MergeIncompatible tests merging incompatible filters
func TestBloomFilter_MergeIncompatible(t *testing.T) {
	// Create filters with different sizes
	bf1 := NewBloomFilter(100, 0.01)
	bf2 := NewBloomFilter(200, 0.01)

	// Attempt to merge
	err := bf1.Merge(bf2)
	if err == nil {
		t.Error("Expected error when merging incompatible filters")
	}

	if err != ErrIncompatibleFilters {
		t.Errorf("Expected ErrIncompatibleFilters, got %v", err)
	}

	// Verify error message
	if err.Error() != "incompatible bloom filters" {
		t.Errorf("Expected error message 'incompatible bloom filters', got '%s'", err.Error())
	}
}

// TestBloomFilter_MergeSelf tests merging a filter with itself
func TestBloomFilter_MergeSelf(t *testing.T) {
	bf := NewBloomFilter(100, 0.01)

	// Add some keys
	keys := [][]byte{[]byte("key1"), []byte("key2")}
	for _, key := range keys {
		bf.Add(key)
	}

	// Merge with self (should be idempotent)
	err := bf.Merge(bf)
	if err != nil {
		t.Fatalf("Failed to merge filter with itself: %v", err)
	}

	// Keys should still be present
	for _, key := range keys {
		if !bf.Contains(key) {
			t.Errorf("Expected to find key %s after self-merge", key)
		}
	}
}

// TestBloomFilter_Error tests the error type
func TestBloomFilter_Error(t *testing.T) {
	err := ErrIncompatibleFilters

	// Check Error() method
	msg := err.Error()
	if msg != "incompatible bloom filters" {
		t.Errorf("Expected error message 'incompatible bloom filters', got '%s'", msg)
	}

	// Verify it's actually a BloomFilterError (already known to be concrete type)
	if err == nil {
		t.Error("Expected non-nil error")
	}
}
