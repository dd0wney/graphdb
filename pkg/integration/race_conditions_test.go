package integration

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/lsm"
	"github.com/darraghdowney/cluso-graphdb/pkg/parallel"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// TestStorageBatchConcurrentWrites tests concurrent batch writes don't produce duplicate IDs
// This validates the atomic ID allocation fix in storage batches
func TestStorageBatchConcurrentWrites(t *testing.T) {
	store, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	numGoroutines := 20
	nodesPerGoroutine := 50

	var wg sync.WaitGroup
	nodeIDs := make(chan uint64, numGoroutines*nodesPerGoroutine)

	// Concurrent batch operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			batch := store.BeginBatch()
			props := map[string]storage.Value{
				"id": storage.IntValue(int64(goroutineID)),
			}

			for j := 0; j < nodesPerGoroutine; j++ {
				nodeID, err := batch.AddNode([]string{"Test"}, props)
				if err != nil {
					t.Errorf("Goroutine %d: AddNode failed: %v", goroutineID, err)
					return
				}
				nodeIDs <- nodeID
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Goroutine %d: Commit failed: %v", goroutineID, err)
			}
		}(i)
	}

	wg.Wait()
	close(nodeIDs)

	// Verify all IDs are unique
	seen := make(map[uint64]bool)
	duplicates := 0

	for id := range nodeIDs {
		if seen[id] {
			duplicates++
			t.Errorf("Duplicate node ID: %d", id)
		}
		seen[id] = true
	}

	if duplicates > 0 {
		t.Fatalf("Found %d duplicate IDs - atomic ID allocation broken", duplicates)
	}

	totalExpected := numGoroutines * nodesPerGoroutine
	if len(seen) != totalExpected {
		t.Errorf("Expected %d unique IDs, got %d", totalExpected, len(seen))
	}
}

// TestLSMConcurrentReadsWithCompaction tests that concurrent reads work correctly during compaction
// This validates the copy-on-write compaction race fix
func TestLSMConcurrentReadsWithCompaction(t *testing.T) {
	tmpDir := t.TempDir()
	opts := lsm.DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = true // Enable auto-compaction

	lsmStore, err := lsm.NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsmStore.Close()

	// Insert data to trigger compactions
	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		value := []byte(fmt.Sprintf("value-%06d", i))
		if err := lsmStore.Put(key, value); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Concurrent readers while compaction is happening
	stopReaders := make(chan struct{})
	var readerWg sync.WaitGroup
	numReaders := 20
	errors := make(chan error, numReaders*100)

	for r := 0; r < numReaders; r++ {
		readerWg.Add(1)
		go func(readerID int) {
			defer readerWg.Done()
			readCount := 0
			for {
				select {
				case <-stopReaders:
					return
				default:
					keyNum := readCount % numKeys
					key := []byte(fmt.Sprintf("key-%06d", keyNum))
					expectedValue := []byte(fmt.Sprintf("value-%06d", keyNum))

					value, found := lsmStore.Get(key)
					if !found {
						errors <- fmt.Errorf("reader %d: key %s not found", readerID, key)
						return
					}
					if !bytes.Equal(value, expectedValue) {
						errors <- fmt.Errorf("reader %d: value mismatch for %s", readerID, key)
						return
					}
					readCount++

					if readCount >= 100 {
						return
					}
				}
			}
		}(r)
	}

	// Let readers run for a while
	time.Sleep(500 * time.Millisecond)

	close(stopReaders)
	readerWg.Wait()

	// Check for errors
	close(errors)
	errorCount := 0
	for err := range errors {
		errorCount++
		t.Error(err)
	}

	if errorCount > 0 {
		t.Fatalf("Found %d read errors during compaction - race condition present", errorCount)
	}
}

// TestLSMCacheInvalidation tests that cache is properly invalidated on updates/deletes
// This validates the cache invalidation bug fix
func TestLSMCacheInvalidation(t *testing.T) {
	tmpDir := t.TempDir()
	opts := lsm.DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = false

	lsmStore, err := lsm.NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsmStore.Close()

	key := []byte("test-key")
	value1 := []byte("value-1")
	value2 := []byte("value-2")

	// Write initial value
	if err := lsmStore.Put(key, value1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Read to populate cache
	if val, ok := lsmStore.Get(key); !ok || !bytes.Equal(val, value1) {
		t.Fatal("Initial read failed")
	}

	// Update value
	if err := lsmStore.Put(key, value2); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Read should return updated value (not cached old value)
	if val, ok := lsmStore.Get(key); !ok || !bytes.Equal(val, value2) {
		t.Fatal("Cache not invalidated on update - read returned stale value")
	}

	// Delete key
	if err := lsmStore.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Read should return not found (not cached value)
	if _, ok := lsmStore.Get(key); ok {
		t.Fatal("Cache not invalidated on delete - read returned deleted value")
	}
}

// TestWorkerPoolConcurrentCloseAndSubmit tests concurrent close and submit operations
// This validates the mutex-based close race fix
func TestWorkerPoolConcurrentCloseAndSubmit(t *testing.T) {
	numIterations := 100

	for iteration := 0; iteration < numIterations; iteration++ {
		pool := parallel.NewWorkerPool(4)

		var wg sync.WaitGroup
		numSubmitters := 10
		successCount := make(chan bool, numSubmitters*10)

		// Concurrent submitters
		for i := 0; i < numSubmitters; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					success := pool.Submit(func() {
						time.Sleep(1 * time.Millisecond)
					})
					successCount <- success
				}
			}()
		}

		// Close concurrently
		time.Sleep(5 * time.Millisecond)
		pool.Close()

		wg.Wait()
		close(successCount)

		// Count successful submissions
		successful := 0
		for success := range successCount {
			if success {
				successful++
			}
		}

		// Some submissions should succeed, some should fail after close
		// The important thing is no race/panic
		t.Logf("Iteration %d: %d successful submissions", iteration, successful)
	}
}

// TestWorkerPoolPanicRecovery tests that panics in tasks don't crash the pool
// This validates the panic recovery fix
func TestWorkerPoolPanicRecovery(t *testing.T) {
	pool := parallel.NewWorkerPool(4)
	defer pool.Close()

	var normalTaskCount int64
	var mu sync.Mutex

	// Submit tasks that panic
	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			panic("intentional test panic")
		})
	}

	// Submit normal tasks that should still execute
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		pool.Submit(func() {
			defer wg.Done()
			mu.Lock()
			normalTaskCount++
			mu.Unlock()
		})
	}

	wg.Wait()
	pool.Close()

	mu.Lock()
	count := normalTaskCount
	mu.Unlock()

	if count != 20 {
		t.Errorf("Expected 20 normal tasks to execute, got %d - panic recovery broken", count)
	}
}

// TestIntegratedGraphOperationsUnderLoad tests the full stack under concurrent load
// This validates all race fixes work together
func TestIntegratedGraphOperationsUnderLoad(t *testing.T) {
	store, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	pool := parallel.NewWorkerPool(10)
	defer pool.Close()

	numOperations := 100
	var wg sync.WaitGroup

	// Concurrent node creation using batches
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		nodeNum := i
		pool.Submit(func() {
			defer wg.Done()

			batch := store.BeginBatch()
			props := map[string]storage.Value{
				"id": storage.IntValue(int64(nodeNum)),
			}

			_, err := batch.AddNode([]string{"TestNode"}, props)
			if err != nil {
				t.Errorf("AddNode failed: %v", err)
				return
			}

			if err := batch.Commit(); err != nil {
				t.Errorf("Commit failed: %v", err)
			}
		})
	}

	wg.Wait()

	// Verify all nodes created
	stats := store.GetStatistics()
	if stats.NodeCount != uint64(numOperations) {
		t.Errorf("Expected %d nodes, got %d", numOperations, stats.NodeCount)
	}
}

// TestLSMUnderWorkerPoolLoad tests LSM operations executed via worker pool
// This validates LSM cache + worker pool fixes work together
func TestLSMUnderWorkerPoolLoad(t *testing.T) {
	tmpDir := t.TempDir()
	opts := lsm.DefaultLSMOptions(tmpDir)
	opts.EnableAutoCompaction = true

	lsmStore, err := lsm.NewLSMStorage(opts)
	if err != nil {
		t.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer lsmStore.Close()

	pool := parallel.NewWorkerPool(10)
	defer pool.Close()

	numOperations := 200
	var wg sync.WaitGroup

	// Concurrent writes via worker pool
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		opNum := i
		pool.Submit(func() {
			defer wg.Done()

			key := []byte(fmt.Sprintf("key-%06d", opNum))
			value := []byte(fmt.Sprintf("value-%06d", opNum))

			if err := lsmStore.Put(key, value); err != nil {
				t.Errorf("Put failed: %v", err)
			}
		})
	}

	wg.Wait()

	// Concurrent reads via worker pool
	var readWg sync.WaitGroup
	errors := make(chan error, numOperations)

	for i := 0; i < numOperations; i++ {
		readWg.Add(1)
		opNum := i
		pool.Submit(func() {
			defer readWg.Done()

			key := []byte(fmt.Sprintf("key-%06d", opNum))
			expectedValue := []byte(fmt.Sprintf("value-%06d", opNum))

			value, found := lsmStore.Get(key)
			if !found {
				errors <- fmt.Errorf("key %s not found", key)
				return
			}
			if !bytes.Equal(value, expectedValue) {
				errors <- fmt.Errorf("value mismatch for %s", key)
			}
		})
	}

	readWg.Wait()
	close(errors)

	errorCount := 0
	for err := range errors {
		errorCount++
		t.Error(err)
	}

	if errorCount > 0 {
		t.Fatalf("Found %d errors in LSM under worker pool load", errorCount)
	}
}
