package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/queryutil"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

type CLI struct {
	graph    *storage.GraphStorage
	executor *query.Executor
	scanner  *bufio.Scanner
}

func main() {
	dataDir := flag.String("data", "./data/cli", "Data directory")
	flag.Parse()

	printBanner()

	// Initialize graph storage
	fmt.Printf("📂 Opening database at %s...\n", *dataDir)
	graph, err := storage.NewGraphStorage(*dataDir)
	if err != nil {
		fmt.Printf("❌ Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer graph.Close()

	stats := graph.GetStatistics()
	fmt.Printf("✅ Database loaded\n")
	fmt.Printf("   Nodes: %d\n", stats.NodeCount)
	fmt.Printf("   Edges: %d\n\n", stats.EdgeCount)

	cli := &CLI{
		graph:    graph,
		executor: queryutil.WireCapabilities(query.NewExecutor(graph), graph),
		scanner:  bufio.NewScanner(os.Stdin),
	}

	fmt.Println("Type 'help' for available commands, 'exit' to quit")
	fmt.Println()

	cli.run()
}

func printBanner() {
	banner := `
╔═══════════════════════════════════════════════════════════╗
║                                                           ║
║   ██████╗██╗     ██╗   ██╗███████╗ ██████╗               ║
║  ██╔════╝██║     ██║   ██║██╔════╝██╔═══██╗              ║
║  ██║     ██║     ██║   ██║███████╗██║   ██║              ║
║  ██║     ██║     ██║   ██║╚════██║██║   ██║              ║
║  ╚██████╗███████╗╚██████╔╝███████║╚██████╔╝              ║
║   ╚═════╝╚══════╝ ╚═════╝ ╚══════╝ ╚═════╝               ║
║                                                           ║
║              GraphDB Interactive CLI v1.0                 ║
║                                                           ║
╚═══════════════════════════════════════════════════════════╝
`
	fmt.Println(banner)
}

func (cli *CLI) run() {
	for {
		fmt.Print("cluso> ")

		if !cli.scanner.Scan() {
			break
		}

		input := strings.TrimSpace(cli.scanner.Text())
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("👋 Goodbye!")
			break
		}

		cli.executeCommand(input)
		fmt.Println()
	}
}

func (cli *CLI) executeCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "help":
		cli.showHelp()

	case "stats", "status":
		cli.showStats()

	case "query", "q":
		if len(parts) < 2 {
			fmt.Println("Usage: query <cypher-query>")
			return
		}
		queryStr := strings.Join(parts[1:], " ")
		cli.executeQuery(queryStr)

	case "create-node", "cn":
		cli.createNodeInteractive()

	case "create-edge", "ce":
		cli.createEdgeInteractive()

	case "list-nodes", "ln":
		cli.listNodes()

	case "get-node", "gn":
		if len(parts) < 2 {
			fmt.Println("Usage: get-node <node-id>")
			return
		}
		nodeID, _ := strconv.ParseUint(parts[1], 10, 64)
		cli.getNode(nodeID)

	case "neighbors":
		if len(parts) < 2 {
			fmt.Println("Usage: neighbors <node-id>")
			return
		}
		nodeID, _ := strconv.ParseUint(parts[1], 10, 64)
		cli.showNeighbors(nodeID)

	case "traverse":
		if len(parts) < 3 {
			fmt.Println("Usage: traverse <node-id> <max-depth>")
			return
		}
		nodeID, _ := strconv.ParseUint(parts[1], 10, 64)
		maxDepth, _ := strconv.Atoi(parts[2])
		cli.traverse(nodeID, maxDepth)

	case "path":
		if len(parts) < 3 {
			fmt.Println("Usage: path <from-id> <to-id>")
			return
		}
		fromID, _ := strconv.ParseUint(parts[1], 10, 64)
		toID, _ := strconv.ParseUint(parts[2], 10, 64)
		cli.findPath(fromID, toID)

	case "pagerank", "pr":
		cli.runPageRank()

	case "betweenness", "bc":
		cli.runBetweenness()

	case "demo":
		cli.runDemo()

	case "clear":
		fmt.Print("\033[H\033[2J")

	default:
		fmt.Printf("❌ Unknown command: %s (type 'help' for available commands)\n", command)
	}
}

func (cli *CLI) showHelp() {
	help := `
📖 Available Commands:

🔍 Query & Inspection:
  query <query>          Execute a Cypher-like query
  q <query>             Shorthand for query
  stats                 Show database statistics
  list-nodes            List all nodes
  ln                    Shorthand for list-nodes
  get-node <id>         Get details of a specific node
  gn <id>               Shorthand for get-node
  neighbors <id>        Show neighbors of a node

🛠️  Data Manipulation:
  create-node           Interactive node creation
  cn                    Shorthand for create-node
  create-edge           Interactive edge creation
  ce                    Shorthand for create-edge

🌐 Graph Operations:
  traverse <id> <depth> Traverse graph from node
  path <from> <to>      Find shortest path between nodes

📊 Algorithms:
  pagerank              Run PageRank algorithm
  pr                    Shorthand for pagerank
  betweenness           Run Betweenness Centrality
  bc                    Shorthand for betweenness

🎮 Other:
  demo                  Run interactive demo
  clear                 Clear screen
  help                  Show this help
  exit/quit             Exit the CLI

💡 Examples:
  query MATCH (n:Person) RETURN n
  create-node
  neighbors 1
  path 1 5
  pagerank
`
	fmt.Println(help)
}

func (cli *CLI) showStats() {
	stats := cli.graph.GetStatistics()

	fmt.Println("📊 Database Statistics:")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Nodes:        %d\n", stats.NodeCount)
	fmt.Printf("  Edges:        %d\n", stats.EdgeCount)
	fmt.Printf("  Total Queries: %d\n", stats.TotalQueries)
	if stats.AvgQueryTime > 0 {
		fmt.Printf("  Avg Query Time: %.2fms\n", stats.AvgQueryTime)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func (cli *CLI) executeQuery(queryStr string) {
	start := time.Now()

	// Parse query
	lexer := query.NewLexer(queryStr)
	tokens, err := lexer.Tokenize()
	if err != nil {
		fmt.Printf("❌ Lexer error: %v\n", err)
		return
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		fmt.Printf("❌ Parser error: %v\n", err)
		return
	}

	// Execute query
	results, err := cli.executor.Execute(parsedQuery)
	if err != nil {
		fmt.Printf("❌ Execution error: %v\n", err)
		return
	}

	// Display results
	fmt.Printf("✅ Query executed in %v\n\n", time.Since(start))

	if len(results.Columns) > 0 {
		// Print header
		for _, col := range results.Columns {
			fmt.Printf("%-20s ", col)
		}
		fmt.Println()
		fmt.Println(strings.Repeat("─", len(results.Columns)*21))

		// Print rows
		for _, row := range results.Rows {
			for _, col := range results.Columns {
				fmt.Printf("%-20v ", row[col])
			}
			fmt.Println()
		}

		fmt.Printf("\n%d rows\n", results.Count)
	} else {
		fmt.Printf("Query affected %d items\n", results.Count)
	}
}

func (cli *CLI) createNodeInteractive() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🆕 Create New Node")
	fmt.Println("━━━━━━━━━━━━━━━━━━")

	fmt.Print("Labels (comma-separated): ")
	labelsStr, _ := reader.ReadString('\n')
	labelsStr = strings.TrimSpace(labelsStr)
	labels := strings.Split(labelsStr, ",")
	for i := range labels {
		labels[i] = strings.TrimSpace(labels[i])
	}

	properties := make(map[string]storage.Value)
	fmt.Println("\nProperties (enter empty key to finish):")
	for {
		fmt.Print("  Key: ")
		key, _ := reader.ReadString('\n')
		key = strings.TrimSpace(key)
		if key == "" {
			break
		}

		fmt.Print("  Value: ")
		value, _ := reader.ReadString('\n')
		value = strings.TrimSpace(value)

		properties[key] = storage.StringValue(value)
	}

	node, err := cli.graph.CreateNode(labels, properties)
	if err != nil {
		fmt.Printf("❌ Failed to create node: %v\n", err)
		return
	}

	fmt.Printf("\n✅ Created node with ID: %d\n", node.ID)
	fmt.Printf("   Labels: %v\n", node.Labels)
	fmt.Printf("   Properties: %v\n", node.Properties)
}

func (cli *CLI) createEdgeInteractive() {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🔗 Create New Edge")
	fmt.Println("━━━━━━━━━━━━━━━━━━")

	fmt.Print("From Node ID: ")
	fromStr, _ := reader.ReadString('\n')
	fromID, _ := strconv.ParseUint(strings.TrimSpace(fromStr), 10, 64)

	fmt.Print("To Node ID: ")
	toStr, _ := reader.ReadString('\n')
	toID, _ := strconv.ParseUint(strings.TrimSpace(toStr), 10, 64)

	fmt.Print("Edge Type: ")
	edgeType, _ := reader.ReadString('\n')
	edgeType = strings.TrimSpace(edgeType)

	fmt.Print("Weight (default 1.0): ")
	weightStr, _ := reader.ReadString('\n')
	weightStr = strings.TrimSpace(weightStr)
	weight := 1.0
	if weightStr != "" {
		weight, _ = strconv.ParseFloat(weightStr, 64)
	}

	edge, err := cli.graph.CreateEdge(fromID, toID, edgeType, nil, weight)
	if err != nil {
		fmt.Printf("❌ Failed to create edge: %v\n", err)
		return
	}

	fmt.Printf("\n✅ Created edge with ID: %d\n", edge.ID)
	fmt.Printf("   %d -[%s]-> %d (weight: %.2f)\n", fromID, edgeType, toID, weight)
}

func (cli *CLI) listNodes() {
	stats := cli.graph.GetStatistics()

	fmt.Printf("📋 All Nodes (total: %d)\n", stats.NodeCount)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	count := 0
	for nodeID := uint64(1); nodeID <= stats.NodeCount; nodeID++ {
		node, err := cli.graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		fmt.Printf("  [%d] Labels: %v", node.ID, node.Labels)
		if len(node.Properties) > 0 {
			fmt.Printf(" Properties: %d", len(node.Properties))
		}
		fmt.Println()

		count++
		if count >= 50 {
			fmt.Printf("  ... and %d more nodes\n", stats.NodeCount-uint64(count))
			break
		}
	}
}

func (cli *CLI) getNode(nodeID uint64) {
	node, err := cli.graph.GetNode(nodeID)
	if err != nil {
		fmt.Printf("❌ Node %d not found\n", nodeID)
		return
	}

	fmt.Printf("🔍 Node Details (ID: %d)\n", nodeID)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Labels: %v\n", node.Labels)
	fmt.Println("\nProperties:")
	for key, value := range node.Properties {
		fmt.Printf("  %s: %v\n", key, value.Data)
	}

	// Show edges
	outgoing, _ := cli.graph.GetOutgoingEdges(nodeID)
	incoming, _ := cli.graph.GetIncomingEdges(nodeID)

	fmt.Printf("\nOutgoing Edges: %d\n", len(outgoing))
	for i, edge := range outgoing {
		if i < 10 {
			fmt.Printf("  -[%s]-> %d\n", edge.Type, edge.ToNodeID)
		}
	}
	if len(outgoing) > 10 {
		fmt.Printf("  ... and %d more\n", len(outgoing)-10)
	}

	fmt.Printf("\nIncoming Edges: %d\n", len(incoming))
	for i, edge := range incoming {
		if i < 10 {
			fmt.Printf("  %d -[%s]->\n", edge.FromNodeID, edge.Type)
		}
	}
	if len(incoming) > 10 {
		fmt.Printf("  ... and %d more\n", len(incoming)-10)
	}
}

func (cli *CLI) showNeighbors(nodeID uint64) {
	outgoing, err := cli.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		fmt.Printf("❌ Node %d not found\n", nodeID)
		return
	}

	fmt.Printf("👥 Neighbors of Node %d\n", nodeID)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if len(outgoing) == 0 {
		fmt.Println("  No outgoing edges")
		return
	}

	for i, edge := range outgoing {
		if i >= 20 {
			fmt.Printf("  ... and %d more neighbors\n", len(outgoing)-i)
			break
		}

		neighbor, _ := cli.graph.GetNode(edge.ToNodeID)
		if neighbor != nil {
			fmt.Printf("  [%d] -[%s]-> %v\n", edge.ToNodeID, edge.Type, neighbor.Labels)
		}
	}
}

func (cli *CLI) traverse(nodeID uint64, maxDepth int) {
	start := time.Now()

	visited := make(map[uint64]bool)
	nodes := make([]*storage.Node, 0)

	var traverseFrom func(uint64, int)
	traverseFrom = func(id uint64, depth int) {
		if depth > maxDepth || visited[id] {
			return
		}

		visited[id] = true
		node, err := cli.graph.GetNode(id)
		if err != nil {
			return
		}
		nodes = append(nodes, node)

		edges, _ := cli.graph.GetOutgoingEdges(id)
		for _, edge := range edges {
			traverseFrom(edge.ToNodeID, depth+1)
		}
	}

	traverseFrom(nodeID, 0)

	fmt.Printf("🌐 Traversal from Node %d (max depth: %d)\n", nodeID, maxDepth)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Found %d nodes in %v\n\n", len(nodes), time.Since(start))

	for i, node := range nodes {
		if i < 20 {
			fmt.Printf("  [%d] %v\n", node.ID, node.Labels)
		}
	}
	if len(nodes) > 20 {
		fmt.Printf("  ... and %d more nodes\n", len(nodes)-20)
	}
}

func (cli *CLI) findPath(fromID, toID uint64) {
	start := time.Now()

	path, err := algorithms.ShortestPath(cli.graph, fromID, toID)
	if err != nil || len(path) == 0 {
		fmt.Printf("❌ No path found from %d to %d\n", fromID, toID)
		return
	}

	fmt.Printf("🛤️  Shortest Path: %d → %d\n", fromID, toID)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Length: %d hops\n", len(path)-1)
	fmt.Printf("Time: %v\n\n", time.Since(start))

	fmt.Print("Path: ")
	for i, nodeID := range path {
		if i > 0 {
			fmt.Print(" → ")
		}
		fmt.Printf("%d", nodeID)
	}
	fmt.Println()
}

func (cli *CLI) runPageRank() {
	start := time.Now()

	opts := algorithms.PageRankOptions{
		MaxIterations: 20,
		DampingFactor: 0.85,
		Tolerance:     1e-6,
	}

	result, err := algorithms.PageRank(cli.graph, opts)
	if err != nil {
		fmt.Printf("❌ PageRank error: %v\n", err)
		return
	}

	fmt.Println("📊 PageRank Results")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Iterations: %d\n", result.Iterations)
	fmt.Printf("Converged: %v\n", result.Converged)
	fmt.Printf("Time: %v\n\n", time.Since(start))

	fmt.Println("Top 10 Nodes:")
	count := 0
	for _, ranked := range result.TopNodes {
		if count >= 10 {
			break
		}
		fmt.Printf("  #%d: Node %d (score: %.6f)\n", count+1, ranked.NodeID, ranked.Score)
		count++
	}
}

func (cli *CLI) runBetweenness() {
	start := time.Now()

	scores, err := algorithms.BetweennessCentrality(cli.graph)
	if err != nil {
		fmt.Printf("❌ Betweenness error: %v\n", err)
		return
	}

	fmt.Println("📊 Betweenness Centrality Results")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Time: %v\n\n", time.Since(start))

	// Sort and show top 10
	type scored struct {
		id    uint64
		score float64
	}
	sorted := make([]scored, 0, len(scores))
	for id, score := range scores {
		sorted = append(sorted, scored{id, score})
	}

	// Simple bubble sort for top 10
	for i := 0; i < len(sorted) && i < 10; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].score > sorted[i].score {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	fmt.Println("Top 10 Nodes:")
	for i := 0; i < 10 && i < len(sorted); i++ {
		fmt.Printf("  #%d: Node %d (score: %.6f)\n", i+1, sorted[i].id, sorted[i].score)
	}
}

func (cli *CLI) runDemo() {
	fmt.Println("🎮 Running Interactive Demo...")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Create sample social network
	fmt.Println("Step 1: Creating sample social network...")

	people := []struct {
		name string
		age  int64
	}{
		{"Alice", 30},
		{"Bob", 25},
		{"Charlie", 35},
		{"Diana", 28},
		{"Eve", 32},
	}

	nodeIDs := make([]uint64, 0)
	for _, person := range people {
		node, _ := cli.graph.CreateNode(
			[]string{"Person"},
			map[string]storage.Value{
				"name": storage.StringValue(person.name),
				"age":  storage.IntValue(person.age),
			},
		)
		nodeIDs = append(nodeIDs, node.ID)
		fmt.Printf("  Created: %s (ID: %d)\n", person.name, node.ID)
	}

	fmt.Println("\nStep 2: Creating friendships...")
	connections := [][2]int{
		{0, 1}, // Alice -> Bob
		{1, 2}, // Bob -> Charlie
		{2, 3}, // Charlie -> Diana
		{3, 4}, // Diana -> Eve
		{0, 2}, // Alice -> Charlie
		{1, 4}, // Bob -> Eve
	}

	for _, conn := range connections {
		cli.graph.CreateEdge(nodeIDs[conn[0]], nodeIDs[conn[1]], "KNOWS", nil, 1.0)
		fmt.Printf("  %s -> %s\n", people[conn[0]].name, people[conn[1]].name)
	}

	fmt.Println("\n✅ Demo data created!")
	fmt.Println("\n💡 Try these commands:")
	fmt.Println("  query MATCH (p:Person) RETURN p")
	fmt.Println("  neighbors", nodeIDs[0])
	fmt.Println("  path", nodeIDs[0], nodeIDs[4])
	fmt.Println("  pagerank")
}
