package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"runtime"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/parallel"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	numNodes := flag.Int("nodes", 1000, "Number of nodes")
	avgDegree := flag.Int("degree", 10, "Average degree per node")
	depth := flag.Int("depth", 5, "Traversal depth")
	numWorkers := flag.Int("workers", 0, "Number of worker goroutines (0 = CPU count)")
	flag.Parse()

	if *numWorkers == 0 {
		*numWorkers = runtime.NumCPU()
	}

	fmt.Printf("🔬 Parallel Graph Traversal Benchmark\n")
	fmt.Printf("======================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes:       %d\n", *numNodes)
	fmt.Printf("  Avg Degree:  %d\n", *avgDegree)
	fmt.Printf("  Depth:       %d\n", *depth)
	fmt.Printf("  CPU Cores:   %d\n", runtime.NumCPU())
	fmt.Printf("  Workers:     %d\n\n", *numWorkers)

	// Create graph
	fmt.Printf("📊 Creating test graph...\n")
	graph := createTestGraph(*numNodes, *avgDegree)
	fmt.Printf("   Created %d nodes with ~%d edges\n\n", *numNodes, *numNodes**avgDegree)

	// Select random start nodes
	numStartNodes := 10
	startNodes := make([]uint64, numStartNodes)
	for i := 0; i < numStartNodes; i++ {
		startNodes[i] = uint64(rand.Intn(*numNodes) + 1)
	}

	// Benchmark 1: Sequential BFS
	fmt.Printf("🐌 Testing Sequential BFS...\n")
	seqStats := benchmarkSequentialBFS(graph, startNodes, *depth)
	fmt.Printf("   Nodes Visited: %d\n", seqStats.NodesVisited)
	fmt.Printf("   Duration:      %s\n", seqStats.Duration)
	fmt.Printf("   Throughput:    %.0f nodes/sec\n\n", seqStats.Throughput)

	// Benchmark 2: Parallel BFS (2 workers)
	fmt.Printf("⚡ Testing Parallel BFS (2 workers)...\n")
	par2Stats := benchmarkParallelBFS(graph, startNodes, *depth, 2)
	fmt.Printf("   Nodes Visited: %d\n", par2Stats.NodesVisited)
	fmt.Printf("   Duration:      %s\n", par2Stats.Duration)
	fmt.Printf("   Throughput:    %.0f nodes/sec\n", par2Stats.Throughput)
	fmt.Printf("   Speedup:       %.2fx\n\n", seqStats.Duration.Seconds()/par2Stats.Duration.Seconds())

	// Benchmark 3: Parallel BFS (4 workers)
	fmt.Printf("⚡⚡ Testing Parallel BFS (4 workers)...\n")
	par4Stats := benchmarkParallelBFS(graph, startNodes, *depth, 4)
	fmt.Printf("   Nodes Visited: %d\n", par4Stats.NodesVisited)
	fmt.Printf("   Duration:      %s\n", par4Stats.Duration)
	fmt.Printf("   Throughput:    %.0f nodes/sec\n", par4Stats.Throughput)
	fmt.Printf("   Speedup:       %.2fx\n\n", seqStats.Duration.Seconds()/par4Stats.Duration.Seconds())

	// Benchmark 4: Parallel BFS (max workers)
	fmt.Printf("🚀 Testing Parallel BFS (%d workers)...\n", *numWorkers)
	parMaxStats := benchmarkParallelBFS(graph, startNodes, *depth, *numWorkers)
	fmt.Printf("   Nodes Visited: %d\n", parMaxStats.NodesVisited)
	fmt.Printf("   Duration:      %s\n", parMaxStats.Duration)
	fmt.Printf("   Throughput:    %.0f nodes/sec\n", parMaxStats.Throughput)
	fmt.Printf("   Speedup:       %.2fx\n\n", seqStats.Duration.Seconds()/parMaxStats.Duration.Seconds())

	// Summary
	fmt.Printf("📊 Summary\n")
	fmt.Printf("======================================\n")
	fmt.Printf("Sequential:    %s (baseline)\n", seqStats.Duration)
	fmt.Printf("Parallel (2):  %s (%.2fx faster)\n", par2Stats.Duration, seqStats.Duration.Seconds()/par2Stats.Duration.Seconds())
	fmt.Printf("Parallel (4):  %s (%.2fx faster)\n", par4Stats.Duration, seqStats.Duration.Seconds()/par4Stats.Duration.Seconds())
	fmt.Printf("Parallel (%d): %s (%.2fx faster)\n", *numWorkers, parMaxStats.Duration, seqStats.Duration.Seconds()/parMaxStats.Duration.Seconds())

	bestSpeedup := seqStats.Duration.Seconds() / parMaxStats.Duration.Seconds()
	fmt.Printf("\n🎯 Best Speedup: %.2fx with %d workers\n", bestSpeedup, *numWorkers)

	if bestSpeedup >= 4.0 {
		fmt.Printf("✅ Excellent! Achieved 4-8x target speedup\n")
	} else if bestSpeedup >= 2.0 {
		fmt.Printf("⚡ Good! Significant parallel speedup\n")
	} else {
		fmt.Printf("💡 Modest speedup - may need larger graphs for better parallelization\n")
	}
}

type BenchmarkStats struct {
	NodesVisited int
	Duration     time.Duration
	Throughput   float64
}

func createTestGraph(numNodes, avgDegree int) *storage.GraphStorage {
	graph, err := storage.NewGraphStorage("./data/benchmark-parallel")
	if err != nil {
		log.Fatalf("Failed to create graph: %v", err)
	}

	// Create nodes
	for i := 1; i <= numNodes; i++ {
		_, err := graph.CreateNode(
			[]string{"TestNode"},
			map[string]storage.Value{
				"id": storage.IntValue(int64(i)),
			},
		)
		if err != nil {
			log.Fatalf("Failed to create node: %v", err)
		}
	}

	// Create edges (random graph)
	numEdges := numNodes * avgDegree
	for i := 0; i < numEdges; i++ {
		fromID := uint64(rand.Intn(numNodes) + 1)
		toID := uint64(rand.Intn(numNodes) + 1)

		if fromID != toID {
			graph.CreateEdge(fromID, toID, "CONNECTS", nil, 1.0)
		}
	}

	return graph
}

func benchmarkSequentialBFS(graph *storage.GraphStorage, startNodes []uint64, maxDepth int) BenchmarkStats {
	visited := make(map[uint64]bool)
	currentLevel := startNodes

	start := time.Now()

	// Mark start nodes as visited
	for _, nodeID := range startNodes {
		visited[nodeID] = true
	}

	// BFS
	for depth := 0; depth < maxDepth && len(currentLevel) > 0; depth++ {
		nextLevel := make([]uint64, 0)

		for _, nodeID := range currentLevel {
			edges, err := graph.GetOutgoingEdges(nodeID)
			if err != nil {
				continue
			}

			for _, edge := range edges {
				if !visited[edge.ToNodeID] {
					visited[edge.ToNodeID] = true
					nextLevel = append(nextLevel, edge.ToNodeID)
				}
			}
		}

		currentLevel = nextLevel
	}

	duration := time.Since(start)
	nodesVisited := len(visited)
	throughput := float64(nodesVisited) / duration.Seconds()

	return BenchmarkStats{
		NodesVisited: nodesVisited,
		Duration:     duration,
		Throughput:   throughput,
	}
}

func benchmarkParallelBFS(graph *storage.GraphStorage, startNodes []uint64, maxDepth, numWorkers int) BenchmarkStats {
	traverser, err := parallel.NewParallelTraverser(graph, numWorkers)
	if err != nil {
		log.Fatalf("Failed to create parallel traverser: %v", err)
	}
	defer traverser.Close()

	start := time.Now()
	results := traverser.TraverseBFS(startNodes, maxDepth)
	duration := time.Since(start)

	nodesVisited := len(results) + len(startNodes) // Results don't include start nodes
	throughput := float64(nodesVisited) / duration.Seconds()

	return BenchmarkStats{
		NodesVisited: nodesVisited,
		Duration:     duration,
		Throughput:   throughput,
	}
}
