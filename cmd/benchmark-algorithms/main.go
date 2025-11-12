package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 1000, "Number of nodes to create")
	edges := flag.Int("edges", 3000, "Number of edges to create")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - Graph Algorithms Benchmark\n")
	fmt.Printf("============================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Edges: %d\n\n", *edges)

	// Clean up old data
	os.RemoveAll("./data/benchmark-algorithms")

	// Initialize storage
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorage("./data/benchmark-algorithms")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create nodes
	fmt.Printf("\n📝 Creating %d nodes...\n", *nodes)
	start := time.Now()

	nodeIDs := make([]uint64, *nodes)
	for i := 0; i < *nodes; i++ {
		node, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"id":         storage.StringValue(fmt.Sprintf("user%d", i)),
				"trustScore": storage.IntValue(int64(rand.Intn(1000))),
			},
		)
		if err != nil {
			graph.Close()
			log.Fatalf("Failed to create node: %v", err)
		}
		nodeIDs[i] = node.ID
	}

	duration := time.Since(start)
	fmt.Printf("✅ Created %d nodes in %v\n", *nodes, duration)

	// Create edges (random graph)
	fmt.Printf("\n🔗 Creating %d edges...\n", *edges)
	start = time.Now()

	for i := 0; i < *edges; i++ {
		fromIdx := rand.Intn(*nodes)
		toIdx := rand.Intn(*nodes)

		if fromIdx == toIdx {
			toIdx = (toIdx + 1) % *nodes
		}

		_, err := graph.CreateEdge(
			nodeIDs[fromIdx],
			nodeIDs[toIdx],
			"CONNECTED_TO",
			map[string]storage.Value{},
			rand.Float64(),
		)
		if err != nil {
			log.Printf("Warning: Failed to create edge: %v", err)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ Created %d edges in %v\n", *edges, duration)

	// Benchmark 1: PageRank
	fmt.Printf("\n📊 Benchmark 1: PageRank\n")
	start = time.Now()

	prOpts := algorithms.DefaultPageRankOptions()
	prResult, err := algorithms.PageRank(graph, prOpts)
	if err != nil {
		log.Fatalf("PageRank failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ PageRank completed in %v\n", duration)
	fmt.Printf("  Iterations: %d\n", prResult.Iterations)
	fmt.Printf("  Converged: %v\n", prResult.Converged)
	fmt.Printf("  Top 5 nodes by PageRank:\n")
	for i, node := range prResult.GetTopNodesByPageRank(5) {
		fmt.Printf("    %d. Node %d (score: %.6f)\n", i+1, node.NodeID, node.Score)
	}

	// Benchmark 2: Betweenness Centrality
	fmt.Printf("\n📊 Benchmark 2: Betweenness Centrality\n")
	start = time.Now()

	betweenness, err := algorithms.BetweennessCentrality(graph)
	if err != nil {
		log.Fatalf("Betweenness Centrality failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Betweenness Centrality completed in %v\n", duration)

	// Find top nodes by betweenness
	topBetweenness := findTopNFromMap(betweenness, 5)
	fmt.Printf("  Top 5 nodes by Betweenness:\n")
	for i, item := range topBetweenness {
		fmt.Printf("    %d. Node %d (score: %.6f)\n", i+1, item.nodeID, item.score)
	}

	// Benchmark 3: Degree Centrality
	fmt.Printf("\n📊 Benchmark 3: Degree Centrality\n")
	start = time.Now()

	degree, err := algorithms.DegreeCentrality(graph)
	if err != nil {
		log.Fatalf("Degree Centrality failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Degree Centrality completed in %v\n", duration)

	topDegree := findTopNFromMap(degree, 5)
	fmt.Printf("  Top 5 nodes by Degree:\n")
	for i, item := range topDegree {
		fmt.Printf("    %d. Node %d (score: %.6f)\n", i+1, item.nodeID, item.score)
	}

	// Benchmark 4: Clustering Coefficient
	fmt.Printf("\n📊 Benchmark 4: Clustering Coefficient\n")
	start = time.Now()

	avgCluster, err := algorithms.AverageClusteringCoefficient(graph)
	if err != nil {
		log.Fatalf("Clustering Coefficient failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Clustering Coefficient completed in %v\n", duration)
	fmt.Printf("  Average Clustering Coefficient: %.6f\n", avgCluster)

	// Benchmark 5: Connected Components
	fmt.Printf("\n📊 Benchmark 5: Connected Components\n")
	start = time.Now()

	components, err := algorithms.ConnectedComponents(graph)
	if err != nil {
		log.Fatalf("Connected Components failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Connected Components completed in %v\n", duration)
	fmt.Printf("  Number of components: %d\n", len(components.Communities))
	fmt.Printf("  Largest component size: %d nodes\n", findLargestComponent(components))

	// Benchmark 6: Label Propagation (Community Detection)
	fmt.Printf("\n📊 Benchmark 6: Label Propagation (Community Detection)\n")
	start = time.Now()

	labelProp, err := algorithms.LabelPropagation(graph, 100)
	if err != nil {
		log.Fatalf("Label Propagation failed: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Label Propagation completed in %v\n", duration)
	fmt.Printf("  Number of communities: %d\n", len(labelProp.Communities))
	fmt.Printf("  Largest community size: %d nodes\n", findLargestComponent(labelProp))

	// Summary
	fmt.Printf("\n🎯 Algorithm Summary\n")
	fmt.Printf("==================\n")
	fmt.Printf("Graph with %d nodes and %d edges:\n", *nodes, *edges)
	fmt.Printf("  PageRank: Fast convergence in %d iterations\n", prResult.Iterations)
	fmt.Printf("  Betweenness: Identifies bridge nodes\n")
	fmt.Printf("  Degree: Most connected nodes\n")
	fmt.Printf("  Clustering: %.2f%% local connectivity\n", avgCluster*100)
	fmt.Printf("  Components: %d separate subgraphs\n", len(components.Communities))
	fmt.Printf("  Communities: %d detected via label propagation\n", len(labelProp.Communities))

	fmt.Printf("\n✅ Benchmark complete!\n")
}

type scoreItem struct {
	nodeID uint64
	score  float64
}

func findTopNFromMap(scores map[uint64]float64, n int) []scoreItem {
	items := make([]scoreItem, 0, len(scores))
	for nodeID, score := range scores {
		items = append(items, scoreItem{nodeID, score})
	}

	// Simple selection sort for top N
	if n > len(items) {
		n = len(items)
	}

	for i := 0; i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[maxIdx].score {
				maxIdx = j
			}
		}
		items[i], items[maxIdx] = items[maxIdx], items[i]
	}

	return items[:n]
}

func findLargestComponent(result *algorithms.CommunityDetectionResult) int {
	maxSize := 0
	for _, community := range result.Communities {
		if community.Size > maxSize {
			maxSize = community.Size
		}
	}
	return maxSize
}
