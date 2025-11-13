package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestGraphStorage_CreateNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create a node
	node, err := gs.CreateNode(
		[]string{"User", "Verified"},
		map[string]Value{
			"id":         StringValue("user123"),
			"trustScore": IntValue(750),
			"active":     BoolValue(true),
		},
	)

	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	if node.ID != 1 {
		t.Errorf("Expected node ID 1, got %d", node.ID)
	}

	if len(node.Labels) != 2 {
		t.Errorf("Expected 2 labels, got %d", len(node.Labels))
	}

	if !node.HasLabel("User") {
		t.Error("Node should have 'User' label")
	}

	// Verify property values
	idVal, ok := node.GetProperty("id")
	if !ok {
		t.Fatal("Property 'id' not found")
	}

	id, err := idVal.AsString()
	if err != nil {
		t.Fatalf("Failed to decode id: %v", err)
	}

	if id != "user123" {
		t.Errorf("Expected id 'user123', got '%s'", id)
	}

	trustScore, ok := node.GetProperty("trustScore")
	if !ok {
		t.Fatal("Property 'trustScore' not found")
	}

	score, err := trustScore.AsInt()
	if err != nil {
		t.Fatalf("Failed to decode trustScore: %v", err)
	}

	if score != 750 {
		t.Errorf("Expected trustScore 750, got %d", score)
	}
}

func TestGraphStorage_CreateEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create two nodes
	node1, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user1")},
	)

	node2, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user2")},
	)

	// Create an edge between them
	edge, err := gs.CreateEdge(
		node1.ID,
		node2.ID,
		"VERIFIED_BY",
		map[string]Value{
			"timestamp":  TimestampValue(time.Now()),
			"confidence": FloatValue(0.95),
		},
		1.0,
	)

	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	if edge.FromNodeID != node1.ID {
		t.Errorf("Expected FromNodeID %d, got %d", node1.ID, edge.FromNodeID)
	}

	if edge.ToNodeID != node2.ID {
		t.Errorf("Expected ToNodeID %d, got %d", node2.ID, edge.ToNodeID)
	}

	if edge.Type != "VERIFIED_BY" {
		t.Errorf("Expected edge type 'VERIFIED_BY', got '%s'", edge.Type)
	}

	// Verify edge can be retrieved
	retrievedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to get edge: %v", err)
	}

	if retrievedEdge.ID != edge.ID {
		t.Errorf("Retrieved edge ID mismatch")
	}
}

func TestGraphStorage_GetOutgoingEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node3, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create edges from node1 to node2 and node3
	gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "VERIFIED_BY", map[string]Value{}, 1.0)

	// Get outgoing edges
	edges, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get outgoing edges: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 outgoing edges, got %d", len(edges))
	}
}

func TestGraphStorage_FindNodesByLabel(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with different labels
	gs.CreateNode([]string{"User"}, map[string]Value{})
	gs.CreateNode([]string{"User", "Verified"}, map[string]Value{})
	gs.CreateNode([]string{"Book"}, map[string]Value{})

	// Find User nodes
	userNodes, err := gs.FindNodesByLabel("User")
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(userNodes) != 2 {
		t.Errorf("Expected 2 User nodes, got %d", len(userNodes))
	}

	// Find Book nodes
	bookNodes, err := gs.FindNodesByLabel("Book")
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(bookNodes) != 1 {
		t.Errorf("Expected 1 Book node, got %d", len(bookNodes))
	}
}

func TestGraphStorage_DeleteNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)

	// Delete node1
	err = gs.DeleteNode(node1.ID)
	if err != nil {
		t.Fatalf("Failed to delete node: %v", err)
	}

	// Verify node is gone
	_, err = gs.GetNode(node1.ID)
	if err != ErrNodeNotFound {
		t.Error("Node should not exist after deletion")
	}

	// Verify statistics updated
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", stats.NodeCount)
	}
}

func TestGraphStorage_Snapshot(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create some data
	node1, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user1")},
	)
	node2, _ := gs.CreateNode(
		[]string{"User"},
		map[string]Value{"id": StringValue("user2")},
	)
	gs.CreateEdge(node1.ID, node2.ID, "VERIFIED_BY", map[string]Value{}, 1.0)

	// Save snapshot
	err = gs.Snapshot()
	if err != nil {
		t.Fatalf("Failed to save snapshot: %v", err)
	}

	// Close storage
	gs.Close()

	// Load from disk
	gs2, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to load from disk: %v", err)
	}
	defer gs2.Close()

	// Verify data was restored
	stats := gs2.GetStatistics()
	if stats.NodeCount != 2 {
		t.Errorf("Expected 2 nodes after reload, got %d", stats.NodeCount)
	}

	if stats.EdgeCount != 1 {
		t.Errorf("Expected 1 edge after reload, got %d", stats.EdgeCount)
	}

	// Verify node data
	reloadedNode, err := gs2.GetNode(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get node after reload: %v", err)
	}

	idVal, _ := reloadedNode.GetProperty("id")
	id, _ := idVal.AsString()
	if id != "user1" {
		t.Errorf("Expected id 'user1', got '%s'", id)
	}
}

func TestGraphStorage_FindNodesByProperty(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with different trust scores
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(750)},
	)
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(850)},
	)
	gs.CreateNode(
		[]string{"User"},
		map[string]Value{"trustScore": IntValue(750)},
	)

	// Find nodes with trustScore = 750
	nodes, err := gs.FindNodesByProperty("trustScore", IntValue(750))
	if err != nil {
		t.Fatalf("Failed to find nodes: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes with trustScore 750, got %d", len(nodes))
	}
}

func BenchmarkGraphStorage_CreateNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateNode(
			[]string{"User"},
			map[string]Value{"id": StringValue("user")},
		)
	}
}

func BenchmarkGraphStorage_CreateEdge(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-create nodes
	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
	node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}
}

func BenchmarkGraphStorage_GetNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	node, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetNode(node.ID)
	}
}

func BenchmarkGraphStorage_GetOutgoingEdges(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	node1, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create 10 outgoing edges
	for i := 0; i < 10; i++ {
		node2, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		gs.CreateEdge(node1.ID, node2.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetOutgoingEdges(node1.ID)
	}
}

// BenchmarkGraphStorage_Snapshot benchmarks creating snapshots
func BenchmarkGraphStorage_Snapshot(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with 1000 nodes and 3000 edges
	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		node, _ := gs.CreateNode([]string{"User"}, map[string]Value{
			"id": StringValue(fmt.Sprintf("user%d", i)),
		})
		nodeIDs[i] = node.ID
	}

	for i := 0; i < 3000; i++ {
		from := nodeIDs[i%1000]
		to := nodeIDs[(i+1)%1000]
		gs.CreateEdge(from, to, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.Snapshot()
	}
}

// BenchmarkGraphStorage_FindNodesByLabel benchmarks label-based lookups
func BenchmarkGraphStorage_FindNodesByLabel(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with mixed labels
	for i := 0; i < 1000; i++ {
		labels := []string{"User"}
		if i%2 == 0 {
			labels = append(labels, "Verified")
		}
		gs.CreateNode(labels, map[string]Value{})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.FindNodesByLabel("Verified")
	}
}

// BenchmarkGraphStorage_FindNodesByProperty benchmarks property-based lookups
func BenchmarkGraphStorage_FindNodesByProperty(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-populate with properties
	for i := 0; i < 1000; i++ {
		gs.CreateNode([]string{"User"}, map[string]Value{
			"trustScore": IntValue(int64(i % 100)),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.FindNodesByProperty("trustScore", IntValue(50))
	}
}

// BenchmarkGraphStorage_DeleteNode benchmarks node deletion
func BenchmarkGraphStorage_DeleteNode(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Pre-create nodes for deletion
	nodeIDs := make([]uint64, b.N)
	for i := 0; i < b.N; i++ {
		node, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.DeleteNode(nodeIDs[i])
	}
}

// BenchmarkGraphStorage_GetIncomingEdges benchmarks getting incoming edges
func BenchmarkGraphStorage_GetIncomingEdges(b *testing.B) {
	dataDir := b.TempDir()
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()

	// Create target node
	target, _ := gs.CreateNode([]string{"User"}, map[string]Value{})

	// Create 10 nodes pointing to target
	for i := 0; i < 10; i++ {
		source, _ := gs.CreateNode([]string{"User"}, map[string]Value{})
		gs.CreateEdge(source.ID, target.ID, "FOLLOWS", map[string]Value{}, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetIncomingEdges(target.ID)
	}
}

// TestGraphStorage_GetCurrentLSN tests retrieving the current LSN from storage
func TestGraphStorage_GetCurrentLSN(t *testing.T) {
	tests := []struct {
		name           string
		enableBatching bool
		operations     int
		expectedLSN    uint64
	}{
		{
			name:           "basic WAL - initial state",
			enableBatching: false,
			operations:     0,
			expectedLSN:    0,
		},
		{
			name:           "basic WAL - after one operation",
			enableBatching: false,
			operations:     1,
			expectedLSN:    1,
		},
		{
			name:           "basic WAL - after multiple operations",
			enableBatching: false,
			operations:     5,
			expectedLSN:    5,
		},
		{
			name:           "batched WAL - after operations",
			enableBatching: true,
			operations:     3,
			expectedLSN:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()

			config := StorageConfig{
				DataDir:        dataDir,
				EnableBatching: tt.enableBatching,
				BatchSize:      10,
				FlushInterval:  100 * time.Millisecond,
			}

			gs, err := NewGraphStorageWithConfig(config)
			if err != nil {
				t.Fatalf("Failed to create storage: %v", err)
			}
			defer gs.Close()

			// Perform operations
			for i := 0; i < tt.operations; i++ {
				_, err := gs.CreateNode([]string{"Test"}, map[string]Value{
					"index": IntValue(int64(i)),
				})
				if err != nil {
					t.Fatalf("Failed to create node: %v", err)
				}
			}

			// If batching is enabled, flush to ensure WAL entries are written
			if tt.enableBatching {
				time.Sleep(150 * time.Millisecond) // Wait for flush
			}

			// Get current LSN
			lsn := gs.GetCurrentLSN()

			if lsn != tt.expectedLSN {
				t.Errorf("GetCurrentLSN() = %d, want %d", lsn, tt.expectedLSN)
			}
		})
	}
}

// TestQueryStatistics_TotalQueries verifies that TotalQueries is incremented correctly
func TestQueryStatistics_TotalQueries(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test data
	node, err := gs.CreateNode([]string{"Test"}, map[string]Value{"name": StringValue("test")})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Initial state - should be 0
	stats := gs.GetStatistics()
	if stats.TotalQueries != 0 {
		t.Errorf("Initial TotalQueries = %d, want 0", stats.TotalQueries)
	}

	// Perform queries
	_, _ = gs.GetNode(node.ID)
	stats = gs.GetStatistics()
	if stats.TotalQueries != 1 {
		t.Errorf("After 1 query, TotalQueries = %d, want 1", stats.TotalQueries)
	}

	_, _ = gs.GetOutgoingEdges(node.ID)
	stats = gs.GetStatistics()
	if stats.TotalQueries != 2 {
		t.Errorf("After 2 queries, TotalQueries = %d, want 2", stats.TotalQueries)
	}

	_, _ = gs.GetIncomingEdges(node.ID)
	stats = gs.GetStatistics()
	if stats.TotalQueries != 3 {
		t.Errorf("After 3 queries, TotalQueries = %d, want 3", stats.TotalQueries)
	}
}

// TestQueryStatistics_AvgQueryTime verifies average query time calculation
func TestQueryStatistics_AvgQueryTime(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test data
	node, err := gs.CreateNode([]string{"Test"}, map[string]Value{"name": StringValue("test")})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Initial state - should be 0
	stats := gs.GetStatistics()
	if stats.AvgQueryTime != 0 {
		t.Errorf("Initial AvgQueryTime = %f, want 0", stats.AvgQueryTime)
	}

	// Perform queries
	for i := 0; i < 10; i++ {
		_, _ = gs.GetNode(node.ID)
	}

	stats = gs.GetStatistics()
	// AvgQueryTime should be > 0 after queries
	if stats.AvgQueryTime <= 0 {
		t.Errorf("AvgQueryTime = %f, want > 0", stats.AvgQueryTime)
	}

	// Should be reasonable (sub-millisecond for in-memory ops)
	if stats.AvgQueryTime > 10.0 {
		t.Errorf("AvgQueryTime = %f ms, seems too high for simple operations", stats.AvgQueryTime)
	}

	if stats.TotalQueries != 10 {
		t.Errorf("TotalQueries = %d, want 10", stats.TotalQueries)
	}
}

// TestQueryStatistics_ConcurrentQueries verifies thread-safe query tracking
func TestQueryStatistics_ConcurrentQueries(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test data
	node, err := gs.CreateNode([]string{"Test"}, map[string]Value{"name": StringValue("test")})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Run concurrent queries
	const numGoroutines = 100
	const queriesPerGoroutine = 10
	const expectedTotal = numGoroutines * queriesPerGoroutine

	done := make(chan bool, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			for j := 0; j < queriesPerGoroutine; j++ {
				_, _ = gs.GetNode(node.ID)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	stats := gs.GetStatistics()
	if stats.TotalQueries != expectedTotal {
		t.Errorf("Concurrent TotalQueries = %d, want %d", stats.TotalQueries, expectedTotal)
	}

	// AvgQueryTime should be reasonable
	if stats.AvgQueryTime <= 0 {
		t.Errorf("AvgQueryTime = %f, want > 0", stats.AvgQueryTime)
	}
}

// TestQueryStatistics_AllOperations verifies all tracked operations increment stats
func TestQueryStatistics_AllOperations(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test data
	node1, err := gs.CreateNode([]string{"Test"}, map[string]Value{"name": StringValue("node1")})
	if err != nil {
		t.Fatalf("Failed to create node1: %v", err)
	}

	node2, err := gs.CreateNode([]string{"Test"}, map[string]Value{"name": StringValue("node2")})
	if err != nil {
		t.Fatalf("Failed to create node2: %v", err)
	}

	edge, err := gs.CreateEdge(node1.ID, node2.ID, "LINKS_TO", nil, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	// Reset stats to 0 for clean test
	stats := gs.GetStatistics()
	initialQueries := stats.TotalQueries

	// Test each operation
	operations := []struct {
		name string
		fn   func()
	}{
		{"GetNode", func() { gs.GetNode(node1.ID) }},
		{"GetEdge", func() { gs.GetEdge(edge.ID) }},
		{"GetOutgoingEdges", func() { gs.GetOutgoingEdges(node1.ID) }},
		{"GetIncomingEdges", func() { gs.GetIncomingEdges(node2.ID) }},
	}

	for i, op := range operations {
		op.fn()
		stats = gs.GetStatistics()
		expected := initialQueries + uint64(i+1)
		if stats.TotalQueries != expected {
			t.Errorf("%s: TotalQueries = %d, want %d", op.name, stats.TotalQueries, expected)
		}
	}
}

// BenchmarkGetNode_Sequential measures single-threaded GetNode performance with sharded locks
func BenchmarkGetNode_Sequential(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test nodes
	const numNodes = 1000
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetNode(nodeIDs[i%numNodes])
	}
}

// BenchmarkGetNode_Concurrent measures concurrent GetNode performance with sharded locks
func BenchmarkGetNode_Concurrent(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test nodes spread across different shards
	const numNodes = 1000
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			gs.GetNode(nodeIDs[i%numNodes])
			i++
		}
	})
}

// BenchmarkGetNode_ConcurrentSameShard measures contention when accessing same shard
func BenchmarkGetNode_ConcurrentSameShard(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes that hash to the same shard (shard 0)
	// Node IDs that are multiples of 256 will hash to shard 0
	const numNodes = 10
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		// Force specific node IDs in same shard by creating nodes first
		node, _ := gs.CreateNode([]string{"Test"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	// Verify they're in the same shard
	shard0 := gs.getShardIndex(nodeIDs[0])
	for _, id := range nodeIDs[1:] {
		if gs.getShardIndex(id) != shard0 {
			b.Skipf("Nodes not in same shard, skipping contention test")
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			gs.GetNode(nodeIDs[i%numNodes])
			i++
		}
	})
}

// BenchmarkGetNode_ConcurrentDifferentShards measures performance across different shards
func BenchmarkGetNode_ConcurrentDifferentShards(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes in many different shards
	const numNodes = 256 // One per shard
	nodeIDs := make([]uint64, numNodes)
	shardCounts := make(map[int]int)

	for i := 0; i < numNodes*10; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		shard := gs.getShardIndex(node.ID)
		if shardCounts[shard] == 0 {
			nodeIDs[len(shardCounts)] = node.ID
			shardCounts[shard]++
			if len(shardCounts) == numNodes {
				break
			}
		}
	}

	if len(shardCounts) < 256 {
		b.Logf("Only got %d different shards out of 256", len(shardCounts))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			gs.GetNode(nodeIDs[i%numNodes])
			i++
		}
	})
}

// BenchmarkGetOutgoingEdges_Concurrent measures concurrent edge retrieval
func BenchmarkGetOutgoingEdges_Concurrent(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes with edges
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Test"}, map[string]Value{})
		nodeIDs[i] = node.ID

		// Create 10 outgoing edges per node
		for j := 0; j < 10; j++ {
			target, _ := gs.CreateNode([]string{"Target"}, map[string]Value{})
			gs.CreateEdge(node.ID, target.ID, "LINKS_TO", nil, 1.0)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			gs.GetOutgoingEdges(nodeIDs[i%numNodes])
			i++
		}
	})
}

// BenchmarkMixedOperations_Concurrent benchmarks realistic mixed read workload
func BenchmarkMixedOperations_Concurrent(b *testing.B) {
	dataDir := b.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		b.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create realistic graph: 500 nodes, avg degree 5
	const numNodes = 500
	nodeIDs := make([]uint64, numNodes)
	edgeIDs := make([]uint64, 0, numNodes*5)

	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"User"}, map[string]Value{
			"id": StringValue(fmt.Sprintf("user%d", i)),
		})
		nodeIDs[i] = node.ID
	}

	// Create edges
	for i := 0; i < numNodes; i++ {
		for j := 0; j < 5; j++ {
			target := nodeIDs[(i+j+1)%numNodes]
			edge, _ := gs.CreateEdge(nodeIDs[i], target, "FOLLOWS", nil, 1.0)
			edgeIDs = append(edgeIDs, edge.ID)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Mix of operations: 40% GetNode, 30% GetOutgoing, 20% GetIncoming, 10% GetEdge
			switch i % 10 {
			case 0, 1, 2, 3:
				gs.GetNode(nodeIDs[i%numNodes])
			case 4, 5, 6:
				gs.GetOutgoingEdges(nodeIDs[i%numNodes])
			case 7, 8:
				gs.GetIncomingEdges(nodeIDs[i%numNodes])
			case 9:
				gs.GetEdge(edgeIDs[i%len(edgeIDs)])
			}
			i++
		}
	})
}
