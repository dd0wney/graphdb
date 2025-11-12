package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/algorithms"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	dataDir := flag.String("data", "./data/dimacs", "Data directory with imported graph")
	numQueries := flag.Int("queries", 100, "Number of shortest path queries to run")
	maxDepth := flag.Int("max-depth", 100, "Maximum search depth")
	flag.Parse()

	fmt.Printf("🚗 Road Network Benchmark\n")
	fmt.Printf("=========================\n\n")

	// Open graph
	fmt.Printf("📂 Opening graph from %s...\n", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to open graph storage: %v", err)
	}
	defer graph.Close()

	stats := graph.GetStatistics()
	fmt.Printf("   Nodes: %d\n", stats.NodeCount)
	fmt.Printf("   Edges: %d\n\n", stats.EdgeCount)

	if stats.NodeCount == 0 {
		fmt.Printf("❌ No data found. Please import data first:\n")
		fmt.Printf("   ./bin/import-dimacs --graph test_data/USA-road-d.USA.gr --max-nodes 10000 --max-edges 50000\n")
		os.Exit(1)
	}

	// Run shortest path benchmarks
	fmt.Printf("🎯 Running %d shortest path queries...\n\n", *numQueries)
	runShortestPathBenchmark(graph, stats.NodeCount, *numQueries, *maxDepth)

	// Run traversal benchmarks
	fmt.Printf("\n🌐 Running graph traversal benchmarks...\n\n")
	runTraversalBenchmark(graph, stats.NodeCount, *numQueries)

	// Analyze graph structure
	fmt.Printf("\n📊 Analyzing graph structure...\n\n")
	analyzeGraph(graph, stats.NodeCount)
}

func runShortestPathBenchmark(graph *storage.GraphStorage, nodeCount uint64, numQueries, maxDepth int) {
	rand.Seed(time.Now().UnixNano())

	results := struct {
		found     int
		notFound  int
		totalTime time.Duration
		totalHops int
		minHops   int
		maxHops   int
		minTime   time.Duration
		maxTime   time.Duration
	}{
		minHops: 999999,
		minTime: time.Hour,
	}

	fmt.Printf("%-8s %-12s %-12s %-10s %-10s\n", "Query", "From", "To", "Result", "Time")
	fmt.Printf("─────────────────────────────────────────────────────────\n")

	for i := 0; i < numQueries; i++ {
		// Pick random start and end nodes
		startID := uint64(rand.Intn(int(nodeCount)) + 1)
		endID := uint64(rand.Intn(int(nodeCount)) + 1)

		start := time.Now()
		path, err := algorithms.ShortestPath(graph, startID, endID)
		elapsed := time.Since(start)

		if err != nil {
			continue
		}

		if len(path) > 0 {
			results.found++
			results.totalHops += len(path) - 1
			if len(path)-1 < results.minHops {
				results.minHops = len(path) - 1
			}
			if len(path)-1 > results.maxHops {
				results.maxHops = len(path) - 1
			}

			fmt.Printf("#%-7d %-12d %-12d %-3d hops   %s\n",
				i+1, startID, endID, len(path)-1, elapsed)
		} else {
			results.notFound++
			fmt.Printf("#%-7d %-12d %-12d Not found  %s\n",
				i+1, startID, endID, elapsed)
		}

		results.totalTime += elapsed
		if elapsed < results.minTime {
			results.minTime = elapsed
		}
		if elapsed > results.maxTime {
			results.maxTime = elapsed
		}
	}

	fmt.Printf("\n📈 Shortest Path Statistics\n")
	fmt.Printf("───────────────────────────\n")
	fmt.Printf("Total queries:  %d\n", numQueries)
	fmt.Printf("Found:          %d (%.1f%%)\n", results.found, 100.0*float64(results.found)/float64(numQueries))
	fmt.Printf("Not found:      %d (%.1f%%)\n", results.notFound, 100.0*float64(results.notFound)/float64(numQueries))
	fmt.Printf("\nPath length:\n")
	if results.found > 0 {
		fmt.Printf("  Average:      %.1f hops\n", float64(results.totalHops)/float64(results.found))
		fmt.Printf("  Min:          %d hops\n", results.minHops)
		fmt.Printf("  Max:          %d hops\n", results.maxHops)
	}
	fmt.Printf("\nPerformance:\n")
	fmt.Printf("  Average:      %s per query\n", results.totalTime/time.Duration(numQueries))
	fmt.Printf("  Min:          %s\n", results.minTime)
	fmt.Printf("  Max:          %s\n", results.maxTime)
	fmt.Printf("  Throughput:   %.0f queries/sec\n",
		float64(numQueries)/results.totalTime.Seconds())
}

func runTraversalBenchmark(graph *storage.GraphStorage, nodeCount uint64, numQueries int) {
	rand.Seed(time.Now().UnixNano())

	depths := []int{1, 2, 3, 5}
	fmt.Printf("%-8s %-12s %-10s %-15s %-10s\n", "Depth", "Start", "Direction", "Nodes Found", "Time")
	fmt.Printf("─────────────────────────────────────────────────────────────\n")

	for _, depth := range depths {
		for i := 0; i < 5; i++ {
			startID := uint64(rand.Intn(int(nodeCount)) + 1)

			for _, direction := range []string{"outgoing", "incoming"} {
				start := time.Now()

				// Perform BFS traversal manually
				visited := make(map[uint64]bool)
				queue := []uint64{startID}
				levelMap := make(map[uint64]int)
				levelMap[startID] = 0
				visited[startID] = true

				for len(queue) > 0 {
					current := queue[0]
					queue = queue[1:]

					currentLevel := levelMap[current]
					if currentLevel >= depth {
						continue
					}

					var edges []*storage.Edge
					if direction == "outgoing" {
						edges, _ = graph.GetOutgoingEdges(current)
					} else {
						edges, _ = graph.GetIncomingEdges(current)
					}

					for _, edge := range edges {
						var nextID uint64
						if direction == "outgoing" {
							nextID = edge.ToNodeID
						} else {
							nextID = edge.FromNodeID
						}

						if !visited[nextID] {
							visited[nextID] = true
							levelMap[nextID] = currentLevel + 1
							queue = append(queue, nextID)
						}
					}
				}

				elapsed := time.Since(start)

				fmt.Printf("%-8d %-12d %-10s %-15d %s\n",
					depth, startID, direction, len(visited), elapsed)
			}
		}
	}
}

func analyzeGraph(graph *storage.GraphStorage, nodeCount uint64) {
	rand.Seed(time.Now().UnixNano())

	// Sample nodes to analyze degree distribution
	sampleSize := 100
	if int(nodeCount) < sampleSize {
		sampleSize = int(nodeCount)
	}

	degrees := make([]int, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		nodeID := uint64(rand.Intn(int(nodeCount)) + 1)

		outgoing, _ := graph.GetOutgoingEdges(nodeID)
		incoming, _ := graph.GetIncomingEdges(nodeID)

		degree := len(outgoing) + len(incoming)
		degrees = append(degrees, degree)
	}

	// Calculate statistics
	var sum, min, max int
	min = 999999

	for _, d := range degrees {
		sum += d
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}

	avg := float64(sum) / float64(len(degrees))

	fmt.Printf("Degree Distribution (sample of %d nodes):\n", sampleSize)
	fmt.Printf("  Average:  %.1f\n", avg)
	fmt.Printf("  Min:      %d\n", min)
	fmt.Printf("  Max:      %d\n", max)

	// Sample a few nodes with coordinates
	fmt.Printf("\nSample Locations:\n")
	count := 0
	for i := 0; i < int(nodeCount) && count < 5; i++ {
		nodeID := uint64(rand.Intn(int(nodeCount)) + 1)
		node, err := graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		if lat, ok := node.Properties["lat"]; ok {
			if lon, ok := node.Properties["lon"]; ok {
				latFloat, _ := lat.AsFloat()
				lonFloat, _ := lon.AsFloat()
				fmt.Printf("  Node %d: (%.6f, %.6f)\n", nodeID, latFloat, lonFloat)
				count++
			}
		}
	}
}
