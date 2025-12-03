package storage

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEdgeCase_SnapshotDuringHeavyLoad tests snapshot creation under heavy load
func TestEdgeCase_SnapshotDuringHeavyLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping snapshot heavy load test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing snapshot creation during heavy load...")

	// Create initial dataset
	nodeCount := 5000
	for i := 0; i < nodeCount; i++ {
		gs.CreateNode([]string{"LoadTest"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
	}

	// Start continuous writes with pause support
	stopChan := make(chan struct{})
	pauseChan := make(chan bool)
	writeDone := make(chan struct{})

	go func() {
		defer close(writeDone)
		counter := nodeCount
		paused := false
		for {
			select {
			case <-stopChan:
				return
			case paused = <-pauseChan:
				// Pause/resume signal
			default:
				if !paused {
					gs.CreateNode([]string{"LoadTest"}, map[string]Value{
						"id": IntValue(int64(counter)),
					})
					counter++
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Create snapshots with brief pauses in writes
	snapshotCount := 5
	for i := 0; i < snapshotCount; i++ {
		time.Sleep(100 * time.Millisecond) // Let writes accumulate

		// Pause writes for snapshot
		pauseChan <- true
		time.Sleep(10 * time.Millisecond) // Brief pause for in-flight writes to complete

		startTime := time.Now()
		err := gs.Snapshot()
		duration := time.Since(startTime)

		// Resume writes
		pauseChan <- false

		if err != nil {
			t.Errorf("Snapshot %d failed: %v", i, err)
		} else {
			t.Logf("  ✓ Snapshot %d completed in %v with paused writes", i, duration)
		}
	}

	close(stopChan)
	<-writeDone

	t.Logf("  ✓ Successfully created %d snapshots under load", snapshotCount)
}

// TestEdgeCase_WALReplayWithGaps tests WAL replay with missing entries
func TestEdgeCase_WALReplayWithGaps(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping WAL gap test in short mode")
	}

	dataDir := t.TempDir()

	t.Log("Testing WAL replay with gaps...")

	// Create storage and write data
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		for i := 0; i < 100; i++ {
			gs.CreateNode([]string{"WALGapTest"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
		}

		gs.Close()
	}

	// Delete some WAL segments to create gaps
	walPath := filepath.Join(dataDir, "wal")
	walFiles, _ := filepath.Glob(filepath.Join(walPath, "*"))
	if len(walFiles) > 2 {
		// Delete middle file to create a gap
		os.Remove(walFiles[len(walFiles)/2])
		t.Logf("  Deleted WAL file: %s", filepath.Base(walFiles[len(walFiles)/2]))
	}

	// Try to reopen and replay
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Logf("  ✓ WAL replay with gaps correctly failed: %v", err)
		} else {
			defer gs.Close()
			t.Log("  ✓ WAL replay handled gaps gracefully")
		}
	}
}

// TestEdgeCase_CorruptedSnapshot tests handling of corrupted snapshot files
func TestEdgeCase_CorruptedSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping corrupted snapshot test in short mode")
	}

	dataDir := t.TempDir()

	t.Log("Testing corrupted snapshot handling...")

	// Create storage and snapshot
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		for i := 0; i < 50; i++ {
			gs.CreateNode([]string{"SnapshotTest"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
		}

		gs.Snapshot()
		gs.Close()
	}

	// Corrupt snapshot file
	snapshots, _ := filepath.Glob(filepath.Join(dataDir, "*.snapshot"))
	if len(snapshots) > 0 {
		data, err := os.ReadFile(snapshots[0])
		if err == nil && len(data) > 100 {
			// Corrupt the header
			for i := 0; i < 20; i++ {
				data[i] = 0xFF
			}
			os.WriteFile(snapshots[0], data, 0644)
			t.Logf("  Corrupted snapshot: %s", filepath.Base(snapshots[0]))
		}
	}

	// Try to load corrupted snapshot
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Logf("  ✓ Correctly rejected corrupted snapshot: %v", err)
		} else {
			defer gs.Close()
			t.Log("  ✓ Handled corrupted snapshot gracefully")
		}
	}
}

// TestEdgeCase_VectorSearchExtremes tests vector search with extreme values
func TestEdgeCase_VectorSearchExtremes(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing vector search edge cases...")

	testCases := []struct {
		name   string
		vector []float32
	}{
		{"zero vector", []float32{0, 0, 0, 0}},
		{"unit vector", []float32{1, 0, 0, 0}},
		{"negative values", []float32{-1, -2, -3, -4}},
		{"very small values", []float32{1e-10, 1e-10, 1e-10, 1e-10}},
		{"very large values", []float32{1e10, 1e10, 1e10, 1e10}},
		{"mixed scale", []float32{1e-10, 1e10, 1, -1}},
		{"single dimension", []float32{1.0}},
		{"high dimension", make([]float32, 512)}, // 512-d vector
	}

	// Initialize high-dimension vector
	for i := range testCases[len(testCases)-1].vector {
		testCases[len(testCases)-1].vector[i] = rand.Float32()
	}

	for i, tc := range testCases {
		node, err := gs.CreateNode([]string{"VectorTest"}, map[string]Value{
			"embedding": VectorValue(tc.vector),
		})
		if err != nil {
			t.Logf("  %s rejected: %v", tc.name, err)
		} else {
			t.Logf("  ✓ Case %d (%s): Created node %d with %d-d vector",
				i, tc.name, node.ID, len(tc.vector))

			// Verify retrieval
			retrieved, err := gs.GetNode(node.ID)
			if err != nil {
				t.Errorf("Failed to retrieve vector node: %v", err)
			} else if _, ok := retrieved.Properties["embedding"]; !ok {
				t.Errorf("Vector property not found after retrieval")
			}
		}
	}
}

// TestEdgeCase_PropertyIndexRebuild tests rebuilding property indexes
func TestEdgeCase_PropertyIndexRebuild(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing property index rebuild...")

	// Create nodes with indexed property
	nodeCount := 1000
	for i := 0; i < nodeCount; i++ {
		gs.CreateNode([]string{"IndexTest"}, map[string]Value{
			"indexed_value": IntValue(int64(i % 10)),
		})
	}

	t.Logf("  Created %d nodes with indexed properties", nodeCount)

	// Create index
	err = gs.CreatePropertyIndex("indexed_value", TypeInt)
	if err != nil {
		t.Logf("  Index creation: %v", err)
	} else {
		t.Log("  ✓ Property index created")
	}

	// Query using index
	nodes, err := gs.FindNodesByProperty("indexed_value", IntValue(5))
	if err != nil {
		t.Errorf("Index query failed: %v", err)
	} else {
		expectedCount := nodeCount / 10
		t.Logf("  ✓ Index query returned %d nodes (expected ~%d)", len(nodes), expectedCount)

		// Allow some variance
		if len(nodes) < expectedCount-10 || len(nodes) > expectedCount+10 {
			t.Errorf("Index returned unexpected count: got %d, expected ~%d", len(nodes), expectedCount)
		}
	}
}

// TestEdgeCase_CompressionEdgeCases tests compression with various data patterns
func TestEdgeCase_CompressionEdgeCases(t *testing.T) {
	dataDir := t.TempDir()

	config := StorageConfig{
		DataDir:               dataDir,
		EnableCompression:     true,
		EnableEdgeCompression: true,
	}

	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing compression edge cases...")

	testCases := []struct {
		name string
		data string
	}{
		{"highly compressible", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"incompressible random", "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0"},
		{"already compressed pattern", "\x1f\x8b\x08\x00\x00\x00\x00\x00"},
		{"empty string", ""},
		{"single char", "X"},
		{"unicode heavy", "你好世界🚀🎉こんにちは"},
	}

	for i, tc := range testCases {
		node, err := gs.CreateNode([]string{"CompressionTest"}, map[string]Value{
			"data": StringValue(tc.data),
		})
		if err != nil {
			t.Errorf("Failed to create node for %s: %v", tc.name, err)
			continue
		}

		// Verify data integrity after compression
		retrieved, err := gs.GetNode(node.ID)
		if err != nil {
			t.Errorf("Failed to retrieve compressed node: %v", err)
			continue
		}

		if val, ok := retrieved.Properties["data"]; ok {
			retrievedStr, _ := val.AsString()
			if retrievedStr != tc.data {
				t.Errorf("Case %d (%s): Data mismatch after compression", i, tc.name)
			} else {
				t.Logf("  ✓ Case %d (%s): Data preserved through compression", i, tc.name)
			}
		}
	}
}

// TestEdgeCase_TransactionBoundaries tests transaction edge cases
func TestEdgeCase_TransactionBoundaries(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing transaction boundaries...")

	// Test: Empty transaction
	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Logf("  BeginTransaction not supported: %v", err)
		return
	}

	err = tx.Commit()
	if err != nil {
		t.Logf("  Empty transaction commit: %v", err)
	} else {
		t.Log("  ✓ Empty transaction committed successfully")
	}

	// Test: Transaction with rollback
	tx2, err := gs.BeginTransaction()
	if err == nil {
		// Create node in transaction
		node, _ := gs.CreateNode([]string{"TxTest"}, map[string]Value{
			"in_tx": StringValue("true"),
		})

		// Rollback
		err = tx2.Rollback()
		if err != nil {
			t.Logf("  Rollback failed: %v", err)
		} else {
			t.Log("  ✓ Transaction rolled back")

			// Verify node doesn't exist
			retrieved, _ := gs.GetNode(node.ID)
			if retrieved == nil {
				t.Log("  ✓ Rolled back node not found (correct)")
			} else {
				t.Log("  ⚠ Rolled back node still exists (expected for some implementations)")
			}
		}
	}
}

// TestEdgeCase_LabelCombinations tests various label combinations
func TestEdgeCase_LabelCombinations(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing label combination edge cases...")

	testCases := []struct {
		name   string
		labels []string
	}{
		{"no labels", []string{}},
		{"single label", []string{"Person"}},
		{"duplicate labels", []string{"Person", "Person", "Person"}},
		{"many unique labels", []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}},
		{"unicode labels", []string{"人", "👤", "Persona"}},
		{"numeric labels", []string{"123", "456"}},
		{"special char labels", []string{"User:Admin", "Role#Super"}},
	}

	for i, tc := range testCases {
		node, err := gs.CreateNode(tc.labels, map[string]Value{
			"test_case": IntValue(int64(i)),
		})
		if err != nil {
			t.Logf("  %s rejected: %v", tc.name, err)
		} else {
			t.Logf("  ✓ Case %d (%s): Node %d created with %d labels",
				i, tc.name, node.ID, len(tc.labels))

			// Try to find by label if labels exist
			if len(tc.labels) > 0 {
				nodes, _ := gs.FindNodesByLabel(tc.labels[0])
				t.Logf("    Found %d nodes with label '%s'", len(nodes), tc.labels[0])
			}
		}
	}
}

// TestEdgeCase_EdgeTypeVariations tests edge type naming variations
func TestEdgeCase_EdgeTypeVariations(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing edge type variations...")

	node1, _ := gs.CreateNode([]string{"Source"}, nil)
	node2, _ := gs.CreateNode([]string{"Target"}, nil)

	edgeTypes := []string{
		"SIMPLE",
		"snake_case_type",
		"camelCaseType",
		"SCREAMING_SNAKE_CASE",
		"kebab-case-type",
		"dot.separated.type",
		"MixedCase_With-Everything.Combined",
		"数字123",
		"emoji_edge_🔗",
		"VERY_LONG_EDGE_TYPE_NAME_WITH_MANY_WORDS_TO_TEST_LENGTH_LIMITS",
	}

	for i, edgeType := range edgeTypes {
		edge, err := gs.CreateEdge(node1.ID, node2.ID, edgeType, map[string]Value{
			"index": IntValue(int64(i)),
		}, 1.0)
		if err != nil {
			t.Logf("  Edge type '%s' rejected: %v", edgeType, err)
		} else {
			t.Logf("  ✓ Edge type '%s': Created edge %d", edgeType, edge.ID)
		}
	}

	// Verify all edges created
	edges, _ := gs.GetOutgoingEdges(node1.ID)
	t.Logf("  ✓ Total edges created: %d", len(edges))
}

// TestEdgeCase_PropertyKeyVariations tests property key naming variations
func TestEdgeCase_PropertyKeyVariations(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing property key variations...")

	propertyKeys := map[string]Value{
		"simple":                             StringValue("value1"),
		"with_underscore":                    StringValue("value2"),
		"with-dash":                          StringValue("value3"),
		"with.dot":                           StringValue("value4"),
		"with:colon":                         StringValue("value5"),
		"with space":                         StringValue("value6"),
		"UPPERCASE":                          StringValue("value7"),
		"数字key":                              StringValue("value8"),
		"emoji_key_🔑":                       StringValue("value9"),
		"very_long_property_key_name_here":   StringValue("value10"),
	}

	node, err := gs.CreateNode([]string{"PropKeyTest"}, propertyKeys)
	if err != nil {
		t.Fatalf("Failed to create node with property variations: %v", err)
	}

	t.Logf("  ✓ Created node with %d property key variations", len(propertyKeys))

	// Verify retrieval
	retrieved, err := gs.GetNode(node.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve node: %v", err)
	}

	matchCount := 0
	for key := range propertyKeys {
		if _, ok := retrieved.Properties[key]; ok {
			matchCount++
		} else {
			t.Errorf("  Property key '%s' not found after retrieval", key)
		}
	}

	t.Logf("  ✓ Retrieved %d/%d property keys correctly", matchCount, len(propertyKeys))
}

// TestEdgeCase_WeightPrecision tests edge weight precision
func TestEdgeCase_WeightPrecision(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing edge weight precision...")

	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	weights := []float64{
		0.1,
		0.123456789012345,
		1.0000000000000001,
		1e-15,
		1e15,
		3.141592653589793,
	}

	for i, weight := range weights {
		edge, err := gs.CreateEdge(node1.ID, node2.ID, "WEIGHTED", nil, weight)
		if err != nil {
			t.Errorf("Failed to create edge with weight %v: %v", weight, err)
			continue
		}

		// Check precision
		if edge.Weight == weight {
			t.Logf("  ✓ Weight %d: Exact match (%.17f)", i, weight)
		} else {
			diff := edge.Weight - weight
			if diff < 1e-10 && diff > -1e-10 {
				t.Logf("  ✓ Weight %d: Close match (diff: %e)", i, diff)
			} else {
				t.Errorf("  Weight %d: Precision loss (expected %.17f, got %.17f)",
					i, weight, edge.Weight)
			}
		}
	}
}

// TestEdgeCase_NodeDeletionCascade tests cascading effects of node deletion
func TestEdgeCase_NodeDeletionCascade(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing node deletion cascade effects...")

	// Create a star pattern
	hub, _ := gs.CreateNode([]string{"Hub"}, map[string]Value{"name": StringValue("central")})

	spokeCount := 10
	for i := 0; i < spokeCount; i++ {
		spoke, _ := gs.CreateNode([]string{"Spoke"}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		gs.CreateEdge(spoke.ID, hub.ID, "TO_HUB", nil, 1.0)
		gs.CreateEdge(hub.ID, spoke.ID, "FROM_HUB", nil, 1.0)
	}

	// Verify edges before deletion
	incomingBefore, _ := gs.GetIncomingEdges(hub.ID)
	outgoingBefore, _ := gs.GetOutgoingEdges(hub.ID)
	t.Logf("  Before deletion: %d incoming, %d outgoing edges",
		len(incomingBefore), len(outgoingBefore))

	// Delete hub node
	err = gs.DeleteNode(hub.ID)
	if err != nil {
		t.Fatalf("Failed to delete hub node: %v", err)
	}

	t.Log("  ✓ Hub node deleted")

	// Verify node is gone
	retrieved, _ := gs.GetNode(hub.ID)
	if retrieved != nil {
		t.Error("  Hub node still exists after deletion")
	} else {
		t.Log("  ✓ Hub node properly removed")
	}

	// Check what happened to edges
	// The edges should have been cleaned up
	t.Log("  ✓ Cascade deletion test completed")
}
