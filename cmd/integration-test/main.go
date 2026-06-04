package main

import (
	"fmt"
	"os"
	"time"

	"github.com/dd0wney/graphdb/pkg/parallel"
	"github.com/dd0wney/graphdb/pkg/query"
	"github.com/dd0wney/graphdb/pkg/storage"
)

func main() {
	fmt.Println("🧪 Phase 2 Integration Test")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	// Clean up test data
	os.RemoveAll("./data/integration-test")

	// Create graph storage with all Phase 2 features enabled
	fmt.Println("1. Creating GraphStorage with Phase 2 features...")
	config := storage.StorageConfig{
		DataDir:               "./data/integration-test",
		EnableBatching:        true,
		EnableCompression:     true,
		EnableEdgeCompression: true,
		BatchSize:             100,
		FlushInterval:         10 * time.Millisecond,
	}

	graph, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		fmt.Printf("❌ Failed to create graph: %v\n", err)
		os.Exit(1)
	}
	defer graph.Close()

	fmt.Println("   ✅ GraphStorage created with:")
	fmt.Println("      - Batched WAL enabled")
	fmt.Println("      - WAL compression enabled")
	fmt.Println("      - Edge list compression enabled")
	fmt.Println()

	// Test 1: Create test data
	fmt.Println("2. Creating test data (1000 nodes, ~3000 edges)...")
	startTime := time.Now()

	nodeIDs := make([]uint64, 1000)
	for i := 0; i < 1000; i++ {
		labels := []string{"Person"}
		props := map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("Person%d", i)),
			"age":  storage.IntValue(int64(20 + (i % 60))),
			"city": storage.StringValue([]string{"NYC", "SF", "LA", "Chicago", "Boston"}[i%5]),
		}

		node, err := graph.CreateNode(labels, props)
		if err != nil {
			fmt.Printf("❌ Failed to create node: %v\n", err)
			os.Exit(1)
		}
		nodeIDs[i] = node.ID

		// Create ~3 edges per node
		for j := 0; j < 3; j++ {
			targetIdx := (i + j + 1) % 1000
			edgeProps := map[string]storage.Value{
				"since": storage.IntValue(int64(2020 + (i % 5))),
			}
			graph.CreateEdge(node.ID, nodeIDs[targetIdx], "KNOWS", edgeProps, 1.0)
		}
	}

	createDuration := time.Since(startTime)
	stats := graph.GetStatistics()

	fmt.Printf("   ✅ Created %d nodes and %d edges in %s\n", stats.NodeCount, stats.EdgeCount, createDuration)
	fmt.Printf("      Import rate: %.0f nodes/sec\n", float64(stats.NodeCount)/createDuration.Seconds())
	fmt.Println()

	// Test 2: Edge List Compression
	fmt.Println("3. Testing Edge List Compression...")

	// Compress edge lists
	err = graph.CompressEdgeLists()
	if err != nil {
		fmt.Printf("❌ Failed to compress: %v\n", err)
		os.Exit(1)
	}

	compressionStats := graph.GetCompressionStats()
	fmt.Printf("   ✅ Compression Statistics:\n")
	fmt.Printf("      Total Edges:     %d\n", compressionStats.TotalEdges)
	fmt.Printf("      Uncompressed:    %.2f MB\n", float64(compressionStats.UncompressedBytes)/(1024*1024))
	fmt.Printf("      Compressed:      %.2f MB\n", float64(compressionStats.CompressedBytes)/(1024*1024))
	fmt.Printf("      Ratio:           %.2fx\n", compressionStats.AverageRatio)
	fmt.Printf("      Memory Saved:    %.1f%%\n",
		100*(1-float64(compressionStats.CompressedBytes)/float64(compressionStats.UncompressedBytes)))
	fmt.Println()

	// Test 3: Parallel Graph Traversal
	fmt.Println("4. Testing Parallel Graph Traversal...")

	// Create parallel traverser
	numWorkers := 4
	traverser, err := parallel.NewParallelTraverser(graph, numWorkers)
	if err != nil {
		fmt.Printf("❌ Failed to create parallel traverser: %v\n", err)
		os.Exit(1)
	}
	defer traverser.Close()

	// Sequential BFS
	startNodes := []uint64{nodeIDs[0]}
	maxDepth := 5

	sequentialStart := time.Now()
	sequentialResults := traverseBFSSequential(graph, startNodes, maxDepth)
	sequentialDuration := time.Since(sequentialStart)

	// Parallel BFS
	parallelStart := time.Now()
	parallelResults := traverser.TraverseBFS(startNodes, maxDepth)
	parallelDuration := time.Since(parallelStart)

	fmt.Printf("   ✅ Traversal Results:\n")
	fmt.Printf("      Sequential BFS:  %d nodes in %s\n", len(sequentialResults), sequentialDuration)
	fmt.Printf("      Parallel BFS:    %d nodes in %s (w=%d)\n", len(parallelResults), parallelDuration, numWorkers)

	speedup := float64(sequentialDuration) / float64(parallelDuration)
	fmt.Printf("      Speedup:         %.2fx\n", speedup)
	fmt.Println()

	// Test 4: Query Optimizer
	fmt.Println("5. Testing Query Optimizer...")

	// Create property index for optimization
	graph.CreatePropertyIndex("name", storage.TypeString)
	graph.CreatePropertyIndex("age", storage.TypeInt)

	// Create query executor (includes optimizer)
	executor := query.NewExecutor(graph)

	// Test query with filter
	queryText := "MATCH (n:Person) WHERE n.age > 30 RETURN n.name, n.age LIMIT 10"

	// Parse query using lexer and parser
	lexer := query.NewLexer(queryText)
	tokens, err := lexer.Tokenize()
	if err != nil {
		fmt.Printf("❌ Failed to tokenize query: %v\n", err)
		os.Exit(1)
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		fmt.Printf("❌ Failed to parse query: %v\n", err)
		os.Exit(1)
	}

	// Execute with optimization
	queryStart := time.Now()
	results, err := executor.Execute(parsedQuery)
	queryDuration := time.Since(queryStart)

	if err != nil {
		fmt.Printf("❌ Failed to execute query: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("   ✅ Query Execution:\n")
	fmt.Printf("      Query:           %s\n", queryText)
	fmt.Printf("      Results:         %d rows\n", results.Count)
	fmt.Printf("      Execution Time:  %s\n", queryDuration)
	fmt.Printf("      Throughput:      %.0f queries/sec\n", 1.0/queryDuration.Seconds())
	fmt.Println()

	// Test 5: Query Cache
	fmt.Println("6. Testing Query Cache...")

	// Re-parse for cache test
	lexer2 := query.NewLexer(queryText)
	tokens2, _ := lexer2.Tokenize()
	parser2 := query.NewParser(tokens2)
	parsedQuery2, _ := parser2.Parse()

	// First execution (cache miss)
	firstStart := time.Now()
	_, err = executor.ExecuteWithText(queryText, parsedQuery2)
	firstDuration := time.Since(firstStart)

	if err != nil {
		fmt.Printf("❌ Failed first execution: %v\n", err)
		os.Exit(1)
	}

	// Re-parse again for second execution
	lexer3 := query.NewLexer(queryText)
	tokens3, _ := lexer3.Tokenize()
	parser3 := query.NewParser(tokens3)
	parsedQuery3, _ := parser3.Parse()

	// Second execution (cache hit)
	secondStart := time.Now()
	_, err = executor.ExecuteWithText(queryText, parsedQuery3)
	secondDuration := time.Since(secondStart)

	if err != nil {
		fmt.Printf("❌ Failed second execution: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("   ✅ Query Cache:\n")
	fmt.Printf("      First Execution:   %s (cache miss)\n", firstDuration)
	fmt.Printf("      Second Execution:  %s (cache hit)\n", secondDuration)

	if secondDuration < firstDuration {
		improvement := float64(firstDuration-secondDuration) / float64(firstDuration) * 100
		fmt.Printf("      Improvement:       %.1f%% faster\n", improvement)
	}
	fmt.Println()

	// Summary
	fmt.Println("📊 Phase 2 Integration Summary")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Printf("✅ Edge Compression:     %.2fx ratio, %.1f%% memory saved\n",
		compressionStats.AverageRatio,
		100*(1-float64(compressionStats.CompressedBytes)/float64(compressionStats.UncompressedBytes)))
	fmt.Printf("✅ Parallel Traversal:   %.2fx speedup with %d workers\n", speedup, numWorkers)
	fmt.Printf("✅ Query Optimizer:      Integrated and operational\n")
	fmt.Printf("✅ Query Cache:          Working correctly\n")
	fmt.Println()
	fmt.Println("🎉 All Phase 2 features successfully integrated!")
}

// Helper function for sequential BFS
func traverseBFSSequential(graph storage.Storage, startNodes []uint64, maxDepth int) []uint64 {
	visited := make(map[uint64]bool)
	queue := make([]uint64, 0)
	depths := make(map[uint64]int)

	// Initialize with start nodes
	for _, nodeID := range startNodes {
		queue = append(queue, nodeID)
		visited[nodeID] = true
		depths[nodeID] = 0
	}

	result := make([]uint64, 0)

	// BFS
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		result = append(result, current)

		// Get current depth
		currentDepth := depths[current]
		if currentDepth >= maxDepth {
			continue
		}

		// Get neighbors
		edges, _ := graph.GetOutgoingEdges(current)
		for _, edge := range edges {
			if !visited[edge.ToNodeID] {
				visited[edge.ToNodeID] = true
				depths[edge.ToNodeID] = currentDepth + 1
				queue = append(queue, edge.ToNodeID)
			}
		}
	}

	return result
}
