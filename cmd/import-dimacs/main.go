package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	graphFile := flag.String("graph", "", "Path to .gr graph file")
	coordFile := flag.String("coords", "", "Path to .co coordinates file (optional)")
	dataDir := flag.String("data", "./data/dimacs", "Output data directory")
	maxNodes := flag.Int("max-nodes", 0, "Maximum nodes to import (0 = all)")
	maxEdges := flag.Int("max-edges", 0, "Maximum edges to import (0 = all)")
	flag.Parse()

	if *graphFile == "" {
		log.Fatal("Usage: import-dimacs --graph <file.gr> [--coords <file.co>] [--data <dir>] [--max-nodes N] [--max-edges N]")
	}

	fmt.Printf("🔥 DIMACS Graph Importer\n")
	fmt.Printf("========================\n\n")

	// Create graph storage
	fmt.Printf("📂 Creating graph storage at %s...\n", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		log.Fatalf("Failed to create graph storage: %v", err)
	}
	defer graph.Close()

	// Import coordinates if provided
	coords := make(map[uint64]struct {
		lat float64
		lon float64
	})

	if *coordFile != "" {
		fmt.Printf("📍 Loading coordinates from %s...\n", *coordFile)
		if err := loadCoordinates(*coordFile, coords, *maxNodes); err != nil {
			log.Fatalf("Failed to load coordinates: %v", err)
		}
		fmt.Printf("   Loaded %d coordinates\n\n", len(coords))
	}

	// Import graph
	fmt.Printf("📊 Importing graph from %s...\n", *graphFile)
	stats, err := importGraph(*graphFile, graph, coords, *maxNodes, *maxEdges)
	if err != nil {
		log.Fatalf("Failed to import graph: %v", err)
	}

	fmt.Printf("\n✅ Import completed successfully!\n")
	fmt.Printf("   Nodes:    %d\n", stats.NodesImported)
	fmt.Printf("   Edges:    %d\n", stats.EdgesImported)
	fmt.Printf("   Duration: %s\n", stats.Duration)
	fmt.Printf("   Rate:     %.0f nodes/sec, %.0f edges/sec\n",
		float64(stats.NodesImported)/stats.Duration.Seconds(),
		float64(stats.EdgesImported)/stats.Duration.Seconds())
}

type ImportStats struct {
	NodesImported int
	EdgesImported int
	Duration      time.Duration
}

func loadCoordinates(filename string, coords map[uint64]struct {
	lat float64
	lon float64
}, maxNodes int) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "c") || strings.HasPrefix(line, "p") {
			continue
		}

		if strings.HasPrefix(line, "v") {
			parts := strings.Fields(line)
			if len(parts) != 4 {
				continue
			}

			nodeID, _ := strconv.ParseUint(parts[1], 10, 64)
			lon, _ := strconv.ParseFloat(parts[2], 64)
			lat, _ := strconv.ParseFloat(parts[3], 64)

			coords[nodeID] = struct {
				lat float64
				lon float64
			}{lat: lat / 1000000, lon: lon / 1000000}

			count++
			if count%1000000 == 0 {
				fmt.Printf("   Loaded %dM coordinates...\n", count/1000000)
			}

			if maxNodes > 0 && count >= maxNodes {
				break
			}
		}
	}

	return scanner.Err()
}

func importGraph(filename string, graph storage.Storage, coords map[uint64]struct {
	lat float64
	lon float64
}, maxNodes, maxEdges int) (*ImportStats, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	stats := &ImportStats{}
	start := time.Now()

	nodeMap := make(map[uint64]uint64) // DIMACS ID -> Graph ID
	edgeCount := 0
	lastReport := time.Now()

	// Use batch API for much faster imports
	batch := graph.BeginBatch()
	batchSize := 0
	const maxBatchSize = 10000

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and header
		if strings.HasPrefix(line, "c") || strings.HasPrefix(line, "p") {
			continue
		}

		// Parse arc: a <from> <to> <weight>
		if strings.HasPrefix(line, "a") {
			parts := strings.Fields(line)
			if len(parts) != 4 {
				continue
			}

			fromDIMACS, _ := strconv.ParseUint(parts[1], 10, 64)
			toDIMACS, _ := strconv.ParseUint(parts[2], 10, 64)
			weight, _ := strconv.ParseInt(parts[3], 10, 64)

			// Create nodes if they don't exist
			fromID, exists := nodeMap[fromDIMACS]
			if !exists {
				if maxNodes > 0 && len(nodeMap) >= maxNodes {
					continue
				}

				props := map[string]storage.Value{
					"dimacs_id": storage.IntValue(int64(fromDIMACS)),
				}

				if coord, ok := coords[fromDIMACS]; ok {
					props["lat"] = storage.FloatValue(coord.lat)
					props["lon"] = storage.FloatValue(coord.lon)
				}

				var err error
				fromID, err = batch.AddNode([]string{"Location"}, props)
				if err != nil {
					return nil, fmt.Errorf("failed to add from node: %w", err)
				}
				nodeMap[fromDIMACS] = fromID
				stats.NodesImported++
				batchSize++
			}

			toID, exists := nodeMap[toDIMACS]
			if !exists {
				if maxNodes > 0 && len(nodeMap) >= maxNodes {
					continue
				}

				props := map[string]storage.Value{
					"dimacs_id": storage.IntValue(int64(toDIMACS)),
				}

				if coord, ok := coords[toDIMACS]; ok {
					props["lat"] = storage.FloatValue(coord.lat)
					props["lon"] = storage.FloatValue(coord.lon)
				}

				var err error
				toID, err = batch.AddNode([]string{"Location"}, props)
				if err != nil {
					return nil, fmt.Errorf("failed to add to node: %w", err)
				}
				nodeMap[toDIMACS] = toID
				stats.NodesImported++
				batchSize++
			}

			// Create edge
			if maxEdges == 0 || edgeCount < maxEdges {
				edgeProps := map[string]storage.Value{
					"distance": storage.IntValue(weight),
				}

				_, err := batch.AddEdge(fromID, toID, "ROAD", edgeProps, float64(weight))
				if err != nil {
					return nil, fmt.Errorf("failed to add edge: %w", err)
				}
				stats.EdgesImported++
				edgeCount++
				batchSize++
			}

			// Commit batch periodically for progress reporting
			if batchSize >= maxBatchSize {
				if err := batch.Commit(); err != nil {
					return nil, err
				}
				batch = graph.BeginBatch()
				batchSize = 0
			}

			// Progress reporting
			if time.Since(lastReport) > 5*time.Second {
				elapsed := time.Since(start)
				fmt.Printf("   Progress: %d nodes, %d edges (%.0f nodes/sec, %.0f edges/sec)\n",
					stats.NodesImported,
					stats.EdgesImported,
					float64(stats.NodesImported)/elapsed.Seconds(),
					float64(stats.EdgesImported)/elapsed.Seconds())
				lastReport = time.Now()
			}

			if maxEdges > 0 && edgeCount >= maxEdges {
				break
			}
		}
	}

	// Commit any remaining operations
	if batchSize > 0 {
		if err := batch.Commit(); err != nil {
			return nil, err
		}
	}

	stats.Duration = time.Since(start)

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}
