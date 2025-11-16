package storage

import (
	"testing"
)

// BenchmarkGraphStorage_CreateEdge_InMemory benchmarks edge creation with in-memory storage
func BenchmarkGraphStorage_CreateEdge_InMemory(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: false,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create source and target nodes
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	}
}

// BenchmarkGraphStorage_CreateEdge_DiskBacked benchmarks edge creation with disk-backed storage
func BenchmarkGraphStorage_CreateEdge_DiskBacked(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create source and target nodes
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	}
}

// BenchmarkGraphStorage_GetOutgoingEdges_InMemory benchmarks reading edges from memory
func BenchmarkGraphStorage_GetOutgoingEdges_InMemory(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: false,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	for i := 0; i < 10; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetOutgoingEdges(node1.ID)
	}
}

// BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheHit benchmarks cache hits
func BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheHit(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	for i := 0; i < 10; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
	}

	// Prime the cache
	gs.GetOutgoingEdges(node1.ID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.GetOutgoingEdges(node1.ID)
	}
}

// BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheMiss benchmarks cache misses
func BenchmarkGraphStorage_GetOutgoingEdges_DiskBacked_CacheMiss(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      10, // Small cache to force misses
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create many nodes with edges to evict cache
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
		targetNode, _ := gs.CreateNode([]string{"Node"}, nil)
		for j := 0; j < 10; j++ {
			gs.CreateEdge(node.ID, targetNode.ID, "EDGE", nil, 1.0)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Access different nodes to force cache misses
		gs.GetOutgoingEdges(nodeIDs[i%numNodes])
	}
}

// BenchmarkGraphStorage_DeleteEdge_InMemory benchmarks edge deletion with in-memory storage
func BenchmarkGraphStorage_DeleteEdge_InMemory(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: false,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Pre-create edges for deletion
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	edgeIDs := make([]uint64, b.N)
	for i := 0; i < b.N; i++ {
		edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
		edgeIDs[i] = edge.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.DeleteEdge(edgeIDs[i])
	}
}

// BenchmarkGraphStorage_DeleteEdge_DiskBacked benchmarks edge deletion with disk-backed storage
func BenchmarkGraphStorage_DeleteEdge_DiskBacked(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Pre-create edges for deletion
	node1, _ := gs.CreateNode([]string{"Node"}, nil)
	node2, _ := gs.CreateNode([]string{"Node"}, nil)
	edgeIDs := make([]uint64, b.N)
	for i := 0; i < b.N; i++ {
		edge, _ := gs.CreateEdge(node1.ID, node2.ID, "EDGE", nil, 1.0)
		edgeIDs[i] = edge.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gs.DeleteEdge(edgeIDs[i])
	}
}

// BenchmarkGraphStorage_MixedWorkload_InMemory benchmarks realistic mixed workload
func BenchmarkGraphStorage_MixedWorkload_InMemory(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: false,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Setup: Create initial graph
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 70% reads, 20% writes, 10% deletes (realistic workload)
		op := i % 10
		if op < 7 {
			// Read operation
			gs.GetOutgoingEdges(nodeIDs[i%numNodes])
		} else if op < 9 {
			// Write operation
			src := nodeIDs[i%numNodes]
			dst := nodeIDs[(i+1)%numNodes]
			gs.CreateEdge(src, dst, "EDGE", nil, 1.0)
		} else {
			// Delete operation (if edges exist)
			outgoing, _ := gs.GetOutgoingEdges(nodeIDs[i%numNodes])
			if len(outgoing) > 0 {
				gs.DeleteEdge(outgoing[0].ID)
			}
		}
	}
}

// BenchmarkGraphStorage_MixedWorkload_DiskBacked benchmarks realistic mixed workload with disk-backed storage
func BenchmarkGraphStorage_MixedWorkload_DiskBacked(b *testing.B) {
	dataDir := b.TempDir()

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      1000,
	})
	if err != nil {
		b.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Setup: Create initial graph
	const numNodes = 100
	nodeIDs := make([]uint64, numNodes)
	for i := 0; i < numNodes; i++ {
		node, _ := gs.CreateNode([]string{"Node"}, nil)
		nodeIDs[i] = node.ID
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 70% reads, 20% writes, 10% deletes (realistic workload)
		op := i % 10
		if op < 7 {
			// Read operation
			gs.GetOutgoingEdges(nodeIDs[i%numNodes])
		} else if op < 9 {
			// Write operation
			src := nodeIDs[i%numNodes]
			dst := nodeIDs[(i+1)%numNodes]
			gs.CreateEdge(src, dst, "EDGE", nil, 1.0)
		} else {
			// Delete operation (if edges exist)
			outgoing, _ := gs.GetOutgoingEdges(nodeIDs[i%numNodes])
			if len(outgoing) > 0 {
				gs.DeleteEdge(outgoing[0].ID)
			}
		}
	}
}
