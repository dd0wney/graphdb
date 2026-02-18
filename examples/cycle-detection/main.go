package main

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	// Create a temporary graph database
	graph, err := storage.NewGraphStorage("./data/cycle_demo")
	if err != nil {
		log.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	fmt.Println("=== Cycle Detection Demo ===")

	// Example 1: Network topology with a routing loop
	fmt.Println("Creating network topology with routing loop...")
	router1, _ := graph.CreateNode([]string{"Router"}, map[string]storage.Value{
		"name":     storage.StringValue("CoreRouter1"),
		"priority": storage.IntValue(1),
	})
	router2, _ := graph.CreateNode([]string{"Router"}, map[string]storage.Value{
		"name":     storage.StringValue("CoreRouter2"),
		"priority": storage.IntValue(1),
	})
	router3, _ := graph.CreateNode([]string{"Router"}, map[string]storage.Value{
		"name":     storage.StringValue("EdgeRouter1"),
		"priority": storage.IntValue(2),
	})
	router4, _ := graph.CreateNode([]string{"Router"}, map[string]storage.Value{
		"name":     storage.StringValue("EdgeRouter2"),
		"priority": storage.IntValue(2),
	})

	// Create edges forming a cycle: R1 -> R2 -> R3 -> R4 -> R1
	graph.CreateEdge(router1.ID, router2.ID, "ROUTES_TO", map[string]storage.Value{
		"bandwidth": storage.IntValue(10000),
	}, 1.0)
	graph.CreateEdge(router2.ID, router3.ID, "ROUTES_TO", map[string]storage.Value{
		"bandwidth": storage.IntValue(5000),
	}, 1.0)
	graph.CreateEdge(router3.ID, router4.ID, "ROUTES_TO", map[string]storage.Value{
		"bandwidth": storage.IntValue(5000),
	}, 1.0)
	graph.CreateEdge(router4.ID, router1.ID, "ROUTES_TO", map[string]storage.Value{
		"bandwidth": storage.IntValue(10000),
	}, 1.0) // This creates the cycle!

	// Add a server without cycles
	server, _ := graph.CreateNode([]string{"Server"}, map[string]storage.Value{
		"name": storage.StringValue("AppServer1"),
	})
	graph.CreateEdge(router3.ID, server.ID, "ROUTES_TO", nil, 1.0)

	// Check if graph has cycles (fast check)
	fmt.Println("\n1. Quick cycle check...")
	hasCycle, _ := algorithms.HasCycle(graph)
	fmt.Printf("   Graph has cycles: %v\n", hasCycle)

	// Detect all cycles
	fmt.Println("\n2. Detecting all cycles...")
	cycles, _ := algorithms.DetectCycles(graph)
	fmt.Printf("   Found %d cycle(s)\n", len(cycles))

	for i, cycle := range cycles {
		fmt.Printf("\n   Cycle %d (length %d):\n", i+1, len(cycle))
		for j, nodeID := range cycle {
			node, _ := graph.GetNode(nodeID)
			name, _ := node.GetProperty("name")
			nameStr, _ := name.AsString()
			if j < len(cycle)-1 {
				fmt.Printf("     %s ->", nameStr)
			} else {
				fmt.Printf(" %s (back to start)\n", nameStr)
			}
		}
	}

	// Analyze cycle statistics
	fmt.Println("\n3. Cycle statistics...")
	stats := algorithms.AnalyzeCycles(cycles)
	fmt.Printf("   Total cycles:   %d\n", stats.TotalCycles)
	fmt.Printf("   Shortest cycle: %d nodes\n", stats.ShortestCycle)
	fmt.Printf("   Longest cycle:  %d nodes\n", stats.LongestCycle)
	fmt.Printf("   Average length: %.2f nodes\n", stats.AverageLength)
	fmt.Printf("   Self-loops:     %d\n", stats.SelfLoops)

	// Filter cycles by properties
	fmt.Println("\n4. Filtering cycles (only Router nodes)...")
	opts := algorithms.CycleDetectionOptions{
		NodePredicate: func(n *storage.Node) bool {
			return n.HasLabel("Router")
		},
		MinCycleLength: 3, // Only cycles with 3+ nodes
	}
	filteredCycles, _ := algorithms.DetectCyclesWithOptions(graph, opts)
	fmt.Printf("   Found %d cycle(s) matching criteria\n", len(filteredCycles))

	// Example 2: Add a self-loop for demonstration
	fmt.Println("\n5. Adding self-referencing router...")
	router5, _ := graph.CreateNode([]string{"Router"}, map[string]storage.Value{
		"name": storage.StringValue("MisconfiguredRouter"),
	})
	graph.CreateEdge(router5.ID, router5.ID, "ROUTES_TO", nil, 1.0) // Self-loop

	cycles, _ = algorithms.DetectCycles(graph)
	stats = algorithms.AnalyzeCycles(cycles)
	fmt.Printf("   Total cycles now: %d\n", stats.TotalCycles)
	fmt.Printf("   Self-loops: %d\n", stats.SelfLoops)

	fmt.Println("\n=== Demo Complete ===")
}
