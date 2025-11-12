package main

import (
	"fmt"
	"log"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/query"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	fmt.Println("🚀 Cluso GraphDB - Starting...")

	// Create graph storage
	graph, err := storage.NewGraphStorage("./data/graphdb")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	fmt.Println("✅ Graph storage initialized")

	// Example: Build a trust network
	fmt.Println("\n📊 Building trust network example...")

	// Create users
	alice, _ := graph.CreateNode(
		[]string{"User", "Verified"},
		map[string]storage.Value{
			"id":         storage.StringValue("alice"),
			"trustScore": storage.IntValue(850),
			"active":     storage.BoolValue(true),
		},
	)
	fmt.Printf("  Created node: Alice (ID: %d, Trust: 850)\n", alice.ID)

	bob, _ := graph.CreateNode(
		[]string{"User"},
		map[string]storage.Value{
			"id":         storage.StringValue("bob"),
			"trustScore": storage.IntValue(750),
			"active":     storage.BoolValue(true),
		},
	)
	fmt.Printf("  Created node: Bob (ID: %d, Trust: 750)\n", bob.ID)

	charlie, _ := graph.CreateNode(
		[]string{"User", "Verified"},
		map[string]storage.Value{
			"id":         storage.StringValue("charlie"),
			"trustScore": storage.IntValue(920),
			"active":     storage.BoolValue(true),
		},
	)
	fmt.Printf("  Created node: Charlie (ID: %d, Trust: 920)\n", charlie.ID)

	david, _ := graph.CreateNode(
		[]string{"User"},
		map[string]storage.Value{
			"id":         storage.StringValue("david"),
			"trustScore": storage.IntValue(650),
			"active":     storage.BoolValue(true),
		},
	)
	fmt.Printf("  Created node: David (ID: %d, Trust: 650)\n", david.ID)

	// Create relationships
	fmt.Println("\n🔗 Creating relationships...")

	graph.CreateEdge(alice.ID, bob.ID, "VERIFIED_BY", map[string]storage.Value{
		"timestamp":  storage.TimestampValue(time.Now()),
		"confidence": storage.FloatValue(0.95),
	}, 1.0)
	fmt.Println("  Alice → Bob (VERIFIED_BY)")

	graph.CreateEdge(bob.ID, charlie.ID, "VERIFIED_BY", map[string]storage.Value{
		"timestamp":  storage.TimestampValue(time.Now()),
		"confidence": storage.FloatValue(0.88),
	}, 1.0)
	fmt.Println("  Bob → Charlie (VERIFIED_BY)")

	graph.CreateEdge(alice.ID, charlie.ID, "FOLLOWS", map[string]storage.Value{
		"timestamp": storage.TimestampValue(time.Now()),
	}, 1.0)
	fmt.Println("  Alice → Charlie (FOLLOWS)")

	graph.CreateEdge(charlie.ID, david.ID, "SIMILAR_BEHAVIOR", map[string]storage.Value{
		"similarity": storage.FloatValue(0.72),
	}, 0.72)
	fmt.Println("  Charlie → David (SIMILAR_BEHAVIOR)")

	// Query: Find all verified users
	fmt.Println("\n🔍 Query: Find all verified users...")
	verifiedUsers, _ := graph.FindNodesByLabel("Verified")
	for _, user := range verifiedUsers {
		id, _ := user.GetProperty("id")
		idStr, _ := id.AsString()
		trustScore, _ := user.GetProperty("trustScore")
		score, _ := trustScore.AsInt()
		fmt.Printf("  - %s (Trust: %d)\n", idStr, score)
	}

	// Query: Find users with high trust scores
	fmt.Println("\n🔍 Query: Find users with trust score > 800...")
	allUsers, _ := graph.FindNodesByLabel("User")
	for _, user := range allUsers {
		trustScore, _ := user.GetProperty("trustScore")
		score, _ := trustScore.AsInt()
		if score > 800 {
			id, _ := user.GetProperty("id")
			idStr, _ := id.AsString()
			fmt.Printf("  - %s (Trust: %d)\n", idStr, score)
		}
	}

	// Traversal: Find trust network
	fmt.Println("\n🌐 Traversal: Find Alice's trust network (2 hops)...")
	traverser := query.NewTraverser(graph)
	network, _ := traverser.BFS(query.TraversalOptions{
		StartNodeID: alice.ID,
		Direction:   query.DirectionOutgoing,
		EdgeTypes:   []string{"VERIFIED_BY", "FOLLOWS"},
		MaxDepth:    2,
		MaxResults:  100,
	})
	for _, node := range network.Nodes {
		id, _ := node.GetProperty("id")
		idStr, _ := id.AsString()
		fmt.Printf("  - %s\n", idStr)
	}

	// Find shortest path
	fmt.Println("\n🛤️  Find shortest path: Alice → David...")
	path, err := traverser.FindShortestPath(alice.ID, david.ID, []string{})
	if err == nil {
		fmt.Printf("  Path length: %d edges\n", len(path.Edges))
		for i, node := range path.Nodes {
			id, _ := node.GetProperty("id")
			idStr, _ := id.AsString()
			fmt.Printf("  %d. %s", i+1, idStr)
			if i < len(path.Edges) {
				fmt.Printf(" -[%s]-> ", path.Edges[i].Type)
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("  No path found: %v\n", err)
	}

	// Statistics
	fmt.Println("\n📈 Database statistics:")
	stats := graph.GetStatistics()
	fmt.Printf("  Nodes: %d\n", stats.NodeCount)
	fmt.Printf("  Edges: %d\n", stats.EdgeCount)

	// Save snapshot
	fmt.Println("\n💾 Saving snapshot...")
	if err := graph.Snapshot(); err != nil {
		log.Printf("Failed to save snapshot: %v", err)
	} else {
		fmt.Println("  ✅ Snapshot saved successfully")
	}

	fmt.Println("\n✨ Demo complete!")
}
