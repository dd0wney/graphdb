package storage

import (
	"sort"
	"testing"
)

// TestNewPropertyIndex tests index creation
func TestNewPropertyIndex(t *testing.T) {
	idx := NewPropertyIndex("name", TypeString)

	if idx.propertyKey != "name" {
		t.Errorf("Expected property key 'name', got %q", idx.propertyKey)
	}

	if idx.indexType != TypeString {
		t.Errorf("Expected index type TypeString, got %v", idx.indexType)
	}

	if idx.index == nil {
		t.Error("Index map should be initialized")
	}
}

// TestPropertyIndex_Insert tests inserting nodes into index
func TestPropertyIndex_Insert(t *testing.T) {
	idx := NewPropertyIndex("age", TypeInt)

	// Insert nodes
	err := idx.Insert(1, IntValue(30))
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	err = idx.Insert(2, IntValue(30))
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	err = idx.Insert(3, IntValue(25))
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Verify lookups
	nodes, err := idx.Lookup(IntValue(30))
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with age 30, got %d", len(nodes))
	}

	// Should contain both node 1 and 2
	hasNode1 := false
	hasNode2 := false
	for _, id := range nodes {
		if id == 1 {
			hasNode1 = true
		}
		if id == 2 {
			hasNode2 = true
		}
	}

	if !hasNode1 || !hasNode2 {
		t.Error("Expected to find both node 1 and 2")
	}
}

// TestPropertyIndex_InsertTypeMismatch tests type validation
func TestPropertyIndex_InsertTypeMismatch(t *testing.T) {
	idx := NewPropertyIndex("age", TypeInt)

	// Try to insert string value into int index
	err := idx.Insert(1, StringValue("30"))
	if err == nil {
		t.Error("Expected error when inserting wrong type")
	}
}

// TestPropertyIndex_Remove tests removing nodes from index
func TestPropertyIndex_Remove(t *testing.T) {
	idx := NewPropertyIndex("status", TypeString)

	// Insert nodes
	idx.Insert(1, StringValue("active"))
	idx.Insert(2, StringValue("active"))
	idx.Insert(3, StringValue("inactive"))

	// Remove node 1
	err := idx.Remove(1, StringValue("active"))
	if err != nil {
		t.Fatalf("Failed to remove: %v", err)
	}

	// Verify node 1 is gone
	nodes, _ := idx.Lookup(StringValue("active"))
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node after removal, got %d", len(nodes))
	}

	if nodes[0] != 2 {
		t.Errorf("Expected node 2 to remain, got %d", nodes[0])
	}

	// Verify node 3 is still there
	nodes, _ = idx.Lookup(StringValue("inactive"))
	if len(nodes) != 1 || nodes[0] != 3 {
		t.Error("Node 3 should still be in index")
	}
}

// TestPropertyIndex_RemoveLastNode tests removing the last node with a value
func TestPropertyIndex_RemoveLastNode(t *testing.T) {
	idx := NewPropertyIndex("status", TypeString)

	// Insert single node
	idx.Insert(1, StringValue("pending"))

	// Remove it
	err := idx.Remove(1, StringValue("pending"))
	if err != nil {
		t.Fatalf("Failed to remove: %v", err)
	}

	// Verify the key is deleted from index
	nodes, _ := idx.Lookup(StringValue("pending"))
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes after removing last node, got %d", len(nodes))
	}

	// Verify key is removed from map
	keys := idx.GetAllKeys()
	if len(keys) != 0 {
		t.Error("Expected empty index after removing last node")
	}
}

// TestPropertyIndex_RemoveNonExistent tests removing a node that doesn't exist
func TestPropertyIndex_RemoveNonExistent(t *testing.T) {
	idx := NewPropertyIndex("name", TypeString)

	idx.Insert(1, StringValue("Alice"))

	// Try to remove non-existent node
	err := idx.Remove(999, StringValue("Alice"))
	if err == nil {
		t.Error("Expected error when removing non-existent node")
	}

	// Try to remove from non-existent key
	err = idx.Remove(1, StringValue("Bob"))
	if err == nil {
		t.Error("Expected error when removing from non-existent key")
	}
}

// TestPropertyIndex_Lookup tests basic lookup
func TestPropertyIndex_Lookup(t *testing.T) {
	idx := NewPropertyIndex("city", TypeString)

	idx.Insert(1, StringValue("NYC"))
	idx.Insert(2, StringValue("LA"))
	idx.Insert(3, StringValue("NYC"))

	// Lookup NYC
	nodes, err := idx.Lookup(StringValue("NYC"))
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes in NYC, got %d", len(nodes))
	}

	// Lookup non-existent
	nodes, _ = idx.Lookup(StringValue("SF"))
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes in SF, got %d", len(nodes))
	}
}

// TestPropertyIndex_LookupTypeMismatch tests type validation in lookup
func TestPropertyIndex_LookupTypeMismatch(t *testing.T) {
	idx := NewPropertyIndex("age", TypeInt)

	idx.Insert(1, IntValue(30))

	// Try to lookup with wrong type
	_, err := idx.Lookup(StringValue("30"))
	if err == nil {
		t.Error("Expected error when looking up with wrong type")
	}
}

// TestPropertyIndex_LookupReturnsIsolatedCopy tests that lookup returns a copy
func TestPropertyIndex_LookupReturnsIsolatedCopy(t *testing.T) {
	idx := NewPropertyIndex("tag", TypeString)

	idx.Insert(1, StringValue("important"))
	idx.Insert(2, StringValue("important"))

	// Get results
	nodes, _ := idx.Lookup(StringValue("important"))

	// Modify the returned slice
	originalLen := len(nodes)
	nodes = append(nodes, 999)
	nodes[0] = 888

	// Lookup again - should be unchanged
	nodes2, _ := idx.Lookup(StringValue("important"))
	if len(nodes2) != originalLen {
		t.Error("External modification affected index")
	}

	if nodes2[0] == 888 {
		t.Error("External modification changed index values")
	}
}

// TestPropertyIndex_RangeLookup tests range queries
func TestPropertyIndex_RangeLookup(t *testing.T) {
	idx := NewPropertyIndex("score", TypeInt)

	// Insert nodes with scores
	idx.Insert(1, IntValue(10))
	idx.Insert(2, IntValue(20))
	idx.Insert(3, IntValue(30))
	idx.Insert(4, IntValue(40))
	idx.Insert(5, IntValue(50))

	// Range lookup [20, 40]
	nodes, err := idx.RangeLookup(IntValue(20), IntValue(40))
	if err != nil {
		t.Fatalf("RangeLookup failed: %v", err)
	}

	// Should include nodes 2, 3, 4 (scores 20, 30, 40)
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes in range [20, 40], got %d", len(nodes))
	}
}

// TestPropertyIndex_RangeLookupNegativeNumbers tests range with negative integers
func TestPropertyIndex_RangeLookupNegativeNumbers(t *testing.T) {
	idx := NewPropertyIndex("temperature", TypeInt)

	// Insert negative and positive values
	idx.Insert(1, IntValue(-10))
	idx.Insert(2, IntValue(-5))
	idx.Insert(3, IntValue(0))
	idx.Insert(4, IntValue(5))
	idx.Insert(5, IntValue(10))

	// Range lookup [-5, 5]
	nodes, err := idx.RangeLookup(IntValue(-5), IntValue(5))
	if err != nil {
		t.Fatalf("RangeLookup failed: %v", err)
	}

	// Should include nodes 2, 3, 4 (temps -5, 0, 5)
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes in range [-5, 5], got %d", len(nodes))
	}
}

// TestPropertyIndex_RangeLookupFloats tests range with floats
func TestPropertyIndex_RangeLookupFloats(t *testing.T) {
	idx := NewPropertyIndex("price", TypeFloat)

	idx.Insert(1, FloatValue(10.5))
	idx.Insert(2, FloatValue(20.3))
	idx.Insert(3, FloatValue(30.7))
	idx.Insert(4, FloatValue(40.1))

	// Range lookup [15.0, 35.0]
	nodes, err := idx.RangeLookup(FloatValue(15.0), FloatValue(35.0))
	if err != nil {
		t.Fatalf("RangeLookup failed: %v", err)
	}

	// Should include nodes 2, 3 (prices 20.3, 30.7)
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes in range [15.0, 35.0], got %d", len(nodes))
	}
}

// TestPropertyIndex_PrefixLookup tests prefix search
func TestPropertyIndex_PrefixLookup(t *testing.T) {
	idx := NewPropertyIndex("email", TypeString)

	idx.Insert(1, StringValue("alice@example.com"))
	idx.Insert(2, StringValue("alice@test.com"))
	idx.Insert(3, StringValue("bob@example.com"))
	idx.Insert(4, StringValue("alison@example.com"))

	// Prefix lookup "alice"
	nodes, err := idx.PrefixLookup("alice")
	if err != nil {
		t.Fatalf("PrefixLookup failed: %v", err)
	}

	// Should find alice@example.com and alice@test.com
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with prefix 'alice', got %d", len(nodes))
	}
}

// TestPropertyIndex_PrefixLookupEmptyPrefix tests prefix with empty string
func TestPropertyIndex_PrefixLookupEmptyPrefix(t *testing.T) {
	idx := NewPropertyIndex("name", TypeString)

	idx.Insert(1, StringValue("Alice"))
	idx.Insert(2, StringValue("Bob"))

	// Empty prefix should match all
	nodes, err := idx.PrefixLookup("")
	if err != nil {
		t.Fatalf("PrefixLookup failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with empty prefix, got %d", len(nodes))
	}
}

// TestPropertyIndex_PrefixLookupWrongType tests prefix on non-string index
func TestPropertyIndex_PrefixLookupWrongType(t *testing.T) {
	idx := NewPropertyIndex("age", TypeInt)

	idx.Insert(1, IntValue(30))

	// Try prefix lookup on int index
	_, err := idx.PrefixLookup("30")
	if err == nil {
		t.Error("Expected error when doing prefix lookup on non-string index")
	}
}

// TestPropertyIndex_GetStatistics tests statistics
func TestPropertyIndex_GetStatistics(t *testing.T) {
	idx := NewPropertyIndex("category", TypeString)

	// Insert nodes
	idx.Insert(1, StringValue("A"))
	idx.Insert(2, StringValue("A"))
	idx.Insert(3, StringValue("A"))
	idx.Insert(4, StringValue("B"))
	idx.Insert(5, StringValue("B"))
	idx.Insert(6, StringValue("C"))

	stats := idx.GetStatistics()

	if stats.PropertyKey != "category" {
		t.Errorf("Expected property key 'category', got %q", stats.PropertyKey)
	}

	if stats.UniqueValues != 3 {
		t.Errorf("Expected 3 unique values, got %d", stats.UniqueValues)
	}

	if stats.TotalNodes != 6 {
		t.Errorf("Expected 6 total nodes, got %d", stats.TotalNodes)
	}

	expectedAvg := 6.0 / 3.0
	if stats.AvgNodesPerKey != expectedAvg {
		t.Errorf("Expected average %.2f, got %.2f", expectedAvg, stats.AvgNodesPerKey)
	}
}

// TestPropertyIndex_GetStatisticsEmpty tests statistics on empty index
func TestPropertyIndex_GetStatisticsEmpty(t *testing.T) {
	idx := NewPropertyIndex("status", TypeString)

	stats := idx.GetStatistics()

	if stats.UniqueValues != 0 {
		t.Errorf("Expected 0 unique values, got %d", stats.UniqueValues)
	}

	if stats.TotalNodes != 0 {
		t.Errorf("Expected 0 total nodes, got %d", stats.TotalNodes)
	}

	// Average should be 0 for empty index (uses max(len, 1) to avoid divide by zero)
	if stats.AvgNodesPerKey != 0 {
		t.Errorf("Expected average 0, got %.2f", stats.AvgNodesPerKey)
	}
}

// TestPropertyIndex_GetAllKeys tests key retrieval
func TestPropertyIndex_GetAllKeys(t *testing.T) {
	idx := NewPropertyIndex("status", TypeString)

	idx.Insert(1, StringValue("active"))
	idx.Insert(2, StringValue("pending"))
	idx.Insert(3, StringValue("active"))

	keys := idx.GetAllKeys()

	// Should return sorted keys
	expectedKeys := []string{"active", "pending"}
	if len(keys) != len(expectedKeys) {
		t.Errorf("Expected %d keys, got %d", len(expectedKeys), len(keys))
	}

	// Verify sorted
	if !sort.StringsAreSorted(keys) {
		t.Error("Keys should be sorted")
	}

	// Verify keys match
	for i, expected := range expectedKeys {
		if keys[i] != expected {
			t.Errorf("Expected key %q at position %d, got %q", expected, i, keys[i])
		}
	}
}

// TestPropertyIndex_ValueToKeyInt tests integer key conversion
func TestPropertyIndex_ValueToKeyInt(t *testing.T) {
	idx := NewPropertyIndex("num", TypeInt)

	// Test that numeric keys are zero-padded for proper sorting
	idx.Insert(1, IntValue(5))
	idx.Insert(2, IntValue(50))
	idx.Insert(3, IntValue(500))

	keys := idx.GetAllKeys()

	// Keys should be sorted numerically (not lexically)
	// 5 should come before 50 should come before 500
	if len(keys) != 3 {
		t.Fatalf("Expected 3 keys, got %d", len(keys))
	}

	// Verify the order by looking up in range
	// Range [0, 100] should return nodes 1 and 2
	nodes, _ := idx.RangeLookup(IntValue(0), IntValue(100))
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes in range [0, 100], got %d", len(nodes))
	}
}

// TestPropertyIndex_ValueToKeyBool tests boolean key conversion
func TestPropertyIndex_ValueToKeyBool(t *testing.T) {
	idx := NewPropertyIndex("active", TypeBool)

	idx.Insert(1, BoolValue(true))
	idx.Insert(2, BoolValue(false))
	idx.Insert(3, BoolValue(true))

	// Lookup true
	nodes, _ := idx.Lookup(BoolValue(true))
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with true, got %d", len(nodes))
	}

	// Lookup false
	nodes, _ = idx.Lookup(BoolValue(false))
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node with false, got %d", len(nodes))
	}
}

// TestPropertyIndex_ConcurrentAccess tests thread safety
func TestPropertyIndex_ConcurrentAccess(t *testing.T) {
	idx := NewPropertyIndex("counter", TypeInt)

	// Insert initial data
	for i := 0; i < 100; i++ {
		idx.Insert(uint64(i), IntValue(int64(i%10)))
	}

	// Concurrent reads and writes
	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 100; i < 200; i++ {
			idx.Insert(uint64(i), IntValue(int64(i%10)))
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			idx.Lookup(IntValue(int64(i % 10)))
		}
		done <- true
	}()

	// Remover goroutine
	go func() {
		for i := 0; i < 50; i++ {
			idx.Remove(uint64(i), IntValue(int64(i%10)))
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	// Verify index is still valid
	stats := idx.GetStatistics()
	if stats.UniqueValues == 0 {
		t.Error("Index should still have values after concurrent access")
	}
}

// TestPropertyIndex_TypesAllTypes tests all value types
func TestPropertyIndex_AllTypes(t *testing.T) {
	tests := []struct {
		name      string
		indexType ValueType
		value     Value
	}{
		{"string", TypeString, StringValue("test")},
		{"int", TypeInt, IntValue(42)},
		{"float", TypeFloat, FloatValue(3.14)},
		{"bool", TypeBool, BoolValue(true)},
		{"bytes", TypeBytes, BytesValue([]byte{1, 2, 3})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewPropertyIndex("prop", tt.indexType)

			err := idx.Insert(1, tt.value)
			if err != nil {
				t.Fatalf("Failed to insert %s: %v", tt.name, err)
			}

			nodes, err := idx.Lookup(tt.value)
			if err != nil {
				t.Fatalf("Failed to lookup %s: %v", tt.name, err)
			}

			if len(nodes) != 1 || nodes[0] != 1 {
				t.Errorf("Expected to find node 1 for %s", tt.name)
			}
		})
	}
}

// Benchmarks

// BenchmarkPropertyIndex_Insert benchmarks inserting into index
func BenchmarkPropertyIndex_Insert(b *testing.B) {
	idx := NewPropertyIndex("prop", TypeInt)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Insert(uint64(i), IntValue(int64(i%1000)))
	}
}

// BenchmarkPropertyIndex_Lookup benchmarks point lookups
func BenchmarkPropertyIndex_Lookup(b *testing.B) {
	idx := NewPropertyIndex("prop", TypeInt)

	// Pre-populate index
	for i := 0; i < 10000; i++ {
		idx.Insert(uint64(i), IntValue(int64(i%100)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Lookup(IntValue(int64(i % 100)))
	}
}

// BenchmarkPropertyIndex_RangeLookup benchmarks range queries
func BenchmarkPropertyIndex_RangeLookup(b *testing.B) {
	idx := NewPropertyIndex("prop", TypeInt)

	// Pre-populate index with sequential numbers
	for i := 0; i < 10000; i++ {
		idx.Insert(uint64(i), IntValue(int64(i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.RangeLookup(IntValue(1000), IntValue(2000))
	}
}

// BenchmarkPropertyIndex_PrefixLookup benchmarks prefix lookups
func BenchmarkPropertyIndex_PrefixLookup(b *testing.B) {
	idx := NewPropertyIndex("name", TypeString)

	// Pre-populate with strings
	prefixes := []string{"alice", "bob", "charlie", "david", "eve"}
	for i := 0; i < 10000; i++ {
		prefix := prefixes[i%len(prefixes)]
		idx.Insert(uint64(i), StringValue(prefix+string(rune('0'+i%10))))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.PrefixLookup("alice")
	}
}

// BenchmarkPropertyIndex_Remove benchmarks removing from index
func BenchmarkPropertyIndex_Remove(b *testing.B) {
	// Pre-populate index
	idx := NewPropertyIndex("prop", TypeInt)
	for i := 0; i < b.N; i++ {
		idx.Insert(uint64(i), IntValue(int64(i%1000)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Remove(uint64(i), IntValue(int64(i%1000)))
	}
}

// BenchmarkPropertyIndex_GetStatistics benchmarks statistics calculation
func BenchmarkPropertyIndex_GetStatistics(b *testing.B) {
	idx := NewPropertyIndex("prop", TypeInt)

	// Pre-populate index
	for i := 0; i < 10000; i++ {
		idx.Insert(uint64(i), IntValue(int64(i%100)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.GetStatistics()
	}
}

// === Composite Index Tests ===

// TestNewCompositeIndex tests composite index creation
func TestNewCompositeIndex(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	keys := idx.PropertyKeys()
	if len(keys) != 2 {
		t.Errorf("Expected 2 property keys, got %d", len(keys))
	}
	if keys[0] != "domain" || keys[1] != "status" {
		t.Errorf("Unexpected keys: %v", keys)
	}
}

// TestCompositeIndex_InsertAndLookup tests basic insert and lookup
func TestCompositeIndex_InsertAndLookup(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	// Insert nodes
	err := idx.Insert(1, []Value{StringValue("math"), StringValue("active")})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	err = idx.Insert(2, []Value{StringValue("math"), StringValue("active")})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	err = idx.Insert(3, []Value{StringValue("math"), StringValue("archived")})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	err = idx.Insert(4, []Value{StringValue("science"), StringValue("active")})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Lookup (math, active)
	nodes, err := idx.Lookup([]Value{StringValue("math"), StringValue("active")})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}

	// Lookup (math, archived)
	nodes, err = idx.Lookup([]Value{StringValue("math"), StringValue("archived")})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0] != 3 {
		t.Errorf("Expected [3], got %v", nodes)
	}

	// Lookup non-existent
	nodes, err = idx.Lookup([]Value{StringValue("physics"), StringValue("active")})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(nodes))
	}
}

// TestCompositeIndex_PrefixLookup tests prefix-based lookups
func TestCompositeIndex_PrefixLookup(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	// Insert nodes
	idx.Insert(1, []Value{StringValue("math"), StringValue("active")})
	idx.Insert(2, []Value{StringValue("math"), StringValue("archived")})
	idx.Insert(3, []Value{StringValue("math"), StringValue("draft")})
	idx.Insert(4, []Value{StringValue("science"), StringValue("active")})

	// Prefix lookup for just "math" domain
	nodes, err := idx.PrefixLookup([]Value{StringValue("math")})
	if err != nil {
		t.Fatalf("PrefixLookup failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes for domain=math, got %d", len(nodes))
	}

	// Prefix lookup for "science" domain
	nodes, err = idx.PrefixLookup([]Value{StringValue("science")})
	if err != nil {
		t.Fatalf("PrefixLookup failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("Expected 1 node for domain=science, got %d", len(nodes))
	}
}

// TestCompositeIndex_Remove tests removing nodes from index
func TestCompositeIndex_Remove(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	idx.Insert(1, []Value{StringValue("math"), StringValue("active")})
	idx.Insert(2, []Value{StringValue("math"), StringValue("active")})

	// Remove node 1
	err := idx.Remove(1, []Value{StringValue("math"), StringValue("active")})
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Lookup should only find node 2
	nodes, _ := idx.Lookup([]Value{StringValue("math"), StringValue("active")})
	if len(nodes) != 1 || nodes[0] != 2 {
		t.Errorf("Expected [2], got %v", nodes)
	}

	// Remove node 2
	err = idx.Remove(2, []Value{StringValue("math"), StringValue("active")})
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Lookup should find nothing
	nodes, _ = idx.Lookup([]Value{StringValue("math"), StringValue("active")})
	if len(nodes) != 0 {
		t.Errorf("Expected empty, got %v", nodes)
	}
}

// TestCompositeIndex_MixedTypes tests composite index with different value types
func TestCompositeIndex_MixedTypes(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"category", "priority"},
		[]ValueType{TypeString, TypeInt},
	)

	idx.Insert(1, []Value{StringValue("bug"), IntValue(1)})
	idx.Insert(2, []Value{StringValue("bug"), IntValue(2)})
	idx.Insert(3, []Value{StringValue("feature"), IntValue(1)})

	// Lookup exact match
	nodes, err := idx.Lookup([]Value{StringValue("bug"), IntValue(1)})
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}
	if len(nodes) != 1 || nodes[0] != 1 {
		t.Errorf("Expected [1], got %v", nodes)
	}

	// Prefix lookup for all bugs
	nodes, err = idx.PrefixLookup([]Value{StringValue("bug")})
	if err != nil {
		t.Fatalf("PrefixLookup failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("Expected 2 bugs, got %d", len(nodes))
	}
}

// TestCompositeIndex_ThreeProperties tests index with 3+ properties
func TestCompositeIndex_ThreeProperties(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"region", "type", "status"},
		[]ValueType{TypeString, TypeString, TypeString},
	)

	idx.Insert(1, []Value{StringValue("us"), StringValue("user"), StringValue("active")})
	idx.Insert(2, []Value{StringValue("us"), StringValue("user"), StringValue("inactive")})
	idx.Insert(3, []Value{StringValue("us"), StringValue("admin"), StringValue("active")})
	idx.Insert(4, []Value{StringValue("eu"), StringValue("user"), StringValue("active")})

	// Full key lookup
	nodes, _ := idx.Lookup([]Value{StringValue("us"), StringValue("user"), StringValue("active")})
	if len(nodes) != 1 || nodes[0] != 1 {
		t.Errorf("Full key: expected [1], got %v", nodes)
	}

	// One-key prefix (all US)
	nodes, _ = idx.PrefixLookup([]Value{StringValue("us")})
	if len(nodes) != 3 {
		t.Errorf("One-key prefix: expected 3, got %d", len(nodes))
	}

	// Two-key prefix (US users)
	nodes, _ = idx.PrefixLookup([]Value{StringValue("us"), StringValue("user")})
	if len(nodes) != 2 {
		t.Errorf("Two-key prefix: expected 2, got %d", len(nodes))
	}
}

// TestCompositeIndex_Statistics tests statistics gathering
func TestCompositeIndex_Statistics(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"a", "b"},
		[]ValueType{TypeString, TypeString},
	)

	idx.Insert(1, []Value{StringValue("x"), StringValue("1")})
	idx.Insert(2, []Value{StringValue("x"), StringValue("1")})
	idx.Insert(3, []Value{StringValue("x"), StringValue("2")})
	idx.Insert(4, []Value{StringValue("y"), StringValue("1")})

	stats := idx.GetStatistics()
	if len(stats.PropertyKeys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(stats.PropertyKeys))
	}
	if stats.UniqueKeys != 3 {
		t.Errorf("Expected 3 unique keys, got %d", stats.UniqueKeys)
	}
	if stats.TotalNodes != 4 {
		t.Errorf("Expected 4 total nodes, got %d", stats.TotalNodes)
	}
}

// TestCompositeIndex_TypeMismatch tests error handling for type mismatches
func TestCompositeIndex_TypeMismatch(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"name", "age"},
		[]ValueType{TypeString, TypeInt},
	)

	// Wrong type on insert
	err := idx.Insert(1, []Value{StringValue("Alice"), StringValue("30")}) // Should be IntValue
	if err == nil {
		t.Error("Expected error for type mismatch on insert")
	}

	// Wrong type on lookup
	idx.Insert(1, []Value{StringValue("Alice"), IntValue(30)})
	_, err = idx.Lookup([]Value{StringValue("Alice"), StringValue("30")})
	if err == nil {
		t.Error("Expected error for type mismatch on lookup")
	}
}

// TestCompositeIndex_WrongNumberOfValues tests error handling for wrong value count
func TestCompositeIndex_WrongNumberOfValues(t *testing.T) {
	idx := NewCompositeIndex(
		[]string{"a", "b"},
		[]ValueType{TypeString, TypeString},
	)

	// Too few values on insert
	err := idx.Insert(1, []Value{StringValue("x")})
	if err == nil {
		t.Error("Expected error for too few values on insert")
	}

	// Too many values on insert
	err = idx.Insert(1, []Value{StringValue("x"), StringValue("y"), StringValue("z")})
	if err == nil {
		t.Error("Expected error for too many values on insert")
	}

	// Too many values on prefix lookup
	idx.Insert(1, []Value{StringValue("x"), StringValue("y")})
	_, err = idx.PrefixLookup([]Value{StringValue("x"), StringValue("y"), StringValue("z")})
	if err == nil {
		t.Error("Expected error for too many values on prefix lookup")
	}

	// Zero values on prefix lookup
	_, err = idx.PrefixLookup([]Value{})
	if err == nil {
		t.Error("Expected error for empty prefix lookup")
	}
}

// BenchmarkCompositeIndex_Lookup benchmarks composite index lookups
func BenchmarkCompositeIndex_Lookup(b *testing.B) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	domains := []string{"math", "science", "history", "art", "music"}
	statuses := []string{"active", "archived", "draft"}

	// Populate with 10k nodes
	for i := 0; i < 10000; i++ {
		domain := domains[i%len(domains)]
		status := statuses[i%len(statuses)]
		idx.Insert(uint64(i), []Value{StringValue(domain), StringValue(status)})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Lookup([]Value{StringValue("math"), StringValue("active")})
	}
}

// BenchmarkCompositeIndex_PrefixLookup benchmarks prefix lookups
func BenchmarkCompositeIndex_PrefixLookup(b *testing.B) {
	idx := NewCompositeIndex(
		[]string{"domain", "status"},
		[]ValueType{TypeString, TypeString},
	)

	domains := []string{"math", "science", "history", "art", "music"}
	statuses := []string{"active", "archived", "draft"}

	for i := 0; i < 10000; i++ {
		domain := domains[i%len(domains)]
		status := statuses[i%len(statuses)]
		idx.Insert(uint64(i), []Value{StringValue(domain), StringValue(status)})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.PrefixLookup([]Value{StringValue("math")})
	}
}
