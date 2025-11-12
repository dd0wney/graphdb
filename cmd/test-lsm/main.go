package main

import (
	"fmt"
	"log"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

func main() {
	// Clean up
	os.RemoveAll("./data/test-lsm")

	fmt.Println("Creating LSM storage...")
	opts := lsm.DefaultLSMOptions("./data/test-lsm")
	opts.MemTableSize = 1024          // Very small for quick flush
	opts.EnableAutoCompaction = false // Disable auto for manual control

	storage, err := lsm.NewLSMStorage(opts)
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}

	// Write some data
	fmt.Println("Writing data...")
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%03d", i))
		if err := storage.Put(key, value); err != nil {
			log.Fatalf("Failed to write: %v", err)
		}
		fmt.Printf("  Wrote %s = %s\n", key, value)
	}

	// Read from MemTable (should work)
	fmt.Println("\nReading from MemTable...")
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		if value, ok := storage.Get(key); ok {
			fmt.Printf("  Read %s = %s ✓\n", key, value)
		} else {
			fmt.Printf("  Read %s = NOT FOUND ✗\n", key)
		}
	}

	// Print stats
	fmt.Println("\nStats before close:")
	storage.PrintStats()

	// Close (should flush)
	fmt.Println("\nClosing storage...")
	if err := storage.Close(); err != nil {
		log.Fatalf("Failed to close: %v", err)
	}

	// Reopen
	fmt.Println("\nReopening storage...")
	storage2, err := lsm.NewLSMStorage(opts)
	if err != nil {
		log.Fatalf("Failed to reopen: %v", err)
	}
	defer storage2.Close()

	// Read from disk
	fmt.Println("\nReading from disk...")
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		if value, ok := storage2.Get(key); ok {
			fmt.Printf("  Read %s = %s ✓\n", key, value)
		} else {
			fmt.Printf("  Read %s = NOT FOUND ✗\n", key)
		}
	}

	fmt.Println("\n✅ Test complete!")
}
