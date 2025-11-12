package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/wal"
)

func main() {
	numWrites := flag.Int("writes", 10000, "Number of write operations")
	flag.Parse()

	fmt.Printf("🔬 WAL Compression Benchmark\n")
	fmt.Printf("============================\n\n")

	// Test 1: Regular WAL
	fmt.Printf("📝 Testing Regular WAL...\n")
	regularStats := benchmarkRegularWAL(*numWrites)
	fmt.Printf("   Writes:      %d\n", regularStats.Writes)
	fmt.Printf("   Duration:    %s\n", regularStats.Duration)
	fmt.Printf("   File Size:   %.2f MB\n", regularStats.FileSizeMB)
	fmt.Printf("   Write Rate:  %.0f ops/sec\n\n", float64(regularStats.Writes)/regularStats.Duration.Seconds())

	// Test 2: Compressed WAL
	fmt.Printf("📦 Testing Compressed WAL...\n")
	compressedStats := benchmarkCompressedWAL(*numWrites)
	fmt.Printf("   Writes:           %d\n", compressedStats.Writes)
	fmt.Printf("   Duration:         %s\n", compressedStats.Duration)
	fmt.Printf("   File Size:        %.2f MB\n", compressedStats.FileSizeMB)
	fmt.Printf("   Write Rate:       %.0f ops/sec\n", float64(compressedStats.Writes)/compressedStats.Duration.Seconds())
	fmt.Printf("   Compression:      %.1f%%\n", compressedStats.CompressionRatio*100)
	fmt.Printf("   Space Saved:      %.2f MB\n\n", compressedStats.SpaceSaved)

	// Summary
	fmt.Printf("📊 Comparison\n")
	fmt.Printf("============================\n")
	fmt.Printf("Regular WAL:      %.2f MB\n", regularStats.FileSizeMB)
	fmt.Printf("Compressed WAL:   %.2f MB\n", compressedStats.FileSizeMB)
	fmt.Printf("Compression:      %.1fx smaller\n", regularStats.FileSizeMB/compressedStats.FileSizeMB)
	fmt.Printf("Space Saved:      %.2f MB (%.1f%%)\n",
		regularStats.FileSizeMB-compressedStats.FileSizeMB,
		(1.0-compressedStats.FileSizeMB/regularStats.FileSizeMB)*100)

	speedDiff := compressedStats.Duration.Seconds() / regularStats.Duration.Seconds()
	if speedDiff > 1.0 {
		fmt.Printf("Speed Impact:     %.1fx slower\n", speedDiff)
	} else {
		fmt.Printf("Speed Impact:     %.1fx faster\n", 1.0/speedDiff)
	}
}

type BenchmarkStats struct {
	Writes           int
	Duration         time.Duration
	FileSizeMB       float64
	CompressionRatio float64
	SpaceSaved       float64
}

func benchmarkRegularWAL(numWrites int) BenchmarkStats {
	// Clean up
	os.RemoveAll("./data/benchmark-wal-regular")

	// Create regular WAL
	w, err := wal.NewWAL("./data/benchmark-wal-regular")
	if err != nil {
		log.Fatalf("Failed to create WAL: %v", err)
	}

	// Benchmark writes
	start := time.Now()
	for i := 0; i < numWrites; i++ {
		// Create realistic node data
		nodeData := map[string]interface{}{
			"id":   uint64(i),
			"name": fmt.Sprintf("Node_%d", i),
			"properties": map[string]interface{}{
				"age":      i % 100,
				"city":     "San Francisco",
				"country":  "USA",
				"active":   i%2 == 0,
				"score":    float64(i) * 1.5,
				"metadata": "This is some metadata that makes the entry larger and more realistic",
			},
			"labels": []string{"Person", "User"},
		}

		data, _ := json.Marshal(nodeData)
		w.Append(wal.OpCreateNode, data)
	}
	duration := time.Since(start)

	// Close
	w.Close()

	// Get file size
	fileInfo, _ := os.Stat("./data/benchmark-wal-regular/wal.log")
	fileSizeMB := float64(fileInfo.Size()) / 1024 / 1024

	return BenchmarkStats{
		Writes:     numWrites,
		Duration:   duration,
		FileSizeMB: fileSizeMB,
	}
}

func benchmarkCompressedWAL(numWrites int) BenchmarkStats {
	// Clean up
	os.RemoveAll("./data/benchmark-wal-compressed")

	// Create compressed WAL
	w, err := wal.NewCompressedWAL("./data/benchmark-wal-compressed")
	if err != nil {
		log.Fatalf("Failed to create compressed WAL: %v", err)
	}

	// Benchmark writes
	start := time.Now()
	for i := 0; i < numWrites; i++ {
		// Create realistic node data (same as regular)
		nodeData := map[string]interface{}{
			"id":   uint64(i),
			"name": fmt.Sprintf("Node_%d", i),
			"properties": map[string]interface{}{
				"age":      i % 100,
				"city":     "San Francisco",
				"country":  "USA",
				"active":   i%2 == 0,
				"score":    float64(i) * 1.5,
				"metadata": "This is some metadata that makes the entry larger and more realistic",
			},
			"labels": []string{"Person", "User"},
		}

		data, _ := json.Marshal(nodeData)
		w.Append(wal.OpCreateNode, data)
	}
	duration := time.Since(start)

	// Get compression stats before closing
	stats := w.GetStatistics()

	// Close
	w.Close()

	// Get file size
	fileInfo, _ := os.Stat("./data/benchmark-wal-compressed/wal_compressed.log")
	fileSizeMB := float64(fileInfo.Size()) / 1024 / 1024

	return BenchmarkStats{
		Writes:           numWrites,
		Duration:         duration,
		FileSizeMB:       fileSizeMB,
		CompressionRatio: stats.CompressionRatio,
		SpaceSaved:       stats.SpaceSavings,
	}
}
