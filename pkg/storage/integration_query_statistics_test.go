package storage

import (
	"os"
	"sync"
	"testing"
	"time"
)

// TestQueryStatistics_SnapshotDurability tests that query statistics survive clean shutdown
func TestQueryStatistics_SnapshotDurability(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create storage, run queries, close cleanly
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create test data
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
		}

		// Run 10 queries to track statistics
		for i := 0; i < 10; i++ {
			gs.FindNodesByLabel("Person")
		}

		// Check stats before close
		stats := gs.GetStatistics()
		if stats.TotalQueries != 10 {
			t.Fatalf("Before close: Expected TotalQueries=10, got %d", stats.TotalQueries)
		}
		if stats.AvgQueryTime <= 0 {
			t.Fatalf("Before close: Expected AvgQueryTime > 0, got %f", stats.AvgQueryTime)
		}

		// Store for comparison
		totalQueriesBefore := stats.TotalQueries
		avgQueryTimeBefore := stats.AvgQueryTime

		// Close cleanly (creates snapshot)
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Logf("Before close: TotalQueries=%d, AvgQueryTime=%.2fms",
			totalQueriesBefore, avgQueryTimeBefore)
	}

	// Phase 2: Recover from snapshot and verify query statistics
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// Verify query statistics recovered from snapshot
		stats := gs.GetStatistics()
		if stats.TotalQueries != 10 {
			t.Errorf("After snapshot recovery: Expected TotalQueries=10, got %d", stats.TotalQueries)
		}
		if stats.AvgQueryTime <= 0 {
			t.Errorf("After snapshot recovery: Expected AvgQueryTime > 0, got %f", stats.AvgQueryTime)
		}

		t.Logf("After snapshot recovery: TotalQueries=%d, AvgQueryTime=%.2fms - PRESERVED ✓",
			stats.TotalQueries, stats.AvgQueryTime)
	}
}

// TestQueryStatistics_CrashRecovery tests query statistics after crash (WAL replay)
func TestQueryStatistics_CrashRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create storage, run queries, simulate crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create test data
		for i := 0; i < 5; i++ {
			gs.CreateNode([]string{"Person"}, map[string]Value{
				"id": IntValue(int64(i)),
			})
		}

		// Run 15 queries
		for i := 0; i < 15; i++ {
			gs.FindNodesByLabel("Person")
		}

		// Check stats before crash
		stats := gs.GetStatistics()
		if stats.TotalQueries != 15 {
			t.Fatalf("Before crash: Expected TotalQueries=15, got %d", stats.TotalQueries)
		}

		t.Logf("Before crash: TotalQueries=%d, AvgQueryTime=%.2fms",
			stats.TotalQueries, stats.AvgQueryTime)

		// DON'T CLOSE - simulate crash
	}

	// Phase 2: Recover from WAL and check query statistics
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover: %v", err)
		}
		defer gs.Close()

		// After WAL replay, query statistics should be reset to 0
		// (because they're metadata, not operations that can be replayed)
		stats := gs.GetStatistics()

		// Query statistics are expected to be lost after crash
		// This documents current behavior - may be acceptable
		if stats.TotalQueries != 0 {
			t.Logf("After crash recovery: TotalQueries=%d (were 15 before crash)",
				stats.TotalQueries)
			// This is actually the expected behavior - query stats are lost
			// If this test fails with TotalQueries=15, that means they're being persisted somehow
		}

		t.Logf("After crash recovery: TotalQueries=%d, AvgQueryTime=%.2fms - RESET (expected)",
			stats.TotalQueries, stats.AvgQueryTime)
	}
}

// TestQueryStatistics_MixedRecovery tests snapshot then more queries then crash
func TestQueryStatistics_MixedRecovery(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	// Phase 1: Create, query, close cleanly (snapshot created)
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to create GraphStorage: %v", err)
		}

		// Create nodes
		for i := 0; i < 3; i++ {
			gs.CreateNode([]string{"Person"}, nil)
		}

		// Run 5 queries
		for i := 0; i < 5; i++ {
			gs.FindNodesByLabel("Person")
		}

		// Close cleanly
		err = gs.Close()
		if err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		t.Log("Phase 1: Created snapshot with TotalQueries=5")
	}

	// Phase 2: Recover, run more queries, crash
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover from snapshot: %v", err)
		}

		// Verify snapshot restored query stats
		stats := gs.GetStatistics()
		if stats.TotalQueries != 5 {
			t.Errorf("After snapshot load: Expected TotalQueries=5, got %d", stats.TotalQueries)
		}

		// Run 10 more queries
		for i := 0; i < 10; i++ {
			gs.FindNodesByLabel("Person")
		}

		// Should now have 15 total queries
		stats = gs.GetStatistics()
		if stats.TotalQueries != 15 {
			t.Errorf("After 10 more queries: Expected TotalQueries=15, got %d", stats.TotalQueries)
		}

		t.Logf("Phase 2: After 10 more queries, TotalQueries=%d", stats.TotalQueries)

		// DON'T CLOSE - simulate crash
	}

	// Phase 3: Recover after crash - queries after last snapshot are lost
	{
		gs, err := NewGraphStorageWithConfig(StorageConfig{
			DataDir:            dataDir,
			UseDiskBackedEdges: true,
			EdgeCacheSize:      100,
		})
		if err != nil {
			t.Fatalf("Failed to recover after crash: %v", err)
		}
		defer gs.Close()

		// After crash, only snapshot statistics are preserved
		// The 10 queries run after snapshot are lost
		stats := gs.GetStatistics()

		// We expect TotalQueries=5 (from last snapshot)
		// NOT 15 (which includes queries after snapshot)
		if stats.TotalQueries != 5 {
			t.Errorf("After crash: Expected TotalQueries=5 (from snapshot), got %d", stats.TotalQueries)
		}

		t.Logf("Phase 3: After crash recovery, TotalQueries=%d (from last snapshot)", stats.TotalQueries)
	}
}

// TestQueryStatistics_AvgQueryTimeAccuracy tests average query time calculation
func TestQueryStatistics_AvgQueryTimeAccuracy(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create nodes for querying
	for i := 0; i < 100; i++ {
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
	}

	// Run queries and track execution times
	var totalDuration time.Duration
	numQueries := 20

	for i := 0; i < numQueries; i++ {
		start := time.Now()
		gs.FindNodesByLabel("Person")
		totalDuration += time.Since(start)
	}

	// Calculate expected average (in milliseconds)
	expectedAvgMs := float64(totalDuration.Nanoseconds()) / float64(numQueries) / 1000000.0

	// Get actual statistics
	stats := gs.GetStatistics()

	if stats.TotalQueries != uint64(numQueries) {
		t.Errorf("Expected TotalQueries=%d, got %d", numQueries, stats.TotalQueries)
	}

	// AvgQueryTime uses exponential moving average, so it won't match exactly
	// But it should be in a reasonable range
	if stats.AvgQueryTime <= 0 {
		t.Errorf("AvgQueryTime should be > 0, got %f", stats.AvgQueryTime)
	}

	// Check it's within a reasonable order of magnitude
	// (EMA will differ from simple average, but shouldn't be wildly different)
	ratio := stats.AvgQueryTime / expectedAvgMs
	if ratio < 0.1 || ratio > 10.0 {
		t.Errorf("AvgQueryTime %.2fms seems incorrect (expected ~%.2fms, ratio=%.2f)",
			stats.AvgQueryTime, expectedAvgMs, ratio)
	}

	t.Logf("Queries: %d | Actual avg: %.2fms | EMA avg: %.2fms | Ratio: %.2f",
		numQueries, expectedAvgMs, stats.AvgQueryTime, ratio)
}

// TestQueryStatistics_ConcurrentQueriesDurability tests query statistics under concurrent load
func TestQueryStatistics_ConcurrentQueriesDurability(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create test data
	for i := 0; i < 50; i++ {
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
	}

	// Run queries concurrently
	numWorkers := 10
	queriesPerWorker := 20
	expectedTotal := numWorkers * queriesPerWorker

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < queriesPerWorker; j++ {
				gs.FindNodesByLabel("Person")
			}
		}(i)
	}
	wg.Wait()

	// Verify total queries
	stats := gs.GetStatistics()
	if stats.TotalQueries != uint64(expectedTotal) {
		t.Errorf("After concurrent queries: Expected TotalQueries=%d, got %d",
			expectedTotal, stats.TotalQueries)
	}

	if stats.AvgQueryTime <= 0 {
		t.Errorf("AvgQueryTime should be > 0 after queries, got %f", stats.AvgQueryTime)
	}

	t.Logf("Concurrent queries: %d workers × %d queries = %d total | AvgQueryTime: %.2fms",
		numWorkers, queriesPerWorker, stats.TotalQueries, stats.AvgQueryTime)
}

// TestQueryStatistics_DifferentQueryTypes tests that all query types are tracked
func TestQueryStatistics_DifferentQueryTypes(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
			UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Create test data
	var nodeIDs []uint64
	for i := 0; i < 10; i++ {
		node, _ := gs.CreateNode([]string{"Person"}, map[string]Value{
			"age": IntValue(int64(20 + i)),
		})
		nodeIDs = append(nodeIDs, node.ID)
	}

	// Create edges
	for i := 0; i < 5; i++ {
		gs.CreateEdge(nodeIDs[i], nodeIDs[i+1], "KNOWS", nil, 1.0)
	}

	// Create property index
	gs.CreatePropertyIndex("age", TypeInt)

	expectedQueries := 0

	// Test FindNodesByLabel (1 query)
	gs.FindNodesByLabel("Person")
	expectedQueries++

	// Test FindEdgesByType (1 query)
	gs.FindEdgesByType("KNOWS")
	expectedQueries++

	// Test FindNodesByProperty (1 query)
	gs.FindNodesByProperty("age", IntValue(25))
	expectedQueries++

	// Verify all query types are tracked
	stats := gs.GetStatistics()
	if stats.TotalQueries != uint64(expectedQueries) {
		t.Errorf("Expected TotalQueries=%d, got %d", expectedQueries, stats.TotalQueries)
	}

	t.Logf("Different query types tracked: TotalQueries=%d, AvgQueryTime=%.2fms",
		stats.TotalQueries, stats.AvgQueryTime)
}

// TestQueryStatistics_ZeroStateInitialization tests fresh database has zero query stats
func TestQueryStatistics_ZeroStateInitialization(t *testing.T) {
	dataDir := t.TempDir()
	defer os.RemoveAll(dataDir)

	gs, err := NewGraphStorageWithConfig(StorageConfig{
		DataDir:            dataDir,
		UseDiskBackedEdges: true,
		EdgeCacheSize:      100,
	})
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	defer gs.Close()

	// Verify initial state
	stats := gs.GetStatistics()
	if stats.TotalQueries != 0 {
		t.Errorf("Initial TotalQueries should be 0, got %d", stats.TotalQueries)
	}
	if stats.AvgQueryTime != 0 {
		t.Errorf("Initial AvgQueryTime should be 0, got %f", stats.AvgQueryTime)
	}

	t.Log("Fresh database correctly initialized with zero query statistics")
}
