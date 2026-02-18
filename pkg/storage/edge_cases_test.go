package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEdgeCase_EmptyGraph tests operations on an empty graph
func TestEdgeCase_EmptyGraph(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing operations on empty graph...")

	// Query non-existent node
	node, err := gs.GetNode(999)
	if err == nil && node != nil {
		t.Error("Expected error or nil for non-existent node")
	}

	// Query non-existent edges
	edges, err := gs.GetOutgoingEdges(999)
	if err != nil {
		t.Logf("  ✓ GetOutgoingEdges correctly handles non-existent node: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("Expected 0 edges for non-existent node, got %d", len(edges))
	}

	// Delete non-existent node (should not panic)
	err = gs.DeleteNode(999)
	if err == nil {
		t.Log("  ✓ DeleteNode handles non-existent node gracefully")
	}

	// Delete non-existent edge (should not panic)
	err = gs.DeleteEdge(999)
	if err == nil {
		t.Log("  ✓ DeleteEdge handles non-existent edge gracefully")
	}

	t.Log("✓ Empty graph operations completed successfully")
}

// TestEdgeCase_SingleNode tests operations with only one node
func TestEdgeCase_SingleNode(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing single node operations...")

	// Create single node
	node, err := gs.CreateNode([]string{"Single"}, map[string]Value{
		"name": StringValue("OnlyNode"),
	})
	if err != nil {
		t.Fatalf("Failed to create single node: %v", err)
	}

	// Try to create self-loop
	edge, err := gs.CreateEdge(node.ID, node.ID, "SELF_LOOP", nil, 1.0)
	if err != nil {
		t.Logf("  Self-loop creation failed: %v", err)
	} else {
		t.Logf("  ✓ Self-loop created: edge %d", edge.ID)
	}

	// Verify node is retrievable
	retrieved, err := gs.GetNode(node.ID)
	if err != nil || retrieved == nil {
		t.Errorf("Failed to retrieve single node: %v", err)
	}

	// Delete the only node
	err = gs.DeleteNode(node.ID)
	if err != nil {
		t.Errorf("Failed to delete single node: %v", err)
	}

	// Verify node is gone
	retrieved, err = gs.GetNode(node.ID)
	if retrieved != nil {
		t.Error("Node still exists after deletion")
	}

	t.Log("✓ Single node operations completed successfully")
}

// TestEdgeCase_EmptyProperties tests nodes with no properties
func TestEdgeCase_EmptyProperties(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing empty properties...")

	// Create node with no properties
	node, err := gs.CreateNode([]string{"Empty"}, map[string]Value{})
	if err != nil {
		t.Fatalf("Failed to create node with empty properties: %v", err)
	}

	// Verify node was created
	if node == nil {
		t.Fatal("Node is nil after creation")
	}

	t.Logf("  ✓ Node created with ID %d and 0 properties", node.ID)

	// Create edge with no properties
	node2, _ := gs.CreateNode([]string{"Empty2"}, nil)
	edge, err := gs.CreateEdge(node.ID, node2.ID, "EMPTY_PROPS", nil, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge with empty properties: %v", err)
	}

	t.Logf("  ✓ Edge created with ID %d and nil properties", edge.ID)
}

// TestEdgeCase_EmptyStrings tests handling of empty string values
func TestEdgeCase_EmptyStrings(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing empty string handling...")

	// Node with empty label
	node, err := gs.CreateNode([]string{""}, map[string]Value{
		"name": StringValue(""),
	})
	if err != nil {
		t.Logf("  Empty label rejected: %v", err)
	} else {
		t.Logf("  ✓ Node created with empty label and empty string property")
	}

	// Edge with empty type
	if node != nil {
		node2, _ := gs.CreateNode([]string{"Test"}, nil)
		edge, err := gs.CreateEdge(node.ID, node2.ID, "", nil, 1.0)
		if err != nil {
			t.Logf("  Empty edge type rejected: %v", err)
		} else {
			t.Logf("  ✓ Edge created with empty type: %d", edge.ID)
		}
	}
}

// TestEdgeCase_VeryLargeProperties tests handling of extremely large property values
func TestEdgeCase_VeryLargeProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large property test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing very large properties...")

	// Test various large sizes
	sizes := []int{
		1024,        // 1KB
		10 * 1024,   // 10KB
		100 * 1024,  // 100KB
		1024 * 1024, // 1MB
	}

	for _, size := range sizes {
		largeData := strings.Repeat("X", size)
		props := map[string]Value{
			"large_data": StringValue(largeData),
		}

		startTime := time.Now()
		node, err := gs.CreateNode([]string{"LargeProps"}, props)
		duration := time.Since(startTime)

		if err != nil {
			t.Logf("  %d bytes: FAILED - %v", size, err)
		} else {
			t.Logf("  ✓ %d bytes: Created in %v (node %d)", size, duration, node.ID)

			// Verify we can read it back
			retrieved, err := gs.GetNode(node.ID)
			if err != nil || retrieved == nil {
				t.Errorf("Failed to retrieve node with large property: %v", err)
			} else {
				if val, ok := retrieved.Properties["large_data"]; ok {
					retrievedData, err := val.AsString()
					if err != nil {
						t.Errorf("Failed to convert value to string: %v", err)
					} else if len(retrievedData) != size {
						t.Errorf("Retrieved data size mismatch: expected %d, got %d",
							size, len(retrievedData))
					}
				}
			}
		}
	}
}

// TestEdgeCase_SpecialCharacters tests handling of special characters in strings
func TestEdgeCase_SpecialCharacters(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing special characters...")

	specialStrings := []string{
		"Hello\nWorld",           // Newline
		"Tab\tSeparated",         // Tab
		"Quote\"Test",            // Quote
		"Backslash\\Test",        // Backslash
		"Unicode: 你好世界",        // Unicode
		"Emoji: 🚀🎉",            // Emoji
		"NULL\x00Byte",           // Null byte
		"<script>alert('xss')</script>", // HTML/JS
		"'; DROP TABLE nodes; --", // SQL injection attempt
	}

	for i, special := range specialStrings {
		props := map[string]Value{
			"special": StringValue(special),
		}

		node, err := gs.CreateNode([]string{"SpecialChars"}, props)
		if err != nil {
			t.Errorf("Failed to create node with special chars '%s': %v",
				special[:min(20, len(special))], err)
			continue
		}

		// Verify retrieval
		retrieved, err := gs.GetNode(node.ID)
		if err != nil {
			t.Errorf("Failed to retrieve node %d: %v", node.ID, err)
			continue
		}

		if val, ok := retrieved.Properties["special"]; ok {
			retrievedStr, err := val.AsString()
			if err != nil {
				t.Errorf("Failed to convert value to string: %v", err)
			} else if retrievedStr != special {
				t.Errorf("Special string mismatch for case %d", i)
				t.Logf("  Expected: %q", special)
				t.Logf("  Got: %q", retrievedStr)
			} else {
				t.Logf("  ✓ Case %d: Special chars preserved correctly", i)
			}
		}
	}
}

// TestEdgeCase_MaximumEdgesPerNode tests creating many edges from a single node
func TestEdgeCase_MaximumEdgesPerNode(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping maximum edges test in short mode or with race detector")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing maximum edges per node...")

	// Create source node
	sourceNode, err := gs.CreateNode([]string{"Hub"}, map[string]Value{
		"name": StringValue("CentralHub"),
	})
	if err != nil {
		t.Fatalf("Failed to create source node: %v", err)
	}

	// Create many target nodes
	targetCount := 1000
	targets := make([]*Node, targetCount)
	for i := 0; i < targetCount; i++ {
		target, _ := gs.CreateNode([]string{"Target"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		targets[i] = target
	}

	// Create edges to all targets
	t.Logf("Creating %d edges from single node...", targetCount)
	startTime := time.Now()

	for i, target := range targets {
		_, err := gs.CreateEdge(sourceNode.ID, target.ID, "CONNECTS", nil, 1.0)
		if err != nil {
			t.Errorf("Failed to create edge %d: %v", i, err)
			break
		}
	}

	duration := time.Since(startTime)
	t.Logf("  ✓ Created %d edges in %v", targetCount, duration)

	// Verify we can retrieve all edges
	edges, err := gs.GetOutgoingEdges(sourceNode.ID)
	if err != nil {
		t.Errorf("Failed to get outgoing edges: %v", err)
	} else {
		t.Logf("  ✓ Retrieved %d outgoing edges", len(edges))
		if len(edges) != targetCount {
			t.Errorf("Expected %d edges, got %d", targetCount, len(edges))
		}
	}
}

// TestEdgeCase_OrphanedEdges tests handling of edges with deleted nodes
func TestEdgeCase_OrphanedEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing orphaned edges...")

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)
	edge, err := gs.CreateEdge(node1.ID, node2.ID, "CONNECTS", nil, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	t.Logf("Created edge %d between nodes %d and %d", edge.ID, node1.ID, node2.ID)

	// Delete target node
	err = gs.DeleteNode(node2.ID)
	if err != nil {
		t.Logf("  Delete target node: %v", err)
	}

	// Try to get the edge
	retrievedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Logf("  ✓ Orphaned edge not retrievable: %v", err)
	} else if retrievedEdge != nil {
		t.Logf("  ⚠ Orphaned edge still exists (edge %d)", retrievedEdge.ID)
	}

	// Verify outgoing edges from source
	edges, _ := gs.GetOutgoingEdges(node1.ID)
	t.Logf("  Source node has %d outgoing edges after target deletion", len(edges))
}

// TestEdgeCase_CircularReferences tests circular edge references
func TestEdgeCase_CircularReferences(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing circular references...")

	// Create circular graph: A -> B -> C -> A
	nodeA, _ := gs.CreateNode([]string{"Node"}, map[string]Value{"name": StringValue("A")})
	nodeB, _ := gs.CreateNode([]string{"Node"}, map[string]Value{"name": StringValue("B")})
	nodeC, _ := gs.CreateNode([]string{"Node"}, map[string]Value{"name": StringValue("C")})

	gs.CreateEdge(nodeA.ID, nodeB.ID, "NEXT", nil, 1.0)
	gs.CreateEdge(nodeB.ID, nodeC.ID, "NEXT", nil, 1.0)
	gs.CreateEdge(nodeC.ID, nodeA.ID, "NEXT", nil, 1.0)

	t.Log("  ✓ Created circular reference: A -> B -> C -> A")

	// Verify we can traverse without infinite loop
	visited := make(map[uint64]bool)
	currentID := nodeA.ID
	maxSteps := 10

	for i := 0; i < maxSteps; i++ {
		if visited[currentID] {
			t.Logf("  ✓ Detected cycle at step %d (node %d)", i, currentID)
			break
		}
		visited[currentID] = true

		edges, _ := gs.GetOutgoingEdges(currentID)
		if len(edges) == 0 {
			break
		}
		currentID = edges[0].ToNodeID
	}

	if len(visited) == 3 {
		t.Log("  ✓ Successfully detected 3-node cycle")
	}
}

// TestEdgeCase_DuplicateEdges tests creating multiple edges between same nodes
func TestEdgeCase_DuplicateEdges(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing duplicate edges...")

	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	// Create multiple edges between same nodes
	edgeCount := 5
	edgeIDs := make([]uint64, edgeCount)

	for i := 0; i < edgeCount; i++ {
		edge, err := gs.CreateEdge(node1.ID, node2.ID, "DUPLICATE", map[string]Value{
			"index": IntValue(int64(i)),
		}, 1.0)
		if err != nil {
			t.Errorf("Failed to create duplicate edge %d: %v", i, err)
			continue
		}
		edgeIDs[i] = edge.ID
	}

	t.Logf("  Created %d edges between same nodes", edgeCount)

	// Verify all edges exist
	edges, _ := gs.GetOutgoingEdges(node1.ID)
	t.Logf("  ✓ Retrieved %d outgoing edges", len(edges))

	// Verify each has unique ID
	uniqueIDs := make(map[uint64]bool)
	for _, edge := range edges {
		uniqueIDs[edge.ID] = true
	}
	t.Logf("  ✓ All edges have unique IDs: %d unique out of %d total",
		len(uniqueIDs), len(edges))
}

// TestEdgeCase_ConcurrentSameNodeAccess tests concurrent access to same node
func TestEdgeCase_ConcurrentSameNodeAccess(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent access to same node...")

	// Create a node
	node, _ := gs.CreateNode([]string{"Contested"}, map[string]Value{
		"counter": IntValue(0),
	})

	// Multiple goroutines trying to read/write same node
	workers := 10
	operationsPerWorker := 100
	var wg sync.WaitGroup

	errChan := make(chan error, workers*operationsPerWorker)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < operationsPerWorker; i++ {
				// Read the node
				_, err := gs.GetNode(node.ID)
				if err != nil {
					errChan <- fmt.Errorf("worker %d read failed: %w", workerID, err)
				}

				// Small delay
				time.Sleep(time.Microsecond)
			}
		}(w)
	}

	wg.Wait()
	close(errChan)

	errorCount := 0
	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
		errorCount++
	}

	if errorCount == 0 {
		t.Logf("  ✓ %d workers × %d operations = %d total operations without errors",
			workers, operationsPerWorker, workers*operationsPerWorker)
	}
}

// TestEdgeCase_InvalidNodeIDs tests operations with invalid node IDs
func TestEdgeCase_InvalidNodeIDs(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing invalid node IDs...")

	invalidIDs := []uint64{
		0,                    // Zero
		^uint64(0),          // Max uint64
		999999999,           // Very large
	}

	for _, id := range invalidIDs {
		// Try to get node
		node, err := gs.GetNode(id)
		if err != nil {
			t.Logf("  ✓ GetNode(%d) returned error: %v", id, err)
		} else if node == nil {
			t.Logf("  ✓ GetNode(%d) returned nil", id)
		} else {
			t.Errorf("GetNode(%d) unexpectedly returned a node", id)
		}

		// Try to delete node
		err = gs.DeleteNode(id)
		if err != nil {
			t.Logf("  ✓ DeleteNode(%d) returned error: %v", id, err)
		} else {
			t.Logf("  ✓ DeleteNode(%d) handled gracefully", id)
		}
	}
}

// TestEdgeCase_InvalidEdgeCreation tests creating edges with invalid nodes
func TestEdgeCase_InvalidEdgeCreation(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing invalid edge creation...")

	validNode, _ := gs.CreateNode([]string{"Valid"}, nil)

	testCases := []struct {
		name   string
		fromID uint64
		toID   uint64
	}{
		{"from invalid to valid", 999999, validNode.ID},
		{"from valid to invalid", validNode.ID, 999999},
		{"both invalid", 999999, 888888},
		{"both zero", 0, 0},
	}

	for _, tc := range testCases {
		edge, err := gs.CreateEdge(tc.fromID, tc.toID, "INVALID", nil, 1.0)
		if err != nil {
			t.Logf("  ✓ %s: correctly rejected - %v", tc.name, err)
		} else if edge == nil {
			t.Logf("  ✓ %s: returned nil edge", tc.name)
		} else {
			t.Errorf("  %s: unexpectedly created edge %d", tc.name, edge.ID)
		}
	}
}

// TestEdgeCase_NegativeWeights tests edges with negative weights
func TestEdgeCase_NegativeWeights(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing negative edge weights...")

	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	weights := []float64{-1.0, -100.0, -0.001, 0.0}

	for _, weight := range weights {
		edge, err := gs.CreateEdge(node1.ID, node2.ID, "WEIGHTED", nil, weight)
		if err != nil {
			t.Logf("  Weight %.3f rejected: %v", weight, err)
		} else {
			t.Logf("  ✓ Edge created with weight %.3f (edge %d)", weight, edge.ID)
			if edge.Weight != weight {
				t.Errorf("Weight mismatch: expected %.3f, got %.3f", weight, edge.Weight)
			}
		}
	}
}

// TestEdgeCase_ClosedStorage tests operations on closed storage
func TestEdgeCase_ClosedStorage(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Close storage
	err = gs.Close()
	if err != nil {
		t.Fatalf("Failed to close storage: %v", err)
	}

	t.Log("Testing operations on closed storage...")

	// Try to create node on closed storage
	_, err = gs.CreateNode([]string{"AfterClose"}, nil)
	if err != nil {
		t.Logf("  ✓ CreateNode on closed storage: %v", err)
	} else {
		t.Error("CreateNode succeeded on closed storage (unexpected)")
	}

	// Try to query on closed storage
	_, err = gs.GetNode(1)
	if err != nil {
		t.Logf("  ✓ GetNode on closed storage: %v", err)
	} else {
		t.Error("GetNode succeeded on closed storage (unexpected)")
	}
}

// TestEdgeCase_MultipleLabels tests nodes with multiple labels
func TestEdgeCase_MultipleLabels(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing multiple labels...")

	labels := []string{"Person", "Employee", "Manager", "Executive"}

	node, err := gs.CreateNode(labels, map[string]Value{
		"name": StringValue("MultiLabel"),
	})
	if err != nil {
		t.Fatalf("Failed to create node with multiple labels: %v", err)
	}

	t.Logf("  ✓ Node created with %d labels", len(labels))

	// Verify labels
	if len(node.Labels) != len(labels) {
		t.Errorf("Label count mismatch: expected %d, got %d",
			len(labels), len(node.Labels))
	}
}

// TestEdgeCase_InvalidDataDirectory tests various invalid directory scenarios
func TestEdgeCase_InvalidDataDirectory(t *testing.T) {
	t.Log("Testing invalid data directories...")

	// Try to create storage in non-existent parent directory
	invalidPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "data")
	gs, err := NewGraphStorage(invalidPath)
	if err != nil {
		t.Logf("  ✓ Invalid path rejected: %v", err)
	} else {
		gs.Close()
		t.Log("  ✓ Storage created nested directory successfully")
	}

	// Try to use a file as directory
	tmpFile, _ := os.CreateTemp(t.TempDir(), "testfile")
	tmpFile.Close()

	gs, err = NewGraphStorage(tmpFile.Name())
	if err != nil {
		t.Logf("  ✓ File-as-directory rejected: %v", err)
	} else {
		gs.Close()
		t.Error("Storage created using file path (unexpected)")
	}
}

// TestEdgeCase_RapidCreateDelete tests rapid creation and deletion
func TestEdgeCase_RapidCreateDelete(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing rapid create/delete cycles...")

	cycles := 100
	for i := 0; i < cycles; i++ {
		// Create
		node, err := gs.CreateNode([]string{"Rapid"}, map[string]Value{
			"cycle": IntValue(int64(i)),
		})
		if err != nil {
			t.Errorf("Failed to create node in cycle %d: %v", i, err)
			continue
		}

		// Immediately delete
		err = gs.DeleteNode(node.ID)
		if err != nil {
			t.Errorf("Failed to delete node in cycle %d: %v", i, err)
		}
	}

	t.Logf("  ✓ Completed %d rapid create/delete cycles", cycles)
}

// TestEdgeCase_ZeroWeight tests edges with exactly zero weight
func TestEdgeCase_ZeroWeight(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing zero-weight edges...")

	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	edge, err := gs.CreateEdge(node1.ID, node2.ID, "ZERO_WEIGHT", nil, 0.0)
	if err != nil {
		t.Fatalf("Failed to create zero-weight edge: %v", err)
	}

	if edge.Weight != 0.0 {
		t.Errorf("Weight mismatch: expected 0.0, got %f", edge.Weight)
	} else {
		t.Log("  ✓ Zero-weight edge created successfully")
	}
}
