package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 10000, "Number of nodes to create")
	edges := flag.Int("edges", 30000, "Number of edges to create")
	batchSize := flag.Int("batch", 100, "Batch size for batched WAL")
	flushInterval := flag.Duration("flush", 10*time.Millisecond, "Flush interval for batched WAL")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB WAL Optimization Benchmark\n")
	fmt.Printf("==========================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Edges: %d\n", *edges)
	fmt.Printf("  Batch Size: %d\n", *batchSize)
	fmt.Printf("  Flush Interval: %v\n\n", *flushInterval)

	// Clean up old data
	os.RemoveAll("./data/benchmark-regular")
	os.RemoveAll("./data/benchmark-batched")

	// Benchmark 1: Regular WAL (no batching)
	fmt.Printf("📊 Benchmark 1: Regular WAL (sync on every write)\n")
	regularTime := benchmarkStorage(storage.StorageConfig{
		DataDir:        "./data/benchmark-regular",
		EnableBatching: false,
	}, *nodes, *edges)

	// Benchmark 2: Batched WAL
	fmt.Printf("\n📊 Benchmark 2: Batched WAL (batch=%d, flush=%v)\n", *batchSize, *flushInterval)
	batchedTime := benchmarkStorage(storage.StorageConfig{
		DataDir:        "./data/benchmark-batched",
		EnableBatching: true,
		BatchSize:      *batchSize,
		FlushInterval:  *flushInterval,
	}, *nodes, *edges)

	// Comparison
	fmt.Printf("\n📈 Performance Comparison\n")
	fmt.Printf("========================\n")
	fmt.Printf("Regular WAL:\n")
	fmt.Printf("  Total time: %v\n", regularTime)
	fmt.Printf("  Node creation: %.2fμs per node\n", float64(regularTime.Microseconds())/float64(*nodes))
	fmt.Printf("  Throughput: %.0f nodes/sec\n\n", float64(*nodes)/regularTime.Seconds())

	fmt.Printf("Batched WAL:\n")
	fmt.Printf("  Total time: %v\n", batchedTime)
	fmt.Printf("  Node creation: %.2fμs per node\n", float64(batchedTime.Microseconds())/float64(*nodes))
	fmt.Printf("  Throughput: %.0f nodes/sec\n\n", float64(*nodes)/batchedTime.Seconds())

	improvement := float64(regularTime.Nanoseconds()) / float64(batchedTime.Nanoseconds())
	fmt.Printf("🚀 Speedup: %.2fx faster with batching\n", improvement)
	fmt.Printf("💾 Time saved: %v (%.1f%% reduction)\n", regularTime-batchedTime, (1.0-(1.0/improvement))*100)

	// Verify data integrity
	fmt.Printf("\n✅ Verifying data integrity...\n")
	verifyIntegrity("./data/benchmark-regular", *nodes, *edges)
	verifyIntegrity("./data/benchmark-batched", *nodes, *edges)
	fmt.Printf("✅ All data verified successfully!\n")
}

func benchmarkStorage(config storage.StorageConfig, nodeCount, _ int) time.Duration {
	start := time.Now()

	// Initialize storage
	graph, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}

	// Use workers to create concurrent load
	numWorkers := 10
	nodesPerWorker := nodeCount / numWorkers

	var wg sync.WaitGroup
	wg.Add(numWorkers)

	for workerID := 0; workerID < numWorkers; workerID++ {
		go func(id int) {
			defer wg.Done()

			startNode := id * nodesPerWorker
			endNode := startNode + nodesPerWorker

			for i := startNode; i < endNode; i++ {
				_, err := graph.CreateNode(
					[]string{"User"},
					map[string]storage.Value{
						"id":         storage.StringValue(fmt.Sprintf("user%d", i)),
						"trustScore": storage.IntValue(int64(rand.Intn(1000))),
						"created":    storage.TimestampValue(time.Now()),
					},
				)
				if err != nil {
					log.Printf("Failed to create node: %v", err)
				}

				if (i+1)%1000 == 0 {
					fmt.Printf("  Created %d nodes...\n", i+1)
				}
			}
		}(workerID)
	}

	// Wait for all workers to finish
	wg.Wait()

	// Close and measure total time
	if err := graph.Close(); err != nil {
		log.Fatalf("Failed to close storage: %v", err)
	}

	duration := time.Since(start)
	return duration
}

func verifyIntegrity(dataDir string, expectedNodes, _ int) {
	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		log.Fatalf("Failed to load from %s: %v", dataDir, err)
	}
	defer graph.Close()

	stats := graph.GetStatistics()
	if stats.NodeCount != uint64(expectedNodes) {
		graph.Close()
		log.Fatalf("Expected %d nodes, got %d", expectedNodes, stats.NodeCount)
	}

	fmt.Printf("  %s: %d nodes verified\n", dataDir, stats.NodeCount)
}
