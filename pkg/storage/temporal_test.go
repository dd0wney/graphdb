package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Helper to setup a test graph with temporal edges
func setupTemporalTestGraph(t *testing.T) (*GraphStorage, func()) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_"+t.Name())
	gs, err := NewGraphStorage(tempDir)
	if err != nil {
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
		"name": StringValue("Charlie"),
	})

	// Create edges with temporal properties
	// Alice -> Bob: valid from 100 to 200
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]Value{
		"valid_from": IntValue(100),
		"valid_to":   IntValue(200),
	}, 1.0)

	// Alice -> Charlie: valid from 150 onwards (no end)
	gs.CreateEdge(node1.ID, node3.ID, "KNOWS", map[string]Value{
		"valid_from": IntValue(150),
	}, 1.0)

	// Bob -> Charlie: no temporal properties (always valid)
	gs.CreateEdge(node2.ID, node3.ID, "KNOWS", map[string]Value{}, 1.0)

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tempDir)
	}

	return gs, cleanup
}

// TestNewTemporalQuery tests temporal query creation
func TestNewTemporalQuery(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_new")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	tq := NewTemporalQuery(gs)

	if tq == nil {
		t.Fatal("Expected TemporalQuery to be created")
	}

	if tq.graph != gs {
		t.Error("TemporalQuery should reference the graph")
	}
}

// TestGetEdgesAtTime tests getting edges at a specific timestamp
func TestGetEdgesAtTime(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	tq := NewTemporalQuery(gs)

	tests := []struct {
		name           string
		nodeID         uint64
		timestamp      int64
		expectedCount  int
		expectedTarget []uint64
	}{
		{
			name:           "before any edges",
			nodeID:         1,
			timestamp:      50,
			expectedCount:  0,
			expectedTarget: []uint64{},
		},
		{
			name:           "at time 100",
			nodeID:         1,
			timestamp:      100,
			expectedCount:  1,
			expectedTarget: []uint64{2}, // Alice -> Bob
		},
		{
			name:           "at time 175",
			nodeID:         1,
			timestamp:      175,
			expectedCount:  2,
			expectedTarget: []uint64{2, 3}, // Alice -> Bob and Alice -> Charlie
		},
		{
			name:           "at time 250",
			nodeID:         1,
			timestamp:      250,
			expectedCount:  1,
			expectedTarget: []uint64{3}, // Only Alice -> Charlie (no end)
		},
		{
			name:           "always valid edge",
			nodeID:         2,
			timestamp:      1000,
			expectedCount:  1,
			expectedTarget: []uint64{3}, // Bob -> Charlie (no temporal)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges, err := tq.GetEdgesAtTime(tt.nodeID, tt.timestamp)
			if err != nil {
				t.Fatalf("GetEdgesAtTime failed: %v", err)
			}

			if len(edges) != tt.expectedCount {
				t.Errorf("Expected %d edges, got %d", tt.expectedCount, len(edges))
			}

			// Verify target nodes
			for _, expectedTo := range tt.expectedTarget {
				found := false
				for _, edge := range edges {
					if edge.ToNodeID == expectedTo {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected edge to node %d not found", expectedTo)
				}
			}
		})
	}
}

// TestGetEdgesAtTime_BoundaryConditions tests exact boundary times
func TestGetEdgesAtTime_BoundaryConditions(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	tq := NewTemporalQuery(gs)

	// Test at exact start time (100)
	edges, _ := tq.GetEdgesAtTime(1, 100)
	if len(edges) != 1 {
		t.Errorf("Expected edge to be valid at start time, got %d edges", len(edges))
	}

	// Test at exact end time (200)
	edges, _ = tq.GetEdgesAtTime(1, 200)
	found := false
	for _, edge := range edges {
		if edge.ToNodeID == 2 {
			found = true
		}
	}
	if !found {
		t.Error("Expected edge to be valid at end time")
	}

	// Test just after end time (201)
	edges, _ = tq.GetEdgesAtTime(1, 201)
	for _, edge := range edges {
		if edge.ToNodeID == 2 {
			t.Error("Edge should not be valid after end time")
		}
	}
}

// TestGetEdgesInTimeRange tests getting edges in a time range
func TestGetEdgesInTimeRange(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	tq := NewTemporalQuery(gs)

	tests := []struct {
		name          string
		nodeID        uint64
		start         int64
		end           int64
		expectedCount int
		description   string
	}{
		{
			name:          "range before any edges",
			nodeID:        1,
			start:         0,
			end:           50,
			expectedCount: 0,
			description:   "No edges should exist before time 100",
		},
		{
			name:          "range overlapping first edge",
			nodeID:        1,
			start:         50,
			end:           150,
			expectedCount: 2,
			description:   "Should get both edges (one starts at 100, one at 150)",
		},
		{
			name:          "range fully containing edge",
			nodeID:        1,
			start:         110,
			end:           190,
			expectedCount: 2,
			description:   "Both edges are valid in this range",
		},
		{
			name:          "range after first edge expires",
			nodeID:        1,
			start:         250,
			end:           300,
			expectedCount: 1,
			description:   "Only infinite edge (Alice->Charlie) is valid",
		},
		{
			name:          "range far in future",
			nodeID:        1,
			start:         1000,
			end:           2000,
			expectedCount: 1,
			description:   "Only infinite edge remains",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges, err := tq.GetEdgesInTimeRange(tt.nodeID, tt.start, tt.end)
			if err != nil {
				t.Fatalf("GetEdgesInTimeRange failed: %v", err)
			}

			if len(edges) != tt.expectedCount {
				t.Errorf("%s: Expected %d edges, got %d", tt.description, tt.expectedCount, len(edges))
			}
		})
	}
}

// TestGetEdgesInTimeRange_OverlapLogic tests edge cases of overlap detection
func TestGetEdgesInTimeRange_OverlapLogic(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_overlap")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	node1, _ := gs.CreateNode([]string{"Test"}, nil)
	node2, _ := gs.CreateNode([]string{"Test"}, nil)

	// Edge valid from 100 to 200
	gs.CreateEdge(node1.ID, node2.ID, "TEST", map[string]Value{
		"valid_from": IntValue(100),
		"valid_to":   IntValue(200),
	}, 1.0)

	tq := NewTemporalQuery(gs)

	tests := []struct {
		name          string
		start         int64
		end           int64
		shouldOverlap bool
	}{
		{"range before edge", 0, 50, false},
		{"range touches start", 50, 100, true},
		{"range inside edge", 110, 190, true},
		{"range touches end", 200, 250, true},
		{"range after edge", 201, 300, false},
		{"range contains edge", 50, 250, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges, _ := tq.GetEdgesInTimeRange(node1.ID, tt.start, tt.end)
			hasEdge := len(edges) > 0

			if hasEdge != tt.shouldOverlap {
				t.Errorf("Expected overlap=%v, got %v for range [%d, %d]",
					tt.shouldOverlap, hasEdge, tt.start, tt.end)
			}
		})
	}
}

// TestCreateTemporalEdge tests creating temporal edges
func TestCreateTemporalEdge(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_create")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)

	tq := NewTemporalQuery(gs)

	// Create temporal edge
	edge, err := tq.CreateTemporalEdge(
		node1.ID, node2.ID,
		"WORKS_WITH",
		map[string]Value{"role": StringValue("manager")},
		1.0,
		100, 200,
	)

	if err != nil {
		t.Fatalf("CreateTemporalEdge failed: %v", err)
	}

	// Verify temporal properties were added
	validFrom, hasFrom := edge.Properties["valid_from"]
	if !hasFrom {
		t.Error("Expected valid_from property")
	}

	from, _ := validFrom.AsInt()
	if from != 100 {
		t.Errorf("Expected valid_from=100, got %d", from)
	}

	validTo, hasTo := edge.Properties["valid_to"]
	if !hasTo {
		t.Error("Expected valid_to property")
	}

	to, _ := validTo.AsInt()
	if to != 200 {
		t.Errorf("Expected valid_to=200, got %d", to)
	}

	// Verify custom property was preserved
	role, hasRole := edge.Properties["role"]
	if !hasRole {
		t.Error("Expected custom property 'role'")
	}

	roleStr, _ := role.AsString()
	if roleStr != "manager" {
		t.Errorf("Expected role='manager', got %q", roleStr)
	}
}

// TestCreateTemporalEdge_NoEndTime tests creating edge with no end time
func TestCreateTemporalEdge_NoEndTime(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_noend")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	node1, _ := gs.CreateNode([]string{"Person"}, nil)
	node2, _ := gs.CreateNode([]string{"Person"}, nil)

	tq := NewTemporalQuery(gs)

	// Create temporal edge with no end time (validTo = 0)
	edge, err := tq.CreateTemporalEdge(
		node1.ID, node2.ID,
		"KNOWS",
		nil,
		1.0,
		100, 0, // No end time
	)

	if err != nil {
		t.Fatalf("CreateTemporalEdge failed: %v", err)
	}

	// Should have valid_from but not valid_to
	if _, hasFrom := edge.Properties["valid_from"]; !hasFrom {
		t.Error("Expected valid_from property")
	}

	if _, hasTo := edge.Properties["valid_to"]; hasTo {
		t.Error("Should not have valid_to property when validTo=0")
	}
}

// TestNewGraphSnapshot tests creating graph snapshots
func TestNewGraphSnapshot(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_snapshot")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	timestamp := time.Now().Unix()

	snapshot := NewGraphSnapshot(gs, timestamp)

	if snapshot == nil {
		t.Fatal("Expected GraphSnapshot to be created")
	}

	if snapshot.timestamp != timestamp {
		t.Errorf("Expected timestamp %d, got %d", timestamp, snapshot.timestamp)
	}

	if snapshot.graph != gs {
		t.Error("Snapshot should reference the graph")
	}
}

// TestGraphSnapshot_GetOutgoingEdges tests snapshot edge retrieval
func TestGraphSnapshot_GetOutgoingEdges(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	// Snapshot at time 175 (both edges from Alice are valid)
	snapshot := NewGraphSnapshot(gs, 175)
	edges, err := snapshot.GetOutgoingEdges(1)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(edges) != 2 {
		t.Errorf("Expected 2 edges at time 175, got %d", len(edges))
	}

	// Snapshot at time 250 (only infinite edge from Alice)
	snapshot = NewGraphSnapshot(gs, 250)
	edges, err = snapshot.GetOutgoingEdges(1)
	if err != nil {
		t.Fatalf("GetOutgoingEdges failed: %v", err)
	}

	if len(edges) != 1 {
		t.Errorf("Expected 1 edge at time 250, got %d", len(edges))
	}

	if edges[0].ToNodeID != 3 {
		t.Errorf("Expected edge to node 3, got edge to node %d", edges[0].ToNodeID)
	}
}

// TestComputeTemporalMetrics tests temporal metrics calculation
func TestComputeTemporalMetrics(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	metrics, err := ComputeTemporalMetrics(gs, 0, 1000)
	if err != nil {
		t.Fatalf("ComputeTemporalMetrics failed: %v", err)
	}

	if metrics == nil {
		t.Fatal("Expected metrics to be returned")
	}

	// Check that average lifetime was calculated
	if metrics.AverageEdgeLifetime < 0 {
		t.Error("Average lifetime should not be negative")
	}

	// Check edge creation rate
	if metrics.EdgeCreationRate < 0 {
		t.Error("Edge creation rate should not be negative")
	}
}

// TestComputeTemporalMetrics_EmptyGraph tests metrics on empty graph
func TestComputeTemporalMetrics_EmptyGraph(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_empty")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()

	metrics, err := ComputeTemporalMetrics(gs, 0, 1000)
	if err != nil {
		t.Fatalf("ComputeTemporalMetrics failed: %v", err)
	}

	if metrics.AverageEdgeLifetime != 0 {
		t.Errorf("Expected 0 average lifetime for empty graph, got %.2f", metrics.AverageEdgeLifetime)
	}

	if metrics.EdgeCreationRate != 0 {
		t.Errorf("Expected 0 creation rate for empty graph, got %.2f", metrics.EdgeCreationRate)
	}
}

// TestComputeTemporalMetrics_ZeroTimeRange tests with zero time range
func TestComputeTemporalMetrics_ZeroTimeRange(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	// This might cause division by zero
	metrics, err := ComputeTemporalMetrics(gs, 100, 100)
	if err != nil {
		t.Fatalf("ComputeTemporalMetrics failed: %v", err)
	}

	// Should handle zero time range gracefully
	if metrics == nil {
		t.Fatal("Expected metrics even with zero time range")
	}
}

// TestTemporalTraversal tests temporal BFS traversal
func TestTemporalTraversal(t *testing.T) {
	gs, cleanup := setupTemporalTestGraph(t)
	defer cleanup()

	tests := []struct {
		name          string
		startID       uint64
		timestamp     int64
		maxDepth      int
		expectedCount int
		description   string
	}{
		{
			name:          "from Alice at time 175",
			startID:       1,
			timestamp:     175,
			maxDepth:      2,
			expectedCount: 3,
			description:   "Should reach all 3 nodes",
		},
		{
			name:          "from Alice at time 250",
			startID:       1,
			timestamp:     250,
			maxDepth:      2,
			expectedCount: 2,
			description:   "Should reach Alice and Charlie (Bob edge expired)",
		},
		{
			name:          "depth 0",
			startID:       1,
			timestamp:     175,
			maxDepth:      0,
			expectedCount: 1,
			description:   "Should only include start node",
		},
		{
			name:          "depth 1",
			startID:       1,
			timestamp:     175,
			maxDepth:      1,
			expectedCount: 3,
			description:   "Should reach immediate neighbors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes, err := TemporalTraversal(gs, tt.startID, tt.timestamp, tt.maxDepth)
			if err != nil {
				t.Fatalf("TemporalTraversal failed: %v", err)
			}

			if len(nodes) != tt.expectedCount {
				t.Errorf("%s: Expected %d nodes, got %d", tt.description, tt.expectedCount, len(nodes))
			}
		})
	}
}

// TestTemporalTraversal_NoEdges tests traversal with no valid edges
func TestTemporalTraversal_NoEdges(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "temporal_test_trav")
	defer os.RemoveAll(tempDir)
	gs, _ := NewGraphStorage(tempDir)
	defer gs.Close()
	node, _ := gs.CreateNode([]string{"Isolated"}, nil)

	nodes, err := TemporalTraversal(gs, node.ID, 100, 5)
	if err != nil {
		t.Fatalf("TemporalTraversal failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("Expected 1 node (start node only), got %d", len(nodes))
	}
}

// TestTemporalEdge_Structure tests TemporalEdge structure
func TestTemporalEdge_Structure(t *testing.T) {
	edge := &Edge{
		ID:         1,
		FromNodeID: 10,
		ToNodeID:   20,
		Type:       "TEST",
		Properties: make(map[string]Value),
		Weight:     1.0,
		CreatedAt:  100,
	}

	te := &TemporalEdge{
		Edge:      edge,
		ValidFrom: 100,
		ValidTo:   200,
	}

	if te.Edge.ID != 1 {
		t.Error("TemporalEdge should contain Edge")
	}

	if te.ValidFrom != 100 {
		t.Errorf("Expected ValidFrom=100, got %d", te.ValidFrom)
	}

	if te.ValidTo != 200 {
		t.Errorf("Expected ValidTo=200, got %d", te.ValidTo)
	}
}
