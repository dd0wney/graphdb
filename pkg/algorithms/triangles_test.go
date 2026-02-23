package algorithms

import (
	"math"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupTriangleTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "triangle-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}
	t.Cleanup(func() { gs.Close() })
	return gs
}

func TestCountTriangles_EmptyGraph(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 0 {
		t.Errorf("Expected 0 global triangles, got %d", result.GlobalCount)
	}
	if len(result.PerNode) != 0 {
		t.Errorf("Expected empty PerNode, got %d entries", len(result.PerNode))
	}
}

func TestCountTriangles_SingleTriangle(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// A -> B -> C -> A (directed cycle forms one undirected triangle)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 1 {
		t.Errorf("Expected 1 global triangle, got %d", result.GlobalCount)
	}

	// Each node participates in 1 triangle
	for _, node := range []*storage.Node{a, b, c} {
		if result.PerNode[node.ID] != 1 {
			t.Errorf("Node %d: expected 1 triangle, got %d", node.ID, result.PerNode[node.ID])
		}
	}

	// Each node has degree 2 (undirected), 1 triangle, coefficient = 1/(2*1/2) = 1.0
	for _, node := range []*storage.Node{a, b, c} {
		cc := result.ClusteringCoefficients[node.ID]
		if math.Abs(cc-1.0) > 0.001 {
			t.Errorf("Node %d: expected clustering coefficient ~1.0, got %f", node.ID, cc)
		}
	}
}

func TestCountTriangles_TwoTrianglesSharedEdge(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// Diamond: A-B-C triangle + A-B-D triangle (shared edge A-B)
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	// Triangle 1: A-B-C
	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	// Triangle 2: A-B-D
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, a.ID, "LINKS", nil, 1.0)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 2 {
		t.Errorf("Expected 2 global triangles, got %d", result.GlobalCount)
	}

	// A and B each participate in 2 triangles; C and D in 1 each
	if result.PerNode[a.ID] != 2 {
		t.Errorf("Node A: expected 2 triangles, got %d", result.PerNode[a.ID])
	}
	if result.PerNode[b.ID] != 2 {
		t.Errorf("Node B: expected 2 triangles, got %d", result.PerNode[b.ID])
	}
	if result.PerNode[c.ID] != 1 {
		t.Errorf("Node C: expected 1 triangle, got %d", result.PerNode[c.ID])
	}
	if result.PerNode[d.ID] != 1 {
		t.Errorf("Node D: expected 1 triangle, got %d", result.PerNode[d.ID])
	}
}

func TestCountTriangles_StarNoTriangles(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// Star: hub -> spoke1, hub -> spoke2, hub -> spoke3 (no triangles)
	hub, _ := gs.CreateNode([]string{"Hub"}, nil)
	s1, _ := gs.CreateNode([]string{"Spoke"}, nil)
	s2, _ := gs.CreateNode([]string{"Spoke"}, nil)
	s3, _ := gs.CreateNode([]string{"Spoke"}, nil)

	gs.CreateEdge(hub.ID, s1.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(hub.ID, s2.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(hub.ID, s3.ID, "LINKS", nil, 1.0)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 0 {
		t.Errorf("Expected 0 triangles, got %d", result.GlobalCount)
	}

	// Hub has 3 neighbors, 0 triangles → coefficient 0.0
	if result.ClusteringCoefficients[hub.ID] != 0.0 {
		t.Errorf("Hub: expected CC 0.0, got %f", result.ClusteringCoefficients[hub.ID])
	}
}

func TestCountTriangles_CompleteGraph4(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// K4: 4 nodes, all connected bidirectionally → 4 triangles
	nodes := make([]*storage.Node, 4)
	for i := range nodes {
		nodes[i], _ = gs.CreateNode([]string{"Node"}, nil)
	}
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			gs.CreateEdge(nodes[i].ID, nodes[j].ID, "LINKS", nil, 1.0)
			gs.CreateEdge(nodes[j].ID, nodes[i].ID, "LINKS", nil, 1.0)
		}
	}

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	// K4 has C(4,3) = 4 triangles
	if result.GlobalCount != 4 {
		t.Errorf("Expected 4 triangles in K4, got %d", result.GlobalCount)
	}

	// Each node participates in C(3,2) = 3 triangles
	for _, node := range nodes {
		if result.PerNode[node.ID] != 3 {
			t.Errorf("Node %d: expected 3 triangles, got %d", node.ID, result.PerNode[node.ID])
		}
	}

	// K4: each node has degree 3, 3 triangles, possible = 3*2/2 = 3, CC = 1.0
	for _, node := range nodes {
		cc := result.ClusteringCoefficients[node.ID]
		if math.Abs(cc-1.0) > 0.001 {
			t.Errorf("Node %d: expected CC ~1.0, got %f", node.ID, cc)
		}
	}
}

func TestCountTriangles_TopNodes(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// Create a graph where one node has more triangles than others
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)
	d, _ := gs.CreateNode([]string{"Node"}, nil)

	// Triangle A-B-C
	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(c.ID, a.ID, "LINKS", nil, 1.0)

	// Triangle A-B-D
	gs.CreateEdge(b.ID, d.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(d.ID, a.ID, "LINKS", nil, 1.0)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if len(result.TopNodes) == 0 {
		t.Fatal("Expected non-empty TopNodes")
	}

	// Top node should be A or B (both have 2 triangles)
	top := result.TopNodes[0]
	if top.Score != 2.0 {
		t.Errorf("Top node score: expected 2.0, got %f", top.Score)
	}
}

func TestCountTriangles_IsolatedNodes(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	gs.CreateNode([]string{"Node"}, nil)
	gs.CreateNode([]string{"Node"}, nil)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 0 {
		t.Errorf("Expected 0 triangles for isolated nodes, got %d", result.GlobalCount)
	}
}

func TestCountTriangles_DirectedOnlyTriangle(t *testing.T) {
	gs := setupTriangleTestGraph(t)

	// A -> B, B -> C, A -> C (all one-directional)
	// Treated as undirected: A-B, B-C, A-C forms a triangle
	a, _ := gs.CreateNode([]string{"Node"}, nil)
	b, _ := gs.CreateNode([]string{"Node"}, nil)
	c, _ := gs.CreateNode([]string{"Node"}, nil)

	gs.CreateEdge(a.ID, b.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(b.ID, c.ID, "LINKS", nil, 1.0)
	gs.CreateEdge(a.ID, c.ID, "LINKS", nil, 1.0)

	result, err := CountTriangles(gs)
	if err != nil {
		t.Fatalf("CountTriangles failed: %v", err)
	}

	if result.GlobalCount != 1 {
		t.Errorf("Expected 1 triangle (undirected view), got %d", result.GlobalCount)
	}
}
