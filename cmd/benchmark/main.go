package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 10000, "Number of nodes to create")
	edges := flag.Int("edges", 30000, "Number of edges to create")
	traversals := flag.Int("traversals", 100, "Number of traversals to test")
	dataDir := flag.String("data", "./data/benchmark", "Data directory")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB Benchmark\n")
	fmt.Printf("==========================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Edges: %d\n", *edges)
	fmt.Printf("  Traversals: %d\n", *traversals)
	fmt.Printf("  Data Directory: %s\n\n", *dataDir)

	// Initialize storage
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Benchmark 1: Node Creation
	fmt.Printf("\n📝 Benchmark 1: Node Creation\n")
	start := time.Now()
	nodeIDs := make([]uint64, *nodes)

	for i := 0; i < *nodes; i++ {
		node, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"id":         storage.StringValue(fmt.Sprintf("user%d", i)),
				"trustScore": storage.IntValue(int64(rand.Intn(1000))),
				"created":    storage.TimestampValue(time.Now()),
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
	fmt.Printf("  ✅ Created %d nodes in %v\n", *nodes, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per node\n", float64(duration.Microseconds())/float64(*nodes))
	fmt.Printf("  🚀 Throughput: %.0f nodes/sec\n", float64(*nodes)/duration.Seconds())

	// Benchmark 2: Edge Creation
	fmt.Printf("\n🔗 Benchmark 2: Edge Creation\n")
	start = time.Now()
	edgeIDs := make([]uint64, *edges)

	for i := 0; i < *edges; i++ {
		fromIdx := rand.Intn(*nodes)
		toIdx := rand.Intn(*nodes)

		// Avoid self-loops
		if fromIdx == toIdx {
			toIdx = (toIdx + 1) % *nodes
		}

		edge, err := graph.CreateEdge(
			nodeIDs[fromIdx],
			nodeIDs[toIdx],
			"CONNECTED_TO",
			map[string]storage.Value{
				"weight": storage.FloatValue(rand.Float64()),
			},
			rand.Float64(),
		)
		if err != nil {
			log.Printf("Warning: Failed to create edge: %v", err)
			continue
		}
		edgeIDs[i] = edge.ID

		if (i+1)%1000 == 0 {
			fmt.Printf("  Created %d edges...\n", i+1)
		}
	}

	duration = time.Since(start)
	fmt.Printf("  ✅ Created %d edges in %v\n", *edges, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per edge\n", float64(duration.Microseconds())/float64(*edges))
	fmt.Printf("  🚀 Throughput: %.0f edges/sec\n", float64(*edges)/duration.Seconds())

	// Benchmark 3: Node Lookups
	fmt.Printf("\n🔍 Benchmark 3: Random Node Lookups (10,000 lookups)\n")
	lookups := 10000
	start = time.Now()

	for i := 0; i < lookups; i++ {
		idx := rand.Intn(*nodes)
		_, err := graph.GetNode(nodeIDs[idx])
		if err != nil {
			log.Fatalf("Failed to get node: %v", err)
		}
	}

	duration = time.Since(start)
	fmt.Printf("  ✅ %d lookups in %v\n", lookups, duration)
	fmt.Printf("  ⚡ Average: %.2fμs per lookup\n", float64(duration.Microseconds())/float64(lookups))
	fmt.Printf("  🚀 Throughput: %.0f lookups/sec\n", float64(lookups)/duration.Seconds())

	// Benchmark 4: Get Outgoing Edges
	fmt.Printf("\n📤 Benchmark 4: Get Outgoing Edges (1,000 queries)\n")
	queries := 1000
	start = time.Now()
	totalEdgesRetrieved := 0

	for i := 0; i < queries; i++ {
		idx := rand.Intn(*nodes)
		edges, err := graph.GetOutgoingEdges(nodeIDs[idx])
		if err != nil {
			log.Fatalf("Failed to get edges: %v", err)
		}
		totalEdgesRetrieved += len(edges)
	}

	duration = time.Since(start)
	avgEdges := float64(totalEdgesRetrieved) / float64(queries)
	fmt.Printf("  ✅ %d queries in %v\n", queries, duration)
	fmt.Printf("  📊 Average edges per node: %.1f\n", avgEdges)
	fmt.Printf("  ⚡ Average: %.2fμs per query\n", float64(duration.Microseconds())/float64(queries))
	fmt.Printf("  🚀 Throughput: %.0f queries/sec\n", float64(queries)/duration.Seconds())

	// Benchmark 5: Graph Traversal (BFS)
	fmt.Printf("\n🌐 Benchmark 5: BFS Traversal (%d traversals, depth 3)\n", *traversals)
	traverser := query.NewTraverser(graph)
	start = time.Now()
	totalNodesVisited := 0

	for i := 0; i < *traversals; i++ {
		idx := rand.Intn(*nodes)
		result, err := traverser.BFS(query.TraversalOptions{
			StartNodeID: nodeIDs[idx],
			Direction:   query.DirectionOutgoing,
			EdgeTypes:   []string{},
			MaxDepth:    3,
			MaxResults:  1000,
		})
		if err != nil {
			log.Fatalf("Failed traversal: %v", err)
		}
		totalNodesVisited += len(result.Nodes)
	}

	duration = time.Since(start)
	avgNodes := float64(totalNodesVisited) / float64(*traversals)
	fmt.Printf("  ✅ %d traversals in %v\n", *traversals, duration)
	fmt.Printf("  📊 Average nodes visited: %.1f\n", avgNodes)
	fmt.Printf("  ⚡ Average: %.2fms per traversal\n", float64(duration.Milliseconds())/float64(*traversals))
	fmt.Printf("  🚀 Throughput: %.0f traversals/sec\n", float64(*traversals)/duration.Seconds())

	// Benchmark 6: Shortest Path
	fmt.Printf("\n🛤️  Benchmark 6: Shortest Path (100 queries)\n")
	pathQueries := 100
	start = time.Now()
	pathsFound := 0
	totalPathLength := 0

	for i := 0; i < pathQueries; i++ {
		fromIdx := rand.Intn(*nodes)
		toIdx := rand.Intn(*nodes)

		path, err := traverser.FindShortestPath(nodeIDs[fromIdx], nodeIDs[toIdx], []string{})
		if err == nil {
			pathsFound++
			totalPathLength += len(path.Edges)
		}
	}

	duration = time.Since(start)
	avgPathLength := 0.0
	if pathsFound > 0 {
		avgPathLength = float64(totalPathLength) / float64(pathsFound)
	}
	fmt.Printf("  ✅ %d path queries in %v\n", pathQueries, duration)
	fmt.Printf("  📊 Paths found: %d/%d (%.1f%%)\n", pathsFound, pathQueries, float64(pathsFound)*100/float64(pathQueries))
	fmt.Printf("  📊 Average path length: %.1f edges\n", avgPathLength)
	fmt.Printf("  ⚡ Average: %.2fms per query\n", float64(duration.Milliseconds())/float64(pathQueries))

	// Benchmark 7: Snapshot Performance
	fmt.Printf("\n💾 Benchmark 7: Snapshot Performance\n")
	start = time.Now()
	err = graph.Snapshot()
	if err != nil {
		log.Fatalf("Failed to snapshot: %v", err)
	}
	duration = time.Since(start)
	fmt.Printf("  ✅ Snapshot created in %v\n", duration)
	fmt.Printf("  📊 Graph size: %d nodes, %d edges\n", *nodes, *edges)

	// Final Statistics
	fmt.Printf("\n📊 Final Statistics\n")
	stats := graph.GetStatistics()
	fmt.Printf("  Total Nodes: %d\n", stats.NodeCount)
	fmt.Printf("  Total Edges: %d\n", stats.EdgeCount)
	fmt.Printf("  Avg Edges per Node: %.2f\n", float64(stats.EdgeCount)/float64(stats.NodeCount))

	// Memory estimation (rough)
	avgNodeSize := 200 // bytes (estimate)
	avgEdgeSize := 150 // bytes (estimate)
	estimatedMemory := (stats.NodeCount * uint64(avgNodeSize)) + (stats.EdgeCount * uint64(avgEdgeSize))
	fmt.Printf("  Estimated Memory: %.2f MB\n", float64(estimatedMemory)/(1024*1024))

	fmt.Printf("\n✅ Benchmark complete!\n")
}
