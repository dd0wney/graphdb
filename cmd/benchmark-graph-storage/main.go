package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 10000, "Number of nodes")
	edges := flag.Int("edges", 30000, "Number of edges")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - Storage Backend Comparison\n")
	fmt.Printf("============================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Edges: %d\n\n", *edges)

	// Benchmark 1: In-Memory Storage
	fmt.Printf("📊 Benchmark 1: In-Memory Storage\n")
	fmt.Printf("==================================\n")
	benchmarkInMemory(*nodes, *edges)

	fmt.Printf("\n")

	// Benchmark 2: LSM-Backed Storage
	fmt.Printf("📊 Benchmark 2: LSM-Backed Storage\n")
	fmt.Printf("==================================\n")
	benchmarkLSM(*nodes, *edges)

	fmt.Printf("\n✅ Comparison complete!\n")
}

func benchmarkInMemory(nodeCount, edgeCount int) {
	// Clean up
	os.RemoveAll("./data/benchmark-inmemory")

	fmt.Printf("📂 Initializing in-memory storage...\n")
	graph, err := storage.NewGraphStorage("./data/benchmark-inmemory")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Node creation
	fmt.Printf("\n📝 Creating %d nodes...\n", nodeCount)
	start := time.Now()
	nodeIDs := make([]uint64, nodeCount)

	for i := 0; i < nodeCount; i++ {
		node, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"id":   storage.StringValue(fmt.Sprintf("user%d", i)),
				"name": storage.StringValue(fmt.Sprintf("User %d", i)),
				"age":  storage.IntValue(int64(20 + rand.Intn(50))),
			},
		)
		if err != nil {
			log.Fatalf("Failed to create node: %v", err)
		}
		nodeIDs[i] = node.ID

		if (i+1)%1000 == 0 {
			fmt.Printf("  Created %d nodes...\n", i+1)
		}
	}

	duration := time.Since(start)
	fmt.Printf("✅ Created %d nodes in %v\n", nodeCount, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per node\n", float64(duration.Microseconds())/float64(nodeCount))
	fmt.Printf("  🚀 Throughput: %.0f nodes/sec\n", float64(nodeCount)/duration.Seconds())

	// Edge creation
	fmt.Printf("\n🔗 Creating %d edges...\n", edgeCount)
	start = time.Now()

	for i := 0; i < edgeCount; i++ {
		fromIdx := rand.Intn(nodeCount)
		toIdx := rand.Intn(nodeCount)

		if fromIdx == toIdx {
			toIdx = (toIdx + 1) % nodeCount
		}

		_, err := graph.CreateEdge(
			nodeIDs[fromIdx],
			nodeIDs[toIdx],
			"KNOWS",
			map[string]storage.Value{},
			rand.Float64(),
		)
		if err != nil {
			log.Printf("Warning: Failed to create edge: %v", err)
		}

		if (i+1)%1000 == 0 {
			fmt.Printf("  Created %d edges...\n", i+1)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Created %d edges in %v\n", edgeCount, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per edge\n", float64(duration.Microseconds())/float64(edgeCount))
	fmt.Printf("  🚀 Throughput: %.0f edges/sec\n", float64(edgeCount)/duration.Seconds())

	// Node lookup
	fmt.Printf("\n🔍 Random node lookups (1000 queries)...\n")
	start = time.Now()
	found := 0

	for i := 0; i < 1000; i++ {
		randomIdx := rand.Intn(nodeCount)
		if _, err := graph.GetNode(nodeIDs[randomIdx]); err == nil {
			found++
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Completed 1000 lookups in %v\n", duration)
	fmt.Printf("  ✅ Found: %d/1000\n", found)
	fmt.Printf("  ⚡ Average: %.2fμs per lookup\n", float64(duration.Microseconds())/1000.0)

	// Edge traversal
	fmt.Printf("\n🌐 Edge traversals (1000 queries)...\n")
	start = time.Now()
	totalEdges := 0

	for i := 0; i < 1000; i++ {
		randomIdx := rand.Intn(nodeCount)
		edges, err := graph.GetOutgoingEdges(nodeIDs[randomIdx])
		if err == nil {
			totalEdges += len(edges)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Completed 1000 traversals in %v\n", duration)
	fmt.Printf("  📊 Average edges per node: %.1f\n", float64(totalEdges)/1000.0)
	fmt.Printf("  ⚡ Average: %.2fμs per traversal\n", float64(duration.Microseconds())/1000.0)

	// Memory usage
	stats := graph.GetStatistics()
	fmt.Printf("\n📊 Final Statistics:\n")
	fmt.Printf("  Nodes: %d\n", stats.NodeCount)
	fmt.Printf("  Edges: %d\n", stats.EdgeCount)
}

func benchmarkLSM(nodeCount, edgeCount int) {
	// Clean up
	os.RemoveAll("./data/benchmark-lsm-graph")

	fmt.Printf("📂 Initializing LSM-backed storage...\n")
	graph, err := storage.NewLSMGraphStorage("./data/benchmark-lsm-graph")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Node creation
	fmt.Printf("\n📝 Creating %d nodes...\n", nodeCount)
	start := time.Now()
	nodeIDs := make([]uint64, nodeCount)

	for i := 0; i < nodeCount; i++ {
		node, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"id":   storage.StringValue(fmt.Sprintf("user%d", i)),
				"name": storage.StringValue(fmt.Sprintf("User %d", i)),
				"age":  storage.IntValue(int64(20 + rand.Intn(50))),
			},
		)
		if err != nil {
			log.Fatalf("Failed to create node: %v", err)
		}
		nodeIDs[i] = node.ID

		if (i+1)%1000 == 0 {
			fmt.Printf("  Created %d nodes...\n", i+1)
		}
	}

	duration := time.Since(start)
	fmt.Printf("✅ Created %d nodes in %v\n", nodeCount, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per node\n", float64(duration.Microseconds())/float64(nodeCount))
	fmt.Printf("  🚀 Throughput: %.0f nodes/sec\n", float64(nodeCount)/duration.Seconds())

	// Edge creation
	fmt.Printf("\n🔗 Creating %d edges...\n", edgeCount)
	start = time.Now()

	for i := 0; i < edgeCount; i++ {
		fromIdx := rand.Intn(nodeCount)
		toIdx := rand.Intn(nodeCount)

		if fromIdx == toIdx {
			toIdx = (toIdx + 1) % nodeCount
		}

		_, err := graph.CreateEdge(
			nodeIDs[fromIdx],
			nodeIDs[toIdx],
			"KNOWS",
			map[string]storage.Value{},
			rand.Float64(),
		)
		if err != nil {
			log.Printf("Warning: Failed to create edge: %v", err)
		}

		if (i+1)%1000 == 0 {
			fmt.Printf("  Created %d edges...\n", i+1)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Created %d edges in %v\n", edgeCount, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per edge\n", float64(duration.Microseconds())/float64(edgeCount))
	fmt.Printf("  🚀 Throughput: %.0f edges/sec\n", float64(edgeCount)/duration.Seconds())

	// Wait for background flushes
	fmt.Printf("\n⏱️  Waiting for background flushes...\n")
	time.Sleep(3 * time.Second)

	// Node lookup
	fmt.Printf("\n🔍 Random node lookups (1000 queries)...\n")
	start = time.Now()
	found := 0

	for i := 0; i < 1000; i++ {
		randomIdx := rand.Intn(nodeCount)
		if _, err := graph.GetNode(nodeIDs[randomIdx]); err == nil {
			found++
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Completed 1000 lookups in %v\n", duration)
	fmt.Printf("  ✅ Found: %d/1000\n", found)
	fmt.Printf("  ⚡ Average: %.2fμs per lookup\n", float64(duration.Microseconds())/1000.0)

	// Edge traversal
	fmt.Printf("\n🌐 Edge traversals (1000 queries)...\n")
	start = time.Now()
	totalEdges := 0

	for i := 0; i < 1000; i++ {
		randomIdx := rand.Intn(nodeCount)
		edges, err := graph.GetOutgoingEdges(nodeIDs[randomIdx])
		if err == nil {
			totalEdges += len(edges)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Completed 1000 traversals in %v\n", duration)
	fmt.Printf("  📊 Average edges per node: %.1f\n", float64(totalEdges)/1000.0)
	fmt.Printf("  ⚡ Average: %.2fμs per traversal\n", float64(duration.Microseconds())/1000.0)

	// Statistics
	stats := graph.GetStatistics()
	fmt.Printf("\n📊 Final Statistics:\n")
	fmt.Printf("  Nodes: %d\n", stats.NodeCount)
	fmt.Printf("  Edges: %d\n", stats.EdgeCount)
}
