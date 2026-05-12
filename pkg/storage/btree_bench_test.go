package storage

import (
	"os"
	"testing"
)

func BenchmarkStorage_GetNode_Memory(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-mem-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()
	
	tenantID := "default"
	n, _ := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gs.GetNodeForTenant(n.ID, tenantID)
	}
}

func BenchmarkStorage_GetNode_BTree(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-btree-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewBTreeGraphStorage(dataDir)
	defer gs.Close()
	
	tenantID := "default"
	n, _ := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gs.GetNodeForTenant(n.ID, tenantID)
	}
}

func BenchmarkStorage_CreateNode_Memory(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-create-mem-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()
	
	tenantID := "default"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	}
}

func BenchmarkStorage_CreateNode_BTree(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-create-btree-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewBTreeGraphStorage(dataDir)
	defer gs.Close()
	
	tenantID := "default"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
	}
}

func BenchmarkStorage_Traversal_Memory(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-trav-mem-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewGraphStorage(dataDir)
	defer gs.Close()
	
	runTraversalBench(b, gs)
}

func BenchmarkStorage_Traversal_BTree(b *testing.B) {
	dataDir, _ := os.MkdirTemp("", "bench-trav-btree-*")
	defer os.RemoveAll(dataDir)
	
	gs, _ := NewBTreeGraphStorage(dataDir)
	defer gs.Close()
	
	runTraversalBench(b, gs)
}

func runTraversalBench(b *testing.B, gs Storage) {
	tenantID := "default"
	nodes := make([]uint64, 100)
	for i := 0; i < 100; i++ {
		n, _ := gs.CreateNodeWithTenant(tenantID, []string{"User"}, nil)
		nodes[i] = n.ID
	}
	for i := 0; i < 99; i++ {
		_, _ = gs.CreateEdgeWithTenant(tenantID, nodes[i], nodes[i+1], "FOLLOWS", nil, 1.0)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		curr := nodes[0]
		for j := 0; j < 10; j++ {
			edges, _ := gs.GetOutgoingEdgesForTenant(curr, tenantID)
			if len(edges) == 0 {
				break
			}
			curr = edges[0].ToNodeID
		}
	}
}
