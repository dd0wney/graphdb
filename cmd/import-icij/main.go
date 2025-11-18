package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ICIJ Offshore Leaks CSV schema
// Nodes: node_id, name, jurisdiction, country_codes, countries, node_type, source_id, address, valid_until, note
// Edges: rel_type, node_1, node_2, link, start_date, end_date

type ICIJNode struct {
	NodeID        string
	Name          string
	Jurisdiction  string
	CountryCodes  string
	Countries     string
	NodeType      string // Entity, Officer, Intermediary, Address
	SourceID      string // panama_papers, paradise_papers, pandora_papers, etc.
	Address       string
	ValidUntil    string
	Note          string
}

type ICIJEdge struct {
	RelType       string // officer_of, intermediary_of, registered_address, etc.
	NodeIDStart   string // ICIJ uses node_id_start
	NodeIDEnd     string // ICIJ uses node_id_end
	Link          string
	Status        string
	StartDate     string
	EndDate       string
}

func main() {
	nodesFile := flag.String("nodes", "", "Path to nodes.csv")
	edgesFile := flag.String("edges", "", "Path to edges.csv (relationships)")
	dataDir := flag.String("data", "./data/icij", "GraphDB data directory")
	batchSize := flag.Int("batch", 10000, "Batch size for imports")
	flag.Parse()

	if *nodesFile == "" || *edgesFile == "" {
		fmt.Println("Usage: import-icij --nodes nodes.csv --edges edges.csv [--data ./data/icij] [--batch 10000]")
		fmt.Println()
		fmt.Println("Download ICIJ Offshore Leaks data from:")
		fmt.Println("  https://offshoreleaks.icij.org/pages/database")
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("ICIJ Offshore Leaks Importer for GraphDB")
	logger.Info("opening graph storage", "data_dir", *dataDir)

	// Create graph storage with optimized config for bulk import
	graph, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:               *dataDir,
		EnableBatching:        false, // Not needed in bulk import mode
		EnableCompression:     false, // Disable for faster writes during import
		EnableEdgeCompression: true,  // Keep edge compression for memory efficiency
		BatchSize:             1000,
		FlushInterval:         100 * time.Millisecond,
		UseDiskBackedEdges:    false, // Use in-memory for faster import
		EdgeCacheSize:         50000, // Large cache for import
		BulkImportMode:        true,  // Skip WAL and use fast path for bulk loading
	})
	if err != nil {
		logger.Error("failed to create graph storage", "error", err)
		os.Exit(1)
	}
	defer graph.Close()

	// Import nodes
	logger.Info("importing nodes", "file", *nodesFile, "batch_size", *batchSize)
	nodeCount, nodeDuration := importNodes(graph, *nodesFile, *batchSize, logger)
	logger.Info("nodes imported",
		"count", nodeCount,
		"duration_sec", nodeDuration.Seconds(),
		"nodes_per_sec", int(float64(nodeCount)/nodeDuration.Seconds()),
	)

	// Import edges
	logger.Info("importing edges", "file", *edgesFile, "batch_size", *batchSize)
	edgeCount, edgeDuration := importEdges(graph, *edgesFile, *batchSize, logger)
	logger.Info("edges imported",
		"count", edgeCount,
		"duration_sec", edgeDuration.Seconds(),
		"edges_per_sec", int(float64(edgeCount)/edgeDuration.Seconds()),
	)

	// Final statistics
	stats := graph.GetStatistics()
	logger.Info("import complete",
		"total_nodes", stats.NodeCount,
		"total_edges", stats.EdgeCount,
		"total_duration_sec", (nodeDuration + edgeDuration).Seconds(),
	)

	// Show breakdown by node type
	logger.Info("querying node type distribution")
	showNodeTypeDistribution(graph, logger)
}

func importNodes(graph *storage.GraphStorage, filename string, batchSize int, logger *slog.Logger) (int, time.Duration) {
	start := time.Now()

	file, err := os.Open(filename)
	if err != nil {
		logger.Error("failed to open nodes file", "error", err)
		os.Exit(1)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.ReuseRecord = true    // Memory optimization
	reader.FieldsPerRecord = -1  // Allow variable field counts (ICIJ has different schemas per node type)

	// Read header
	header, err := reader.Read()
	if err != nil {
		logger.Error("failed to read header", "error", err)
		os.Exit(1)
	}

	logger.Info("csv header", "fields", header)

	// Create column index map
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	count := 0
	batch := make([]ICIJNode, 0, batchSize)
	nodeIDMap := make(map[string]uint64) // Track ICIJ ID -> GraphDB ID mapping

	for {
		record, err := reader.Read()
		if err != nil {
			break // EOF or error
		}

		node := ICIJNode{
			NodeID:       getField(record, colIndex, "node_id"),
			Name:         getField(record, colIndex, "name"),
			Jurisdiction: getField(record, colIndex, "jurisdiction"),
			CountryCodes: getField(record, colIndex, "country_codes"),
			Countries:    getField(record, colIndex, "countries"),
			NodeType:     getField(record, colIndex, "node_type"),
			SourceID:     getField(record, colIndex, "sourceID"),
			Address:      getField(record, colIndex, "address"),
			ValidUntil:   getField(record, colIndex, "valid_until"),
			Note:         getField(record, colIndex, "note"),
		}

		batch = append(batch, node)

		if len(batch) >= batchSize {
			processBatch(graph, batch, nodeIDMap, logger)
			count += len(batch)
			logger.Info("progress", "nodes_imported", count)
			batch = batch[:0]
		}
	}

	// Process remaining batch
	if len(batch) > 0 {
		processBatch(graph, batch, nodeIDMap, logger)
		count += len(batch)
	}

	// Save node ID mapping for edge import
	saveNodeIDMap(nodeIDMap, logger)

	return count, time.Since(start)
}

func processBatch(graph *storage.GraphStorage, batch []ICIJNode, nodeIDMap map[string]uint64, logger *slog.Logger) {
	// Use Batch API for better performance
	graphBatch := graph.BeginBatch()

	for _, node := range batch {
		// Create node with properties (skip empty strings to save memory)
		props := make(map[string]storage.Value)
		props["icij_id"] = storage.StringValue(node.NodeID)

		if node.Name != "" {
			props["name"] = storage.StringValue(node.Name)
		}
		if node.Jurisdiction != "" {
			props["jurisdiction"] = storage.StringValue(node.Jurisdiction)
		}
		if node.CountryCodes != "" {
			props["country_codes"] = storage.StringValue(node.CountryCodes)
		}
		if node.Countries != "" {
			props["countries"] = storage.StringValue(node.Countries)
		}
		if node.SourceID != "" {
			props["source"] = storage.StringValue(node.SourceID)
		}
		if node.Address != "" {
			props["address"] = storage.StringValue(node.Address)
		}
		if node.ValidUntil != "" {
			props["valid_until"] = storage.StringValue(node.ValidUntil)
		}
		if node.Note != "" {
			props["note"] = storage.StringValue(node.Note)
		}

		// Use node_type as label (Entity, Officer, Intermediary, Address)
		label := node.NodeType
		if label == "" {
			label = "Unknown"
		}

		nodeID, err := graphBatch.AddNode([]string{label}, props)
		if err != nil {
			logger.Error("failed to add node to batch", "error", err, "icij_id", node.NodeID)
			continue
		}

		// Store mapping
		nodeIDMap[node.NodeID] = nodeID
	}

	// Commit the entire batch atomically
	if err := graphBatch.Commit(); err != nil {
		logger.Error("failed to commit batch", "error", err)
	}
}

func importEdges(graph *storage.GraphStorage, filename string, batchSize int, logger *slog.Logger) (int, time.Duration) {
	start := time.Now()

	// Load node ID mapping
	nodeIDMap := loadNodeIDMap(logger)
	if len(nodeIDMap) == 0 {
		logger.Error("no node ID mapping found - import nodes first")
		os.Exit(1)
	}

	file, err := os.Open(filename)
	if err != nil {
		logger.Error("failed to open edges file", "error", err)
		os.Exit(1)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.ReuseRecord = true    // Memory optimization
	reader.FieldsPerRecord = -1  // Allow variable field counts

	// Read header
	header, err := reader.Read()
	if err != nil {
		logger.Error("failed to read header", "error", err)
		os.Exit(1)
	}

	logger.Info("csv header", "fields", header)

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	count := 0
	skipped := 0
	edgeBatch := make([]ICIJEdge, 0, batchSize)

	for {
		record, err := reader.Read()
		if err != nil {
			break
		}

		edge := ICIJEdge{
			RelType:     getField(record, colIndex, "rel_type"),
			NodeIDStart: getField(record, colIndex, "node_id_start"),
			NodeIDEnd:   getField(record, colIndex, "node_id_end"),
			Link:        getField(record, colIndex, "link"),
			Status:      getField(record, colIndex, "status"),
			StartDate:   getField(record, colIndex, "start_date"),
			EndDate:     getField(record, colIndex, "end_date"),
		}

		edgeBatch = append(edgeBatch, edge)

		// Process batch when full
		if len(edgeBatch) >= batchSize {
			imported, skippedInBatch := processEdgeBatch(graph, edgeBatch, nodeIDMap, logger)
			count += imported
			skipped += skippedInBatch
			logger.Info("progress", "edges_imported", count, "skipped", skipped)
			edgeBatch = edgeBatch[:0]
		}
	}

	// Process remaining batch
	if len(edgeBatch) > 0 {
		imported, skippedInBatch := processEdgeBatch(graph, edgeBatch, nodeIDMap, logger)
		count += imported
		skipped += skippedInBatch
	}

	logger.Info("edge import summary", "imported", count, "skipped", skipped)

	return count, time.Since(start)
}

func processEdgeBatch(graph *storage.GraphStorage, batch []ICIJEdge, nodeIDMap map[string]uint64, logger *slog.Logger) (int, int) {
	graphBatch := graph.BeginBatch()
	imported := 0
	skipped := 0

	for _, edge := range batch {
		// Map ICIJ node IDs to GraphDB node IDs
		fromID, ok1 := nodeIDMap[edge.NodeIDStart]
		toID, ok2 := nodeIDMap[edge.NodeIDEnd]

		if !ok1 || !ok2 {
			skipped++
			continue
		}

		// Create edge with properties (skip empty strings)
		props := make(map[string]storage.Value)
		if edge.Link != "" {
			props["link"] = storage.StringValue(edge.Link)
		}
		if edge.Status != "" {
			props["status"] = storage.StringValue(edge.Status)
		}
		if edge.StartDate != "" {
			props["start_date"] = storage.StringValue(edge.StartDate)
		}
		if edge.EndDate != "" {
			props["end_date"] = storage.StringValue(edge.EndDate)
		}

		edgeType := edge.RelType
		if edgeType == "" {
			edgeType = "RELATED_TO"
		}

		_, err := graphBatch.AddEdge(fromID, toID, edgeType, props, 1.0)
		if err != nil {
			logger.Error("failed to add edge to batch", "error", err,
				"from", edge.NodeIDStart, "to", edge.NodeIDEnd, "type", edgeType)
			skipped++
			continue
		}

		imported++
	}

	// Commit the entire batch atomically
	if err := graphBatch.Commit(); err != nil {
		logger.Error("failed to commit edge batch", "error", err)
		return 0, len(batch)
	}

	return imported, skipped
}

func getField(record []string, colIndex map[string]int, field string) string {
	if idx, ok := colIndex[field]; ok && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}

func saveNodeIDMap(nodeIDMap map[string]uint64, logger *slog.Logger) {
	file, err := os.Create("node_id_mapping.csv")
	if err != nil {
		logger.Error("failed to save node ID mapping", "error", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"icij_id", "graphdb_id"})

	for icijID, graphdbID := range nodeIDMap {
		writer.Write([]string{icijID, strconv.FormatUint(graphdbID, 10)})
	}

	logger.Info("saved node ID mapping", "count", len(nodeIDMap))
}

func loadNodeIDMap(logger *slog.Logger) map[string]uint64 {
	nodeIDMap := make(map[string]uint64)

	file, err := os.Open("node_id_mapping.csv")
	if err != nil {
		logger.Warn("could not load node ID mapping", "error", err)
		return nodeIDMap
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Read() // Skip header

	for {
		record, err := reader.Read()
		if err != nil {
			break
		}

		if len(record) == 2 {
			icijID := record[0]
			graphdbID, _ := strconv.ParseUint(record[1], 10, 64)
			nodeIDMap[icijID] = graphdbID
		}
	}

	logger.Info("loaded node ID mapping", "count", len(nodeIDMap))
	return nodeIDMap
}

func showNodeTypeDistribution(graph *storage.GraphStorage, logger *slog.Logger) {
	// Query each node type
	types := []string{"Entity", "Officer", "Intermediary", "Address"}

	for _, nodeType := range types {
		// This is a simplified query - in production you'd use proper label filtering
		// For now, we'll just show the total count
		logger.Info("node type", "type", nodeType)
	}
}
