package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/query"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 1000, "Number of nodes")
	edges := flag.Int("edges", 3000, "Number of edges")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - Query Language Benchmark\n")
	fmt.Printf("============================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Edges: %d\n\n", *edges)

	// Create test graph
	os.RemoveAll("./data/benchmark-query")
	fmt.Printf("📂 Creating test graph...\n")
	graph, nodeIDs := createTestGraph(*nodes, *edges)
	defer graph.Close()

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Benchmark 1: Simple node matching
	fmt.Printf("📊 Benchmark 1: Find All Users\n")
	fmt.Printf("==============================\n")
	benchmarkFindAllUsers(graph)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Benchmark 2: Property filtering
	fmt.Printf("📊 Benchmark 2: Filter by Property\n")
	fmt.Printf("==================================\n")
	benchmarkPropertyFilter(graph)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Benchmark 3: Relationship traversal
	fmt.Printf("📊 Benchmark 3: Find Friends\n")
	fmt.Printf("============================\n")
	benchmarkFindFriends(graph, nodeIDs)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Benchmark 4: Create nodes
	fmt.Printf("📊 Benchmark 4: Create Nodes\n")
	fmt.Printf("============================\n")
	benchmarkCreateNodes(graph)

	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	// Benchmark 5: Complex pattern matching
	fmt.Printf("📊 Benchmark 5: Complex Pattern (Friends of Friends)\n")
	fmt.Printf("===================================================\n")
	benchmarkFriendsOfFriends(graph, nodeIDs)

	fmt.Printf("\n✅ Benchmark complete!\n")
}

func createTestGraph(nodeCount, edgeCount int) (*storage.GraphStorage, []uint64) {
	graph, err := storage.NewGraphStorage("./data/benchmark-query")
	if err != nil {
		log.Fatalf("Failed to create graph: %v", err)
	}

	// Create nodes with varying properties
	nodeIDs := make([]uint64, nodeCount)
	for i := 0; i < nodeCount; i++ {
		node, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"id":    storage.StringValue(fmt.Sprintf("user%d", i)),
				"name":  storage.StringValue(fmt.Sprintf("User %d", i)),
				"age":   storage.IntValue(int64(20 + rand.Intn(50))),
				"score": storage.IntValue(int64(rand.Intn(1000))),
			},
		)
		if err != nil {
			log.Fatalf("Failed to create node: %v", err)
		}
		nodeIDs[i] = node.ID
	}

	// Create edges (friendships)
	for i := 0; i < edgeCount; i++ {
		fromIdx := rand.Intn(nodeCount)
		toIdx := rand.Intn(nodeCount)
		if fromIdx == toIdx {
			toIdx = (toIdx + 1) % nodeCount
		}

		graph.CreateEdge(
			nodeIDs[fromIdx],
			nodeIDs[toIdx],
			"KNOWS",
			map[string]storage.Value{
				"since": storage.IntValue(int64(2010 + rand.Intn(14))),
			},
			rand.Float64(),
		)
	}

	fmt.Printf("✅ Created %d nodes and %d edges\n", nodeCount, edgeCount)
	return graph, nodeIDs
}

func benchmarkFindAllUsers(graph *storage.GraphStorage) {
	// Procedural approach
	fmt.Printf("🔹 Procedural: Iterate all nodes...\n")
	start := time.Now()

	proceduralCount := 0
	stats := graph.GetStatistics()
	for nodeID := uint64(1); nodeID <= stats.NodeCount; nodeID++ {
		node, err := graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		// Check if User label
		for _, label := range node.Labels {
			if label == "User" {
				proceduralCount++
				break
			}
		}
	}

	proceduralTime := time.Since(start)
	fmt.Printf("  ✅ Found %d users in %v\n", proceduralCount, proceduralTime)

	// Declarative approach (query language)
	fmt.Printf("🔹 Declarative: MATCH (u:User) RETURN u\n")
	start = time.Now()

	queryStr := "MATCH (u:User) RETURN u"
	executor := query.NewExecutor(graph)

	// Parse query
	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		log.Printf("Lexer error: %v", err)
		return
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		log.Printf("Parser error: %v", err)
		return
	}

	// Execute query
	results, err := executor.Execute(parsedQuery)
	if err != nil {
		log.Printf("Execution error: %v", err)
		return
	}

	declarativeTime := time.Since(start)
	fmt.Printf("  ✅ Found %d users in %v\n", results.Count, declarativeTime)

	// Compare
	ratio := float64(proceduralTime) / float64(declarativeTime)
	fmt.Printf("\n  📊 Performance: %.2fx (declarative overhead)\n", ratio)
	fmt.Printf("  ✨ Declarative code: 1 line vs ~15 lines procedural\n")
}

func benchmarkPropertyFilter(graph *storage.GraphStorage) {
	// Procedural approach
	fmt.Printf("🔹 Procedural: Filter age > 30...\n")
	start := time.Now()

	proceduralCount := 0
	stats := graph.GetStatistics()
	for nodeID := uint64(1); nodeID <= stats.NodeCount; nodeID++ {
		node, err := graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		if age, exists := node.Properties["age"]; exists {
			if ageVal, _ := age.AsInt(); ageVal > 30 {
				proceduralCount++
			}
		}
	}

	proceduralTime := time.Since(start)
	fmt.Printf("  ✅ Found %d users in %v\n", proceduralCount, proceduralTime)

	// Declarative approach
	fmt.Printf("🔹 Declarative: MATCH (u:User) WHERE u.age > 30 RETURN u\n")
	start = time.Now()

	queryStr := "MATCH (u:User) WHERE u.age > 30 RETURN u"
	executor := query.NewExecutor(graph)

	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		log.Printf("Lexer error: %v", err)
		return
	}
	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		log.Printf("Parser error: %v", err)
		return
	}
	results, err := executor.Execute(parsedQuery)
	if err != nil {
		log.Printf("Execution error: %v", err)
		return
	}

	declarativeTime := time.Since(start)
	fmt.Printf("  ✅ Found %d users in %v\n", results.Count, declarativeTime)

	ratio := float64(proceduralTime) / float64(declarativeTime)
	fmt.Printf("\n  📊 Performance: %.2fx (declarative overhead)\n", ratio)
	fmt.Printf("  ✨ Declarative code: 1 line vs ~20 lines procedural\n")
}

func benchmarkFindFriends(graph *storage.GraphStorage, nodeIDs []uint64) {
	targetID := nodeIDs[rand.Intn(len(nodeIDs))]

	// Procedural approach
	fmt.Printf("🔹 Procedural: Find friends of user %d...\n", targetID)
	start := time.Now()

	proceduralFriends := make([]*storage.Node, 0)
	edges, err := graph.GetOutgoingEdges(targetID)
	if err == nil {
		for _, edge := range edges {
			if edge.Type == "KNOWS" {
				if friend, err := graph.GetNode(edge.ToNodeID); err == nil {
					proceduralFriends = append(proceduralFriends, friend)
				}
			}
		}
	}

	proceduralTime := time.Since(start)
	fmt.Printf("  ✅ Found %d friends in %v\n", len(proceduralFriends), proceduralTime)

	// Declarative approach
	fmt.Printf("🔹 Declarative: MATCH (u:User)-[:KNOWS]->(friend:User) RETURN friend.id\n")
	start = time.Now()

	queryStr := "MATCH (u:User)-[:KNOWS]->(friend:User) RETURN friend.id"
	executor := query.NewExecutor(graph)

	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		log.Printf("Lexer error: %v", err)
		return
	}
	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		log.Printf("Parser error: %v", err)
		return
	}
	results, err := executor.Execute(parsedQuery)
	if err != nil {
		log.Printf("Execution error: %v", err)
		return
	}

	declarativeTime := time.Since(start)
	fmt.Printf("  ✅ Found %d friends in %v\n", results.Count, declarativeTime)

	ratio := float64(proceduralTime) / float64(declarativeTime)
	fmt.Printf("\n  📊 Performance: %.2fx (declarative overhead)\n", ratio)
	fmt.Printf("  ✨ Declarative code: 1 line vs ~25 lines procedural\n")
}

func benchmarkCreateNodes(graph *storage.GraphStorage) {
	// Procedural approach
	fmt.Printf("🔹 Procedural: Create 100 nodes...\n")
	start := time.Now()

	proceduralCount := 0
	for i := 0; i < 100; i++ {
		_, err := graph.CreateNode(
			[]string{"TestNode"},
			map[string]storage.Value{
				"name": storage.StringValue(fmt.Sprintf("test%d", i)),
			},
		)
		if err == nil {
			proceduralCount++
		}
	}

	proceduralTime := time.Since(start)
	fmt.Printf("  ✅ Created %d nodes in %v\n", proceduralCount, proceduralTime)

	// Declarative approach
	fmt.Printf("🔹 Declarative: CREATE (n:TestNode {name: 'testX'}) [100 times]\n")
	start = time.Now()

	declarativeCount := 0
	executor := query.NewExecutor(graph)

	for i := 0; i < 100; i++ {
		queryStr := fmt.Sprintf("CREATE (n:TestNode {name: 'test%d'})", i+100)

		lexer := query.NewLexer(queryStr)
		tokens, _ := lexer.Tokenize()
		parser := query.NewParser(tokens)
		parsedQuery, _ := parser.Parse()
		_, err := executor.Execute(parsedQuery)

		if err == nil {
			declarativeCount++
		}
	}

	declarativeTime := time.Since(start)
	fmt.Printf("  ✅ Created %d nodes in %v\n", declarativeCount, declarativeTime)

	ratio := float64(proceduralTime) / float64(declarativeTime)
	fmt.Printf("\n  📊 Performance: %.2fx (declarative overhead)\n", ratio)
	fmt.Printf("  ✨ Declarative code: cleaner syntax, database-independent\n")
}

func benchmarkFriendsOfFriends(graph *storage.GraphStorage, nodeIDs []uint64) {
	targetID := nodeIDs[rand.Intn(len(nodeIDs))]

	// Procedural approach
	fmt.Printf("🔹 Procedural: Find friends-of-friends...\n")
	start := time.Now()

	fofMap := make(map[uint64]*storage.Node)

	// Get direct friends
	edges1, _ := graph.GetOutgoingEdges(targetID)
	for _, edge1 := range edges1 {
		if edge1.Type == "KNOWS" {
			// Get friends of this friend
			edges2, _ := graph.GetOutgoingEdges(edge1.ToNodeID)
			for _, edge2 := range edges2 {
				if edge2.Type == "KNOWS" && edge2.ToNodeID != targetID {
					if fof, err := graph.GetNode(edge2.ToNodeID); err == nil {
						fofMap[fof.ID] = fof
					}
				}
			}
		}
	}

	proceduralTime := time.Since(start)
	fmt.Printf("  ✅ Found %d friends-of-friends in %v\n", len(fofMap), proceduralTime)

	// Declarative approach
	fmt.Printf("🔹 Declarative: MATCH (u:User)-[:KNOWS]->(f:User)-[:KNOWS]->(fof:User) RETURN fof.id\n")
	start = time.Now()

	queryStr := "MATCH (u:User)-[:KNOWS]->(f:User)-[:KNOWS]->(fof:User) RETURN fof.id"
	executor := query.NewExecutor(graph)

	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		log.Printf("Lexer error: %v", err)
		return
	}
	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		log.Printf("Parser error: %v", err)
		return
	}
	results, err := executor.Execute(parsedQuery)
	if err != nil {
		log.Printf("Execution error: %v", err)
		return
	}

	declarativeTime := time.Since(start)
	fmt.Printf("  ✅ Found %d friends-of-friends in %v\n", results.Count, declarativeTime)

	ratio := float64(proceduralTime) / float64(declarativeTime)
	fmt.Printf("\n  📊 Performance: %.2fx (declarative overhead)\n", ratio)
	fmt.Printf("  ✨ Declarative code: 1 line vs ~40 lines procedural\n")
	fmt.Printf("  ✨ Much easier to understand and maintain!\n")
}
