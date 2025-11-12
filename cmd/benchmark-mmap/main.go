package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

func main() {
	numEntries := flag.Int("entries", 10000, "Number of entries in SSTable")
	numReads := flag.Int("reads", 1000, "Number of read operations")
	valueSize := flag.Int("value-size", 1024, "Size of values in bytes")
	flag.Parse()

	fmt.Printf("🔬 Memory-Mapped SSTable Benchmark\n")
	fmt.Printf("===================================\n\n")

	// Create test data
	fmt.Printf("📝 Generating %d entries with %d byte values...\n", *numEntries, *valueSize)
	entries := generateEntries(*numEntries, *valueSize)

	// Create SSTable
	os.RemoveAll("./data/benchmark-mmap")
	os.MkdirAll("./data/benchmark-mmap", 0755)
	sstPath := "./data/benchmark-mmap/test.sst"

	fmt.Printf("💾 Creating SSTable at %s...\n", sstPath)
	sst, err := lsm.NewSSTable(sstPath, entries)
	if err != nil {
		log.Fatalf("Failed to create SSTable: %v", err)
	}
	sst.Close()

	fileInfo, _ := os.Stat(sstPath)
	fileSizeMB := float64(fileInfo.Size()) / 1024 / 1024
	fmt.Printf("   File size: %.2f MB\n\n", fileSizeMB)

	// Benchmark 1: Regular SSTable reads
	fmt.Printf("📖 Testing Regular SSTable (file I/O)...\n")
	regularStats := benchmarkRegularSSTable(sstPath, entries, *numReads)
	fmt.Printf("   Reads:       %d\n", regularStats.Reads)
	fmt.Printf("   Duration:    %s\n", regularStats.Duration)
	fmt.Printf("   Avg Latency: %.2fµs\n", regularStats.AvgLatencyUs)
	fmt.Printf("   Throughput:  %.0f reads/sec\n\n", regularStats.Throughput)

	// Benchmark 2: Memory-mapped SSTable reads
	fmt.Printf("🗺️  Testing Memory-Mapped SSTable...\n")
	mmapStats := benchmarkMappedSSTable(sstPath, entries, *numReads)
	fmt.Printf("   Reads:       %d\n", mmapStats.Reads)
	fmt.Printf("   Duration:    %s\n", mmapStats.Duration)
	fmt.Printf("   Avg Latency: %.2fµs\n", mmapStats.AvgLatencyUs)
	fmt.Printf("   Throughput:  %.0f reads/sec\n\n", mmapStats.Throughput)

	// Summary
	fmt.Printf("📊 Comparison\n")
	fmt.Printf("===================================\n")
	fmt.Printf("Regular SSTable:     %.2fµs/read, %.0f reads/sec\n",
		regularStats.AvgLatencyUs, regularStats.Throughput)
	fmt.Printf("Memory-Mapped:       %.2fµs/read, %.0f reads/sec\n",
		mmapStats.AvgLatencyUs, mmapStats.Throughput)

	speedup := regularStats.AvgLatencyUs / mmapStats.AvgLatencyUs
	fmt.Printf("\n🚀 Speedup:           %.1fx faster\n", speedup)

	if speedup >= 5.0 {
		fmt.Printf("✅ Achieved expected 5-10x speedup!\n")
	} else if speedup >= 2.0 {
		fmt.Printf("⚡ Good speedup achieved!\n")
	} else {
		fmt.Printf("💡 Modest improvement (cache effects likely)\n")
	}
}

type BenchmarkStats struct {
	Reads        int
	Duration     time.Duration
	AvgLatencyUs float64
	Throughput   float64
}

func generateEntries(count, valueSize int) []*lsm.Entry {
	entries := make([]*lsm.Entry, count)
	value := make([]byte, valueSize)

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("key_%08d", i)
		rand.Read(value)

		entries[i] = &lsm.Entry{
			Key:       []byte(key),
			Value:     value,
			Timestamp: time.Now().UnixNano(),
			Deleted:   false,
		}
	}

	return entries
}

func benchmarkRegularSSTable(path string, entries []*lsm.Entry, numReads int) BenchmarkStats {
	// Open regular SSTable
	sst, err := lsm.OpenSSTable(path)
	if err != nil {
		log.Fatalf("Failed to open SSTable: %v", err)
	}
	defer sst.Close()

	// Warm-up reads
	for i := 0; i < 100; i++ {
		idx := rand.Intn(len(entries))
		sst.Get(entries[idx].Key)
	}

	// Benchmark reads
	found := 0
	start := time.Now()

	for i := 0; i < numReads; i++ {
		idx := rand.Intn(len(entries))
		_, ok := sst.Get(entries[idx].Key)
		if ok {
			found++
		}
	}

	duration := time.Since(start)
	avgLatencyUs := float64(duration.Microseconds()) / float64(numReads)
	throughput := float64(numReads) / duration.Seconds()

	if found < numReads*9/10 {
		log.Printf("Warning: Only found %d/%d entries in regular SSTable", found, numReads)
	}

	return BenchmarkStats{
		Reads:        numReads,
		Duration:     duration,
		AvgLatencyUs: avgLatencyUs,
		Throughput:   throughput,
	}
}

func benchmarkMappedSSTable(path string, entries []*lsm.Entry, numReads int) BenchmarkStats {
	// Open memory-mapped SSTable
	sst, err := lsm.OpenMappedSSTable(path)
	if err != nil {
		log.Fatalf("Failed to open mapped SSTable: %v", err)
	}
	defer sst.Close()

	// Warm-up reads
	for i := 0; i < 100; i++ {
		idx := rand.Intn(len(entries))
		sst.Get(entries[idx].Key)
	}

	// Benchmark reads
	found := 0
	start := time.Now()

	for i := 0; i < numReads; i++ {
		idx := rand.Intn(len(entries))
		_, ok := sst.Get(entries[idx].Key)
		if ok {
			found++
		}
	}

	duration := time.Since(start)
	avgLatencyUs := float64(duration.Microseconds()) / float64(numReads)
	throughput := float64(numReads) / duration.Seconds()

	if found < numReads*9/10 {
		log.Printf("Warning: Only found %d/%d entries in mapped SSTable", found, numReads)
	}

	return BenchmarkStats{
		Reads:        numReads,
		Duration:     duration,
		AvgLatencyUs: avgLatencyUs,
		Throughput:   throughput,
	}
}
