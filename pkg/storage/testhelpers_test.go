package storage

import (
	"testing"
)

// testGraphStorage creates a new GraphStorage instance for testing with sensible defaults
// Returns the GraphStorage instance and a cleanup function
func testGraphStorage(t *testing.T, config ...StorageConfig) *GraphStorage {
	t.Helper()

	// Use provided config or create default
	var cfg StorageConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		// Default test config
		cfg = StorageConfig{
			DataDir:            t.TempDir(),
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		}
	}

	// Ensure DataDir is set
	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		if err := gs.Close(); err != nil {
			t.Logf("Warning: Close() failed during cleanup: %v", err)
		}
	})

	return gs
}

// testNode creates a test node with given labels and properties
func testNode(t *testing.T, gs *GraphStorage, labels []string, properties map[string]Value) *Node {
	t.Helper()

	node, err := gs.CreateNode(labels, properties)
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	return node
}

// testEdge creates a test edge between two nodes
func testEdge(t *testing.T, gs *GraphStorage, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) *Edge {
	t.Helper()

	edge, err := gs.CreateEdge(fromID, toID, edgeType, properties, weight)
	if err != nil {
		t.Fatalf("Failed to create test edge: %v", err)
	}

	return edge
}

// testGraphStorageWithNodes creates a GraphStorage and populates it with test nodes
// Returns the GraphStorage instance and a slice of created node IDs
func testGraphStorageWithNodes(t *testing.T, numNodes int, label string) (*GraphStorage, []uint64) {
	t.Helper()

	gs := testGraphStorage(t)
	nodeIDs := make([]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		node := testNode(t, gs, []string{label}, map[string]Value{
			"index": IntValue(int64(i)),
		})
		nodeIDs[i] = node.ID
	}

	return gs, nodeIDs
}

// testCrashableStorage creates a GraphStorage for crash simulation tests.
// It registers cleanup to close the storage after the test completes,
// allowing the test to skip calling Close() to simulate a crash while
// preventing goroutine leaks.
func testCrashableStorage(t *testing.T, dataDir string, config ...StorageConfig) *GraphStorage {
	t.Helper()

	var cfg StorageConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		}
	}

	cfg.DataDir = dataDir

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create crashable GraphStorage: %v", err)
	}

	// Register cleanup to prevent goroutine leaks
	// This runs after the test completes, even if Close() was never called
	t.Cleanup(func() {
		gs.Close()
	})

	return gs
}

// testCrashRecovery simulates a crash by NOT calling Close() and returns a new GraphStorage
// instance that will replay from the same data directory
func testCrashRecovery(t *testing.T, dataDir string, config ...StorageConfig) *GraphStorage {
	t.Helper()

	// Use provided config or create default
	var cfg StorageConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		}
	}

	// Ensure DataDir matches
	cfg.DataDir = dataDir

	gs, err := NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to recover GraphStorage after crash: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		if err := gs.Close(); err != nil {
			t.Logf("Warning: Close() failed during cleanup: %v", err)
		}
	})

	return gs
}
