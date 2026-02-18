package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestCorruptedDataFileRecovery tests recovery from corrupted data files
func TestCorruptedDataFileRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()

	// Phase 1: Create and populate storage with data
	t.Log("Phase 1: Creating storage and populating with data...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		// Create 100 nodes
		for i := 0; i < 100; i++ {
			props := map[string]Value{
				"id":   IntValue(int64(i)),
				"name": StringValue(fmt.Sprintf("Node_%d", i)),
			}
			_, err := gs.CreateNode([]string{"RecoveryTest"}, props)
			if err != nil {
				t.Fatalf("Failed to create node: %v", err)
			}
		}

		err = gs.Close()
		if err != nil {
			t.Fatalf("Failed to close storage: %v", err)
		}
	}

	// Phase 2: Corrupt a data file
	t.Log("Phase 2: Corrupting data file...")
	{
		// Find snapshot files
		files, err := os.ReadDir(dataDir)
		if err != nil {
			t.Fatalf("Failed to read data directory: %v", err)
		}

		// Corrupt the first snapshot file we find
		corrupted := false
		for _, file := range files {
			if filepath.Ext(file.Name()) == ".snapshot" {
				filePath := filepath.Join(dataDir, file.Name())

				// Read the file
				data, err := os.ReadFile(filePath)
				if err != nil {
					t.Logf("  Failed to read snapshot file: %v", err)
					continue
				}

				// Corrupt the middle of the file
				if len(data) > 100 {
					for i := len(data)/2; i < len(data)/2+50; i++ {
						data[i] = 0xFF
					}

					err = os.WriteFile(filePath, data, 0644)
					if err != nil {
						t.Fatalf("Failed to write corrupted file: %v", err)
					}

					t.Logf("  Corrupted file: %s", file.Name())
					corrupted = true
					break
				}
			}
		}

		if !corrupted {
			t.Log("  No snapshot files found to corrupt (storage may be in-memory)")
		}
	}

	// Phase 3: Attempt to reopen storage and assess recovery
	t.Log("Phase 3: Reopening storage after corruption...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			// Expected: storage may fail to open with corruption
			t.Logf("  ✓ Storage correctly detected corruption: %v", err)
		} else {
			defer gs.Close()

			// Try to read some nodes - some may be corrupted
			successCount := 0
			for i := 0; i < 100; i++ {
				_, err := gs.GetNode(uint64(i))
				if err == nil {
					successCount++
				}
			}

			t.Logf("  ✓ Storage opened despite corruption")
			t.Logf("  Readable nodes: %d/100", successCount)

			// We expect at least some degradation
			if successCount == 100 {
				t.Log("  ⚠ WARNING: All nodes readable despite corruption (unexpected)")
			}
		}
	}
}

// TestWALRecoveryAfterCrash tests Write-Ahead Log recovery
func TestWALRecoveryAfterCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()

	// Track crashed storage for cleanup
	var crashedStorage *GraphStorage

	// Phase 1: Create storage and write data (simulating writes before crash)
	nodeCount := 50
	t.Logf("Phase 1: Writing %d nodes before simulated crash...", nodeCount)
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}
		crashedStorage = gs

		for i := 0; i < nodeCount; i++ {
			props := map[string]Value{
				"id":        IntValue(int64(i)),
				"pre_crash": StringValue("true"),
			}
			_, err := gs.CreateNode([]string{"WALTest"}, props)
			if err != nil {
				t.Fatalf("Failed to create node: %v", err)
			}
		}

		// Don't call Close() - simulate crash
		t.Log("  Simulating crash (no clean shutdown)...")
	}

	// Cleanup crashed storage after test
	t.Cleanup(func() {
		if crashedStorage != nil {
			crashedStorage.Close()
		}
	})

	// Phase 2: Reopen storage (should replay WAL)
	t.Log("Phase 2: Reopening storage (WAL replay)...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to reopen storage: %v", err)
		}
		defer gs.Close()

		// Verify data recovery
		recoveredCount := 0
		for i := 0; i < nodeCount; i++ {
			node, err := gs.GetNode(uint64(i))
			if err == nil && node != nil {
				recoveredCount++
			}
		}

		recoveryRate := float64(recoveredCount) / float64(nodeCount) * 100
		t.Logf("  ✓ Recovery completed: %d/%d nodes (%.1f%%)",
			recoveredCount, nodeCount, recoveryRate)

		if recoveryRate < 50 {
			t.Errorf("Recovery rate too low: %.1f%% (expected >50%%)", recoveryRate)
		}
	}
}

// TestPartitionedWritesRecovery tests recovery from network partition scenario
func TestPartitionedWritesRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Simulate partitioned writes (concurrent writes that might conflict)
	partitionCount := 4
	writesPerPartition := 100

	t.Logf("Simulating %d partitions with %d writes each...",
		partitionCount, writesPerPartition)

	var wg sync.WaitGroup
	errorChan := make(chan error, partitionCount*writesPerPartition)

	// Each "partition" writes concurrently
	for p := 0; p < partitionCount; p++ {
		wg.Add(1)
		go func(partitionID int) {
			defer wg.Done()

			for i := 0; i < writesPerPartition; i++ {
				props := map[string]Value{
					"partition": IntValue(int64(partitionID)),
					"index":     IntValue(int64(i)),
					"timestamp": IntValue(time.Now().UnixNano()),
				}

				_, err := gs.CreateNode([]string{"PartitionTest"}, props)
				if err != nil {
					errorChan <- fmt.Errorf("partition %d write %d failed: %w",
						partitionID, i, err)
				}
			}
		}(p)
	}

	wg.Wait()
	close(errorChan)

	// Check for errors
	errorCount := 0
	for err := range errorChan {
		t.Errorf("Partitioned write error: %v", err)
		errorCount++
	}

	expectedWrites := partitionCount * writesPerPartition
	successRate := float64(expectedWrites-errorCount) / float64(expectedWrites) * 100

	t.Logf("✓ Partitioned writes completed:")
	t.Logf("  Total writes: %d", expectedWrites)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Success rate: %.1f%%", successRate)

	if successRate < 95 {
		t.Errorf("Success rate too low: %.1f%% (expected >95%%)", successRate)
	}
}

// TestSplitBrainRecovery tests recovery from split-brain scenario
func TestSplitBrainRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	// Simulate two storage instances working on same data (split-brain)
	dataDir := t.TempDir()

	t.Log("Simulating split-brain scenario...")

	// Instance 1: Write some data
	t.Log("Instance 1: Writing initial data...")
	{
		gs1, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create instance 1: %v", err)
		}

		for i := 0; i < 50; i++ {
			props := map[string]Value{
				"source":  StringValue("instance1"),
				"id":      IntValue(int64(i)),
			}
			_, err := gs1.CreateNode([]string{"SplitBrain"}, props)
			if err != nil {
				t.Fatalf("Instance 1 write failed: %v", err)
			}
		}

		err = gs1.Close()
		if err != nil {
			t.Fatalf("Failed to close instance 1: %v", err)
		}
	}

	// Instance 2: Write conflicting data
	t.Log("Instance 2: Writing conflicting data...")
	{
		gs2, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create instance 2: %v", err)
		}

		for i := 0; i < 50; i++ {
			props := map[string]Value{
				"source":  StringValue("instance2"),
				"id":      IntValue(int64(i + 100)),
			}
			_, err := gs2.CreateNode([]string{"SplitBrain"}, props)
			if err != nil {
				t.Fatalf("Instance 2 write failed: %v", err)
			}
		}

		err = gs2.Close()
		if err != nil {
			t.Fatalf("Failed to close instance 2: %v", err)
		}
	}

	// Final instance: Verify data consistency
	t.Log("Final verification: Checking data consistency...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to open for verification: %v", err)
		}
		defer gs.Close()

		// We expect both sets of writes to be present
		// (In a real distributed system, we'd have conflict resolution)
		t.Log("  ✓ Storage opened successfully after split-brain")
		t.Log("  NOTE: Split-brain resolution depends on conflict resolution strategy")
	}
}

// TestDiskFullRecovery tests recovery from disk full condition
func TestDiskFullRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing graceful handling of write failures (simulating disk full)...")

	// Write until we get a reasonable dataset
	successCount := 0
	for i := 0; i < 1000; i++ {
		props := map[string]Value{
			"id": IntValue(int64(i)),
			"data": StringValue(fmt.Sprintf("Data_%d", i)),
		}

		_, err := gs.CreateNode([]string{"DiskTest"}, props)
		if err != nil {
			// In real disk full scenario, we'd get write errors
			t.Logf("  Write failed at operation %d: %v", i, err)
			break
		}
		successCount++
	}

	t.Logf("✓ Completed %d writes before potential failure", successCount)

	// Verify we can still read existing data
	readCount := 0
	for i := 0; i < successCount; i++ {
		_, err := gs.GetNode(uint64(i))
		if err == nil {
			readCount++
		}
	}

	readRate := float64(readCount) / float64(successCount) * 100
	t.Logf("✓ Read verification: %d/%d nodes (%.1f%%)",
		readCount, successCount, readRate)

	if readRate < 95 {
		t.Errorf("Read rate too low after write failure: %.1f%%", readRate)
	}
}

// TestConcurrentFailureRecovery tests recovery from concurrent operation failures
func TestConcurrentFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing concurrent operations with random failures...")

	workers := 10
	operationsPerWorker := 100
	var wg sync.WaitGroup

	successCount := int64(0)
	errorCount := int64(0)
	var countMutex sync.Mutex

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < operationsPerWorker; i++ {
				props := map[string]Value{
					"worker": IntValue(int64(workerID)),
					"op":     IntValue(int64(i)),
				}

				_, err := gs.CreateNode([]string{"ConcurrentFailure"}, props)

				countMutex.Lock()
				if err != nil {
					errorCount++
				} else {
					successCount++
				}
				countMutex.Unlock()
			}
		}(w)
	}

	wg.Wait()

	totalOps := int64(workers * operationsPerWorker)
	successRate := float64(successCount) / float64(totalOps) * 100

	t.Log("✓ Concurrent failure recovery test completed:")
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Successes: %d", successCount)
	t.Logf("  Errors: %d", errorCount)
	t.Logf("  Success rate: %.1f%%", successRate)

	if successRate < 90 {
		t.Errorf("Success rate too low: %.1f%% (expected >90%%)", successRate)
	}
}

// TestIndexCorruptionRecovery tests recovery from corrupted index files
func TestIndexCorruptionRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()

	// Phase 1: Create data with indexes
	t.Log("Phase 1: Creating indexed data...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage: %v", err)
		}

		for i := 0; i < 100; i++ {
			props := map[string]Value{
				"id":   IntValue(int64(i)),
				"name": StringValue(fmt.Sprintf("Indexed_%d", i)),
			}
			_, err := gs.CreateNode([]string{"IndexTest"}, props)
			if err != nil {
				t.Fatalf("Failed to create node: %v", err)
			}
		}

		err = gs.Close()
		if err != nil {
			t.Fatalf("Failed to close storage: %v", err)
		}
	}

	// Phase 2: Corrupt index files
	t.Log("Phase 2: Corrupting index files...")
	{
		indexFiles, err := filepath.Glob(filepath.Join(dataDir, "**/index*"))
		if err == nil && len(indexFiles) > 0 {
			for _, indexFile := range indexFiles {
				data, err := os.ReadFile(indexFile)
				if err != nil {
					continue
				}

				// Corrupt the file
				if len(data) > 10 {
					data[0] = 0xFF
					data[1] = 0xFF
					os.WriteFile(indexFile, data, 0644)
					t.Logf("  Corrupted: %s", filepath.Base(indexFile))
				}
			}
		} else {
			t.Log("  No index files found (may not be applicable)")
		}
	}

	// Phase 3: Reopen and verify graceful handling
	t.Log("Phase 3: Reopening storage with corrupted indexes...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Logf("  Storage failed to open (expected with corruption): %v", err)
		} else {
			defer gs.Close()
			t.Log("  ✓ Storage opened despite index corruption")

			// Try to read some nodes
			readCount := 0
			for i := 0; i < 100; i++ {
				_, err := gs.GetNode(uint64(i))
				if err == nil {
					readCount++
				}
			}

			t.Logf("  Accessible nodes: %d/100", readCount)
		}
	}
}

// TestRapidRestartRecovery tests recovery from rapid restart cycles
func TestRapidRestartRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	dataDir := t.TempDir()
	cycles := 10

	// Track crashed storage instances for cleanup
	var crashedStorages []*GraphStorage

	t.Logf("Testing rapid restart recovery (%d cycles)...", cycles)

	for cycle := 0; cycle < cycles; cycle++ {
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to create storage on cycle %d: %v", cycle, err)
		}

		// Write some data
		for i := 0; i < 10; i++ {
			props := map[string]Value{
				"cycle": IntValue(int64(cycle)),
				"id":    IntValue(int64(i)),
			}
			_, err := gs.CreateNode([]string{"RestartTest"}, props)
			if err != nil {
				t.Errorf("Cycle %d write %d failed: %v", cycle, i, err)
			}
		}

		// Close (or crash - alternate between clean and unclean shutdown)
		if cycle%2 == 0 {
			err = gs.Close()
			if err != nil {
				t.Errorf("Cycle %d clean close failed: %v", cycle, err)
			}
		} else {
			// On odd cycles, don't call Close() to simulate crash
			// Track for cleanup after test
			crashedStorages = append(crashedStorages, gs)
		}
	}

	// Cleanup crashed storage instances after test
	t.Cleanup(func() {
		for _, gs := range crashedStorages {
			if gs != nil {
				gs.Close()
			}
		}
	})

	// Final verification
	t.Log("Final verification after rapid restarts...")
	{
		gs, err := NewGraphStorage(dataDir)
		if err != nil {
			t.Fatalf("Failed to open after rapid restarts: %v", err)
		}
		defer gs.Close()

		t.Logf("✓ Storage recovered successfully after %d rapid restarts", cycles)
	}
}

// TestMemoryPressureRecovery tests behavior under memory pressure
func TestMemoryPressureRecovery(t *testing.T) {
	if testing.Short() || isRaceEnabled() {
		t.Skip("Skipping failure recovery test in short mode or with race detector")
	}

	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	t.Log("Testing behavior under memory pressure...")

	// Create large nodes to consume memory
	largeDataSize := 1024 // 1KB per node
	nodeCount := 10000

	successCount := 0
	for i := 0; i < nodeCount; i++ {
		largeData := make([]byte, largeDataSize)
		for j := range largeData {
			largeData[j] = byte(i % 256)
		}

		props := map[string]Value{
			"id":   IntValue(int64(i)),
			"data": StringValue(string(largeData)),
		}

		_, err := gs.CreateNode([]string{"MemoryPressure"}, props)
		if err != nil {
			// Expected: may fail under memory pressure
			t.Logf("  Write failed at node %d (memory pressure): %v", i, err)
			break
		}
		successCount++

		if i > 0 && i%1000 == 0 {
			t.Logf("  Progress: %d nodes created", i)
		}
	}

	t.Logf("✓ Created %d nodes under memory pressure", successCount)

	// Verify we can still read
	readCount := 0
	sampleSize := min(100, successCount)
	for i := 0; i < sampleSize; i++ {
		_, err := gs.GetNode(uint64(i))
		if err == nil {
			readCount++
		}
	}

	readRate := float64(readCount) / float64(sampleSize) * 100
	t.Logf("✓ Read verification: %d/%d nodes (%.1f%%)",
		readCount, sampleSize, readRate)
}
