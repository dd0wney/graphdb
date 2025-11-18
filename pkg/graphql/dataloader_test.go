package graphql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestDataLoaderBasicBatching tests that multiple Load calls are batched
func TestDataLoaderBasicBatching(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	// Batch function that tracks how many times it's called
	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		mu.Lock()
		callCount++
		mu.Unlock()

		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			results[i] = fmt.Sprintf("value-%s", key)
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})

	// Load multiple keys concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", idx)
			result, err := loader.Load(context.Background(), key)
			if err != nil {
				t.Errorf("Load failed: %v", err)
				return
			}
			expected := fmt.Sprintf("value-%s", key)
			if result != expected {
				t.Errorf("Expected %s, got %v", expected, result)
			}
		}(i)
	}

	wg.Wait()

	// Should have called batch function only once (or very few times due to timing)
	mu.Lock()
	defer mu.Unlock()
	if callCount > 3 {
		t.Errorf("Expected batch function to be called 1-3 times due to batching, got %d", callCount)
	}
}

// TestDataLoaderCaching tests that duplicate keys are cached
func TestDataLoaderCaching(t *testing.T) {
	loadCount := make(map[string]int)
	var mu sync.Mutex

	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		mu.Lock()
		defer mu.Unlock()

		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			loadCount[key]++
			results[i] = fmt.Sprintf("value-%s", key)
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})

	ctx := context.Background()

	// Load the same key multiple times
	for i := 0; i < 5; i++ {
		result, err := loader.Load(ctx, "key1")
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}
		if result != "value-key1" {
			t.Errorf("Expected value-key1, got %v", result)
		}
	}

	// Key should only be loaded once due to caching
	mu.Lock()
	defer mu.Unlock()
	if loadCount["key1"] != 1 {
		t.Errorf("Expected key to be loaded once, got %d times", loadCount["key1"])
	}
}

// TestDataLoaderErrorHandling tests that errors are properly propagated
func TestDataLoaderErrorHandling(t *testing.T) {
	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			if key == "error-key" {
				errors[i] = fmt.Errorf("simulated error for %s", key)
			} else {
				results[i] = fmt.Sprintf("value-%s", key)
			}
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})

	ctx := context.Background()

	// Load a key that will error
	result, err := loader.Load(ctx, "error-key")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if result != nil {
		t.Errorf("Expected nil result on error, got %v", result)
	}

	// Load a key that will succeed
	result, err = loader.Load(ctx, "success-key")
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if result != "value-success-key" {
		t.Errorf("Expected value-success-key, got %v", result)
	}
}

// TestDataLoaderCacheClear tests that cache can be cleared
func TestDataLoaderCacheClear(t *testing.T) {
	loadCount := 0
	var mu sync.Mutex

	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		mu.Lock()
		loadCount++
		mu.Unlock()

		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			results[i] = fmt.Sprintf("value-%d-%s", loadCount, key)
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})

	ctx := context.Background()

	// Load a key
	result1, _ := loader.Load(ctx, "key1")

	// Clear cache
	loader.ClearAll()

	// Load the same key again - should reload
	result2, _ := loader.Load(ctx, "key1")

	// Results should be different due to reload
	if result1 == result2 {
		t.Errorf("Expected different results after cache clear, got same: %v", result1)
	}
}

// TestDataLoaderWithGraphStorage tests DataLoader with actual graph storage
func TestDataLoaderWithGraphStorage(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})

	// Create edges
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{}, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "KNOWS", map[string]storage.Value{}, 1.0)

	// Create a node loader
	nodeLoader := NewNodeDataLoader(gs)

	ctx := context.Background()

	// Load nodes concurrently
	var wg sync.WaitGroup
	nodeIDs := []uint64{node1.ID, node2.ID, node3.ID}

	for _, nodeID := range nodeIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			result, err := nodeLoader.Load(ctx, fmt.Sprintf("%d", id))
			if err != nil {
				t.Errorf("Failed to load node %d: %v", id, err)
				return
			}
			node, ok := result.(*storage.Node)
			if !ok {
				t.Errorf("Expected *storage.Node, got %T", result)
				return
			}
			if node.ID != id {
				t.Errorf("Expected node ID %d, got %d", id, node.ID)
			}
		}(nodeID)
	}

	wg.Wait()
}

// TestDataLoaderWithEdgeTraversal tests solving N+1 problem for edge traversal
func TestDataLoaderWithEdgeTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create 10 person nodes
	personIDs := make([]uint64, 10)
	for i := 0; i < 10; i++ {
		node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("Person%d", i)),
		})
		personIDs[i] = node.ID
	}

	// Create edges from person 0 to all others
	for i := 1; i < 10; i++ {
		gs.CreateEdge(personIDs[0], personIDs[i], "KNOWS", map[string]storage.Value{}, 1.0)
	}

	// Create edge loader with batching
	edgeLoader := NewOutgoingEdgesDataLoader(gs)

	ctx := context.Background()

	// Load outgoing edges for all persons - should be batched
	var wg sync.WaitGroup
	for _, personID := range personIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			result, err := edgeLoader.Load(ctx, fmt.Sprintf("%d", id))
			if err != nil {
				t.Errorf("Failed to load edges for node %d: %v", id, err)
				return
			}
			edges, ok := result.([]*storage.Edge)
			if !ok {
				t.Errorf("Expected []*storage.Edge, got %T", result)
				return
			}
			// Person 0 should have 9 outgoing edges, others should have 0
			if id == personIDs[0] {
				if len(edges) != 9 {
					t.Errorf("Expected person 0 to have 9 edges, got %d", len(edges))
				}
			} else {
				if len(edges) != 0 {
					t.Errorf("Expected person %d to have 0 edges, got %d", id, len(edges))
				}
			}
		}(personID)
	}

	wg.Wait()
}

// TestDataLoaderBatchSizeLimit tests that batches respect size limits
func TestDataLoaderBatchSizeLimit(t *testing.T) {
	batchSizes := []int{}
	var mu sync.Mutex

	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		mu.Lock()
		batchSizes = append(batchSizes, len(keys))
		mu.Unlock()

		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			results[i] = fmt.Sprintf("value-%s", key)
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 5, // Small batch size
		Wait:      10 * time.Millisecond,
	})

	ctx := context.Background()

	// Load 20 keys
	for i := 0; i < 20; i++ {
		go func(idx int) {
			loader.Load(ctx, fmt.Sprintf("key%d", idx))
		}(i)
	}

	// Wait for all batches to complete
	time.Sleep(50 * time.Millisecond)

	// Check that no batch exceeded the limit
	mu.Lock()
	defer mu.Unlock()
	for _, size := range batchSizes {
		if size > 5 {
			t.Errorf("Batch size %d exceeded limit of 5", size)
		}
	}
}

// TestDataLoaderPrime tests priming the cache with known values
func TestDataLoaderPrime(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		mu.Lock()
		callCount++
		mu.Unlock()

		results := make([]interface{}, len(keys))
		errors := make([]error, len(keys))
		for i, key := range keys {
			results[i] = fmt.Sprintf("loaded-%s", key)
		}
		return results, errors
	}

	loader := NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})

	ctx := context.Background()

	// Prime the cache with a known value
	loader.Prime("key1", "primed-value")

	// Load the primed key - should not call batch function
	result, err := loader.Load(ctx, "key1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if result != "primed-value" {
		t.Errorf("Expected primed-value, got %v", result)
	}

	// Batch function should not have been called
	mu.Lock()
	defer mu.Unlock()
	if callCount != 0 {
		t.Errorf("Expected batch function to not be called, got %d calls", callCount)
	}
}
