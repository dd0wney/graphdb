package main

import (
	"flag"
	"fmt"
	"math/rand"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	numNodes := flag.Int("nodes", 10000, "Number of nodes")
	avgDegree := flag.Int("degree", 20, "Average degree per node")
	flag.Parse()

	fmt.Printf("📦 Edge List Compression Benchmark\n")
	fmt.Printf("==================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes:       %d\n", *numNodes)
	fmt.Printf("  Avg Degree:  %d\n", *avgDegree)
	fmt.Printf("  Total Edges: ~%d\n\n", *numNodes**avgDegree)

	// Generate random edge lists
	fmt.Printf("🔨 Generating random edge lists...\n")
	edgeLists := generateEdgeLists(*numNodes, *avgDegree)
	fmt.Printf("   Generated %d edge lists\n\n", len(edgeLists))

	// Benchmark 1: Compression ratio
	fmt.Printf("📊 Testing Compression Ratio...\n")
	compressedLists, compressionTime := compressEdgeLists(edgeLists)
	stats := storage.CalculateCompressionStats(compressedLists)

	fmt.Printf("   Total Edges:         %d\n", stats.TotalEdges)
	fmt.Printf("   Uncompressed Size:   %d bytes (%.2f MB)\n",
		stats.UncompressedBytes, float64(stats.UncompressedBytes)/(1024*1024))
	fmt.Printf("   Compressed Size:     %d bytes (%.2f MB)\n",
		stats.CompressedBytes, float64(stats.CompressedBytes)/(1024*1024))
	fmt.Printf("   Average Ratio:       %.2fx\n", stats.AverageRatio)
	fmt.Printf("   Memory Saved:        %.1f%%\n",
		100*(1-float64(stats.CompressedBytes)/float64(stats.UncompressedBytes)))
	fmt.Printf("   Compression Time:    %s\n\n", compressionTime)

	// Benchmark 2: Decompression speed
	fmt.Printf("⚡ Testing Decompression Speed...\n")
	decompressionTime, edgesDecompressed := benchmarkDecompression(compressedLists)
	fmt.Printf("   Edges Decompressed:  %d\n", edgesDecompressed)
	fmt.Printf("   Decompression Time:  %s\n", decompressionTime)
	fmt.Printf("   Throughput:          %.0f edges/sec\n\n",
		float64(edgesDecompressed)/decompressionTime.Seconds())

	// Benchmark 3: Random access (Contains operation)
	fmt.Printf("🔍 Testing Random Access (Contains)...\n")
	accessTime, accessCount := benchmarkRandomAccess(compressedLists, *numNodes)
	fmt.Printf("   Lookups Performed:   %d\n", accessCount)
	fmt.Printf("   Access Time:         %s\n", accessTime)
	fmt.Printf("   Avg Lookup Time:     %s\n\n", accessTime/time.Duration(accessCount))

	// Benchmark 4: Memory comparison
	fmt.Printf("💾 Memory Usage Comparison...\n")
	uncompressedMemory := calculateUncompressedMemory(edgeLists)
	compressedMemory := calculateCompressedMemory(compressedLists)
	fmt.Printf("   Uncompressed:        %d bytes (%.2f MB)\n",
		uncompressedMemory, float64(uncompressedMemory)/(1024*1024))
	fmt.Printf("   Compressed:          %d bytes (%.2f MB)\n",
		compressedMemory, float64(compressedMemory)/(1024*1024))
	fmt.Printf("   Reduction:           %.2fx (%.1f%% savings)\n\n",
		float64(uncompressedMemory)/float64(compressedMemory),
		100*(1-float64(compressedMemory)/float64(uncompressedMemory)))

	// Summary
	fmt.Printf("📊 Summary\n")
	fmt.Printf("==================================\n")
	switch {
	case stats.AverageRatio >= 5.0:
		fmt.Printf("✅ Excellent! Achieved 5-8x compression target\n")
	case stats.AverageRatio >= 3.0:
		fmt.Printf("⚡ Good! Significant compression achieved\n")
	default:
		fmt.Printf("💡 Modest compression - may need different distribution\n")
	}

	fmt.Printf("🎯 Compression Ratio:     %.2fx\n", stats.AverageRatio)
	fmt.Printf("⚡ Decompression Speed:  %.0f edges/sec\n",
		float64(edgesDecompressed)/decompressionTime.Seconds())
	fmt.Printf("💾 Memory Savings:        %.1f%%\n",
		100*(1-float64(stats.CompressedBytes)/float64(stats.UncompressedBytes)))
}

func generateEdgeLists(numNodes, avgDegree int) [][]uint64 {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	edgeLists := make([][]uint64, numNodes)

	for i := 0; i < numNodes; i++ {
		// Random degree around average (using normal distribution)
		degree := avgDegree + rng.Intn(avgDegree/2) - avgDegree/4
		if degree < 0 {
			degree = 0
		}

		edges := make([]uint64, degree)
		for j := 0; j < degree; j++ {
			// Generate random target node IDs
			// Use clustering to get better compression (nearby IDs)
			offset := rng.Intn(100) - 50
			clusterInt := i + offset
			if clusterInt < 0 {
				clusterInt = 0
			}
			if clusterInt >= numNodes {
				clusterInt = numNodes - 1
			}
			edges[j] = uint64(clusterInt)
		}

		edgeLists[i] = edges
	}

	return edgeLists
}

func compressEdgeLists(edgeLists [][]uint64) ([]*storage.CompressedEdgeList, time.Duration) {
	start := time.Now()

	compressed := make([]*storage.CompressedEdgeList, len(edgeLists))
	for i, edges := range edgeLists {
		compressed[i] = storage.NewCompressedEdgeList(edges)
	}

	return compressed, time.Since(start)
}

func benchmarkDecompression(lists []*storage.CompressedEdgeList) (time.Duration, int) {
	start := time.Now()
	totalEdges := 0

	for _, list := range lists {
		edges := list.Decompress()
		totalEdges += len(edges)
	}

	return time.Since(start), totalEdges
}

func benchmarkRandomAccess(lists []*storage.CompressedEdgeList, maxNodeID int) (time.Duration, int) {
	start := time.Now()
	accessCount := 1000 // Perform 1000 random lookups

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	for i := 0; i < accessCount; i++ {
		// Random list
		listIdx := rng.Intn(len(lists))
		list := lists[listIdx]

		// Random node ID
		nodeID := uint64(rng.Intn(maxNodeID))

		// Check if contains
		_ = list.Contains(nodeID)
	}

	return time.Since(start), accessCount
}

func calculateUncompressedMemory(edgeLists [][]uint64) int {
	total := 0
	for _, edges := range edgeLists {
		total += len(edges) * 8 // Each uint64 is 8 bytes
		total += 24             // Slice header overhead
	}
	return total
}

func calculateCompressedMemory(lists []*storage.CompressedEdgeList) int {
	total := 0
	for _, list := range lists {
		total += list.Size()
	}
	return total
}
