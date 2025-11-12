package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	nodes := flag.Int("nodes", 100000, "Number of nodes to create")
	queries := flag.Int("queries", 1000, "Number of queries to test")
	flag.Parse()

	fmt.Printf("🔍 Cluso GraphDB Property Index Benchmark\n")
	fmt.Printf("==========================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Nodes: %d\n", *nodes)
	fmt.Printf("  Queries: %d\n\n", *queries)

	// Clean up old data
	os.RemoveAll("./data/benchmark-index")

	// Initialize storage
	fmt.Printf("📂 Initializing storage...\n")
	graph, err := storage.NewGraphStorage("./data/benchmark-index")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	// Create nodes with various properties
	fmt.Printf("\n📝 Creating %d nodes with properties...\n", *nodes)
	start := time.Now()

	for i := 0; i < *nodes; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		trustScore := int64(rand.Intn(1000))
		age := int64(18 + rand.Intn(70))

		_, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"email":      storage.StringValue(email),
				"trustScore": storage.IntValue(trustScore),
				"age":        storage.IntValue(age),
				"created":    storage.TimestampValue(time.Now()),
			},
		)
		if err != nil {
			log.Fatalf("Failed to create node: %v", err)
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("  Created %d nodes...\n", i+1)
		}
	}

	duration := time.Since(start)
	fmt.Printf("✅ Created %d nodes in %v\n", *nodes, duration)

	// Benchmark 1: Non-indexed property lookup (full table scan)
	fmt.Printf("\n📊 Benchmark 1: Non-Indexed Property Lookup (Full Table Scan)\n")
	start = time.Now()

	for i := 0; i < *queries; i++ {
		targetEmail := fmt.Sprintf("user%d@example.com", rand.Intn(*nodes))
		_, err := graph.FindNodesByProperty("email", storage.StringValue(targetEmail))
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ %d queries in %v\n", *queries, duration)
	fmt.Printf("⚡ Average: %.2fms per query\n", float64(duration.Milliseconds())/float64(*queries))
	fmt.Printf("🚀 Throughput: %.0f queries/sec\n", float64(*queries)/duration.Seconds())
	nonIndexedTime := duration

	// Create indexes
	fmt.Printf("\n🔨 Creating property indexes...\n")
	start = time.Now()

	err = graph.CreatePropertyIndex("email", storage.TypeString)
	if err != nil {
		log.Fatalf("Failed to create email index: %v", err)
	}

	err = graph.CreatePropertyIndex("trustScore", storage.TypeInt)
	if err != nil {
		log.Fatalf("Failed to create trustScore index: %v", err)
	}

	err = graph.CreatePropertyIndex("age", storage.TypeInt)
	if err != nil {
		log.Fatalf("Failed to create age index: %v", err)
	}

	duration = time.Since(start)
	fmt.Printf("✅ Indexes created in %v\n", duration)

	// Show index statistics
	indexStats := graph.GetIndexStatistics()
	fmt.Printf("\n📈 Index Statistics:\n")
	for key, stats := range indexStats {
		fmt.Printf("  %s:\n", key)
		fmt.Printf("    Unique values: %d\n", stats.UniqueValues)
		fmt.Printf("    Total nodes: %d\n", stats.TotalNodes)
		fmt.Printf("    Avg nodes per key: %.2f\n", stats.AvgNodesPerKey)
	}

	// Benchmark 2: Indexed property lookup (O(1) hash lookup)
	fmt.Printf("\n📊 Benchmark 2: Indexed Property Lookup (O(1) Hash Lookup)\n")
	start = time.Now()

	for i := 0; i < *queries; i++ {
		targetEmail := fmt.Sprintf("user%d@example.com", rand.Intn(*nodes))
		_, err := graph.FindNodesByPropertyIndexed("email", storage.StringValue(targetEmail))
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
	}

	duration = time.Since(start)
	fmt.Printf("✅ %d queries in %v\n", *queries, duration)
	fmt.Printf("⚡ Average: %.2fμs per query\n", float64(duration.Microseconds())/float64(*queries))
	fmt.Printf("🚀 Throughput: %.0f queries/sec\n", float64(*queries)/duration.Seconds())
	indexedTime := duration

	// Benchmark 3: Range queries (indexed)
	fmt.Printf("\n📊 Benchmark 3: Indexed Range Query (trustScore between 500-600)\n")
	start = time.Now()

	totalFound := 0
	for i := 0; i < *queries; i++ {
		nodes, err := graph.FindNodesByPropertyRange(
			"trustScore",
			storage.IntValue(500),
			storage.IntValue(600),
		)
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
		totalFound += len(nodes)
	}

	duration = time.Since(start)
	fmt.Printf("✅ %d range queries in %v\n", *queries, duration)
	fmt.Printf("📊 Average nodes found: %.1f\n", float64(totalFound)/float64(*queries))
	fmt.Printf("⚡ Average: %.2fμs per query\n", float64(duration.Microseconds())/float64(*queries))
	fmt.Printf("🚀 Throughput: %.0f queries/sec\n", float64(*queries)/duration.Seconds())

	// Benchmark 4: Prefix queries (indexed)
	fmt.Printf("\n📊 Benchmark 4: Indexed Prefix Query (emails starting with 'user1')\n")
	start = time.Now()

	totalFound = 0
	for i := 0; i < *queries; i++ {
		nodes, err := graph.FindNodesByPropertyPrefix("email", "user1")
		if err != nil {
			log.Fatalf("Query failed: %v", err)
		}
		totalFound += len(nodes)
	}

	duration = time.Since(start)
	fmt.Printf("✅ %d prefix queries in %v\n", *queries, duration)
	fmt.Printf("📊 Average nodes found: %.1f\n", float64(totalFound)/float64(*queries))
	fmt.Printf("⚡ Average: %.2fμs per query\n", float64(duration.Microseconds())/float64(*queries))
	fmt.Printf("🚀 Throughput: %.0f queries/sec\n", float64(*queries)/duration.Seconds())

	// Summary
	fmt.Printf("\n🎯 Performance Summary\n")
	fmt.Printf("======================\n")
	fmt.Printf("Non-indexed lookup: %.2fms per query\n", float64(nonIndexedTime.Milliseconds())/float64(*queries))
	fmt.Printf("Indexed lookup:     %.2fμs per query\n", float64(indexedTime.Microseconds())/float64(*queries))

	speedup := float64(nonIndexedTime.Nanoseconds()) / float64(indexedTime.Nanoseconds())
	fmt.Printf("\n🚀 Speedup: %.0fx faster with indexing\n", speedup)
	fmt.Printf("💡 Time saved per query: %.2fms\n", float64(nonIndexedTime.Microseconds()-indexedTime.Microseconds())/1000.0)
}
