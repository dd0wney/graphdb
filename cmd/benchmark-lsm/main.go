package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/lsm"
)

func main() {
	writes := flag.Int("writes", 100000, "Number of writes")
	reads := flag.Int("reads", 10000, "Number of reads")
	valueSize := flag.Int("value-size", 1024, "Value size in bytes")
	flag.Parse()

	fmt.Printf("🔥 Cluso GraphDB - LSM Storage Benchmark\n")
	fmt.Printf("========================================\n\n")
	fmt.Printf("Configuration:\n")
	fmt.Printf("  Writes: %d\n", *writes)
	fmt.Printf("  Reads: %d\n", *reads)
	fmt.Printf("  Value Size: %d bytes\n\n", *valueSize)

	// Clean up old data
	os.RemoveAll("./data/benchmark-lsm")

	// Create LSM storage
	fmt.Printf("📂 Initializing LSM storage...\n")
	opts := lsm.DefaultLSMOptions("./data/benchmark-lsm")
	opts.MemTableSize = 4 * 1024 * 1024 // 4MB

	storage, err := lsm.NewLSMStorage(opts)
	if err != nil {
		log.Fatalf("Failed to create LSM storage: %v", err)
	}
	defer storage.Close()

	// Prepare test data
	fmt.Printf("\n📝 Benchmark 1: Sequential Writes\n")
	value := make([]byte, *valueSize)
	for i := range value {
		value[i] = byte(rand.Intn(256))
	}

	start := time.Now()
	for i := 0; i < *writes; i++ {
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))

		if err := storage.Put(key, value); err != nil {
			log.Fatalf("Failed to write: %v", err)
		}

		if (i+1)%10000 == 0 {
			fmt.Printf("  Written %d entries...\n", i+1)
		}
	}

	duration := time.Since(start)
	throughput := float64(*writes) / duration.Seconds()
	avgLatency := duration.Microseconds() / int64(*writes)

	fmt.Printf("✅ Completed %d writes in %v\n", *writes, duration)
	fmt.Printf("  ⚡ Average: %dμs per write\n", avgLatency)
	fmt.Printf("  🚀 Throughput: %.0f writes/sec\n", throughput)
	fmt.Printf("  💾 Data written: %.2f MB\n", float64(*writes**valueSize)/(1024*1024))

	// Wait for background flushes
	fmt.Printf("\n⏱️  Waiting for background flushes...\n")
	time.Sleep(3 * time.Second)

	// Read benchmark
	fmt.Printf("\n📖 Benchmark 2: Random Reads\n")
	start = time.Now()
	found := 0

	for i := 0; i < *reads; i++ {
		randomIdx := rand.Intn(*writes)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(randomIdx))

		if _, ok := storage.Get(key); ok {
			found++
		}

		if (i+1)%1000 == 0 {
			fmt.Printf("  Read %d entries...\n", i+1)
		}
	}

	duration = time.Since(start)
	throughput = float64(*reads) / duration.Seconds()
	avgLatency = duration.Microseconds() / int64(*reads)

	fmt.Printf("✅ Completed %d reads in %v\n", *reads, duration)
	fmt.Printf("  ✅ Found: %d/%d (%.1f%%)\n", found, *reads, float64(found)*100/float64(*reads))
	fmt.Printf("  ⚡ Average: %dμs per read\n", avgLatency)
	fmt.Printf("  🚀 Throughput: %.0f reads/sec\n", throughput)

	// Range scan benchmark
	fmt.Printf("\n🔍 Benchmark 3: Range Scans\n")
	scanCount := 100
	scanSize := 1000
	start = time.Now()
	totalResults := 0

	for i := 0; i < scanCount; i++ {
		startIdx := rand.Intn(*writes - scanSize)
		startKey := make([]byte, 8)
		endKey := make([]byte, 8)
		binary.BigEndian.PutUint64(startKey, uint64(startIdx))
		binary.BigEndian.PutUint64(endKey, uint64(startIdx+scanSize))

		results, err := storage.Scan(startKey, endKey)
		if err != nil {
			log.Printf("Scan failed: %v", err)
			continue
		}
		totalResults += len(results)
	}

	duration = time.Since(start)
	avgLatency = duration.Milliseconds() / int64(scanCount)

	fmt.Printf("✅ Completed %d scans in %v\n", scanCount, duration)
	fmt.Printf("  📊 Average results per scan: %d\n", totalResults/scanCount)
	fmt.Printf("  ⚡ Average: %dms per scan\n", avgLatency)
	fmt.Printf("  🚀 Throughput: %.0f scans/sec\n", float64(scanCount)/duration.Seconds())

	// Update benchmark
	fmt.Printf("\n✏️  Benchmark 4: Random Updates\n")
	updateCount := *writes / 10
	newValue := make([]byte, *valueSize)
	for i := range newValue {
		newValue[i] = byte(0xFF)
	}

	start = time.Now()
	for i := 0; i < updateCount; i++ {
		randomIdx := rand.Intn(*writes)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(randomIdx))

		if err := storage.Put(key, newValue); err != nil {
			log.Fatalf("Failed to update: %v", err)
		}

		if (i+1)%1000 == 0 {
			fmt.Printf("  Updated %d entries...\n", i+1)
		}
	}

	duration = time.Since(start)
	throughput = float64(updateCount) / duration.Seconds()
	avgLatency = duration.Microseconds() / int64(updateCount)

	fmt.Printf("✅ Completed %d updates in %v\n", updateCount, duration)
	fmt.Printf("  ⚡ Average: %dμs per update\n", avgLatency)
	fmt.Printf("  🚀 Throughput: %.0f updates/sec\n", throughput)

	// Deletion benchmark
	fmt.Printf("\n🗑️  Benchmark 5: Random Deletions\n")
	deleteCount := *writes / 20
	start = time.Now()

	for i := 0; i < deleteCount; i++ {
		randomIdx := rand.Intn(*writes)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(randomIdx))

		if err := storage.Delete(key); err != nil {
			log.Fatalf("Failed to delete: %v", err)
		}

		if (i+1)%1000 == 0 {
			fmt.Printf("  Deleted %d entries...\n", i+1)
		}
	}

	duration = time.Since(start)
	throughput = float64(deleteCount) / duration.Seconds()
	avgLatency = duration.Microseconds() / int64(deleteCount)

	fmt.Printf("✅ Completed %d deletions in %v\n", deleteCount, duration)
	fmt.Printf("  ⚡ Average: %dμs per deletion\n", avgLatency)
	fmt.Printf("  🚀 Throughput: %.0f deletions/sec\n", throughput)

	// Wait for final compaction
	fmt.Printf("\n⏱️  Waiting for compaction...\n")
	time.Sleep(5 * time.Second)

	// Print final statistics
	fmt.Printf("\n📊 Final LSM Storage Statistics\n")
	fmt.Printf("================================\n")
	storage.PrintStats()

	fmt.Printf("\n✅ Benchmark complete!\n")
}
