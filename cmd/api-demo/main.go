package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/api"
)

const baseURL = "http://localhost:8080"

func main() {
	fmt.Printf("🔥 Cluso GraphDB API Demo\n")
	fmt.Printf("=========================\n\n")

	// Wait for server to be ready
	fmt.Printf("⏳ Waiting for server to be ready...\n")
	if !waitForServer() {
		log.Fatal("Server not available")
	}
	fmt.Printf("✅ Server is ready\n\n")

	// 1. Health Check
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 1: Health Check\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testHealthCheck()

	// 2. Metrics
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 2: Metrics\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testMetrics()

	// 3. Create Nodes
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 3: Create Nodes\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	node1, node2, node3 := testCreateNodes()

	// 4. Create Edges
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 4: Create Edges\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testCreateEdges(node1, node2, node3)

	// 5. Get Node
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 5: Get Node\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testGetNode(node1)

	// 6. List Nodes
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 6: List All Nodes\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testListNodes()

	// 7. Traversal
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 7: Graph Traversal\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testTraversal(node1)

	// 8. Shortest Path
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 8: Shortest Path\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testShortestPath(node1, node3)

	// 9. Query Language
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 9: Query Language\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testQueryLanguage()

	// 10. Batch Operations
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 10: Batch Operations\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testBatchOperations()

	// 11. PageRank Algorithm
	fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("📊 Test 11: PageRank Algorithm\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	testPageRank()

	fmt.Printf("\n✅ All API tests completed successfully!\n")
}

func waitForServer() bool {
	for i := 0; i < 30; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func testHealthCheck() {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		log.Printf("❌ Health check failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var health api.HealthResponse
	json.NewDecoder(resp.Body).Decode(&health)

	fmt.Printf("Status: %s\n", health.Status)
	fmt.Printf("Version: %s\n", health.Version)
	fmt.Printf("Uptime: %s\n", health.Uptime)
	fmt.Printf("✅ Health check passed\n")
}

func testMetrics() {
	resp, err := http.Get(baseURL + "/metrics")
	if err != nil {
		log.Printf("❌ Metrics failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var metrics api.MetricsResponse
	json.NewDecoder(resp.Body).Decode(&metrics)

	fmt.Printf("Nodes: %d\n", metrics.NodeCount)
	fmt.Printf("Edges: %d\n", metrics.EdgeCount)
	fmt.Printf("Queries: %d\n", metrics.TotalQueries)
	fmt.Printf("✅ Metrics retrieved\n")
}

func testCreateNodes() (uint64, uint64, uint64) {
	nodes := []api.NodeRequest{
		{
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": "Alice",
				"age":  float64(30),
			},
		},
		{
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": "Bob",
				"age":  float64(25),
			},
		},
		{
			Labels: []string{"Person"},
			Properties: map[string]any{
				"name": "Charlie",
				"age":  float64(35),
			},
		},
	}

	var nodeIDs []uint64

	for i, nodeReq := range nodes {
		data, _ := json.Marshal(nodeReq)
		resp, err := http.Post(baseURL+"/nodes", "application/json", bytes.NewBuffer(data))
		if err != nil {
			log.Printf("❌ Create node failed: %v", err)
			continue
		}
		defer resp.Body.Close()

		var node api.NodeResponse
		json.NewDecoder(resp.Body).Decode(&node)
		nodeIDs = append(nodeIDs, node.ID)

		fmt.Printf("Created node %d: %s (age: %.0f) with ID %d\n",
			i+1, node.Properties["name"], node.Properties["age"], node.ID)
	}

	fmt.Printf("✅ Created %d nodes\n", len(nodeIDs))

	if len(nodeIDs) >= 3 {
		return nodeIDs[0], nodeIDs[1], nodeIDs[2]
	}
	return 0, 0, 0
}

func testCreateEdges(node1, node2, node3 uint64) {
	edges := []api.EdgeRequest{
		{
			FromNodeID: node1,
			ToNodeID:   node2,
			Type:       "KNOWS",
			Properties: map[string]any{"since": float64(2020)},
			Weight:     1.0,
		},
		{
			FromNodeID: node2,
			ToNodeID:   node3,
			Type:       "KNOWS",
			Properties: map[string]any{"since": float64(2021)},
			Weight:     1.0,
		},
		{
			FromNodeID: node1,
			ToNodeID:   node3,
			Type:       "KNOWS",
			Properties: map[string]any{"since": float64(2019)},
			Weight:     0.8,
		},
	}

	for i, edgeReq := range edges {
		data, _ := json.Marshal(edgeReq)
		resp, err := http.Post(baseURL+"/edges", "application/json", bytes.NewBuffer(data))
		if err != nil {
			log.Printf("❌ Create edge failed: %v", err)
			continue
		}
		defer resp.Body.Close()

		var edge api.EdgeResponse
		json.NewDecoder(resp.Body).Decode(&edge)

		fmt.Printf("Created edge %d: %d -[%s]-> %d\n",
			i+1, edge.FromNodeID, edge.Type, edge.ToNodeID)
	}

	fmt.Printf("✅ Created %d edges\n", len(edges))
}

func testGetNode(nodeID uint64) {
	resp, err := http.Get(fmt.Sprintf("%s/nodes/%d", baseURL, nodeID))
	if err != nil {
		log.Printf("❌ Get node failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var node api.NodeResponse
	json.NewDecoder(resp.Body).Decode(&node)

	fmt.Printf("Node ID: %d\n", node.ID)
	fmt.Printf("Labels: %v\n", node.Labels)
	fmt.Printf("Properties: %v\n", node.Properties)
	fmt.Printf("✅ Retrieved node\n")
}

func testListNodes() {
	resp, err := http.Get(baseURL + "/nodes")
	if err != nil {
		log.Printf("❌ List nodes failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var nodes []*api.NodeResponse
	json.NewDecoder(resp.Body).Decode(&nodes)

	fmt.Printf("Total nodes: %d\n", len(nodes))
	for i, node := range nodes {
		if i < 5 {
			fmt.Printf("  Node %d: ID=%d, Labels=%v\n", i+1, node.ID, node.Labels)
		}
	}
	fmt.Printf("✅ Listed all nodes\n")
}

func testTraversal(startNodeID uint64) {
	req := api.TraversalRequest{
		StartNodeID: startNodeID,
		MaxDepth:    3,
		Direction:   "outgoing",
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/traverse", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("❌ Traversal failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result api.TraversalResponse
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Starting from node %d\n", startNodeID)
	fmt.Printf("Found %d nodes in traversal\n", result.Count)
	fmt.Printf("Time: %s\n", result.Time)
	fmt.Printf("✅ Traversal completed\n")
}

func testShortestPath(startNodeID, endNodeID uint64) {
	req := api.ShortestPathRequest{
		StartNodeID: startNodeID,
		EndNodeID:   endNodeID,
		MaxDepth:    10,
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/shortest-path", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("❌ Shortest path failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result api.ShortestPathResponse
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("From: %d -> To: %d\n", startNodeID, endNodeID)
	if result.Found {
		fmt.Printf("Path: %v\n", result.Path)
		fmt.Printf("Length: %d hops\n", result.Length-1)
	} else {
		fmt.Printf("No path found\n")
	}
	fmt.Printf("Time: %s\n", result.Time)
	fmt.Printf("✅ Shortest path completed\n")
}

func testQueryLanguage() {
	req := api.QueryRequest{
		Query: "MATCH (p:Person) RETURN p",
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/query", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("❌ Query failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Query failed: %s\n", string(body))
		return
	}

	var result api.QueryResponse
	json.Unmarshal(body, &result)

	fmt.Printf("Query: %s\n", req.Query)
	fmt.Printf("Columns: %v\n", result.Columns)
	fmt.Printf("Rows: %d\n", result.Count)
	fmt.Printf("Time: %s\n", result.Time)
	fmt.Printf("✅ Query executed\n")
}

func testBatchOperations() {
	req := api.BatchNodeRequest{
		Nodes: []api.NodeRequest{
			{
				Labels:     []string{"Product"},
				Properties: map[string]any{"name": "Laptop", "price": float64(999)},
			},
			{
				Labels:     []string{"Product"},
				Properties: map[string]any{"name": "Mouse", "price": float64(29)},
			},
			{
				Labels:     []string{"Product"},
				Properties: map[string]any{"name": "Keyboard", "price": float64(79)},
			},
		},
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/nodes/batch", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("❌ Batch create failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result api.BatchNodeResponse
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Created %d nodes in batch\n", result.Created)
	fmt.Printf("Time: %s\n", result.Time)
	fmt.Printf("✅ Batch operation completed\n")
}

func testPageRank() {
	req := api.AlgorithmRequest{
		Algorithm: "pagerank",
		Parameters: map[string]any{
			"iterations":     float64(10),
			"damping_factor": 0.85,
		},
	}

	data, _ := json.Marshal(req)
	resp, err := http.Post(baseURL+"/algorithms", "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Printf("❌ PageRank failed: %v", err)
		return
	}
	defer resp.Body.Close()

	var result api.AlgorithmResponse
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Printf("Algorithm: %s\n", result.Algorithm)
	fmt.Printf("Time: %s\n", result.Time)
	if scores, ok := result.Results["scores"].(map[string]any); ok {
		fmt.Printf("Computed scores for %d nodes\n", len(scores))
	}
	fmt.Printf("✅ PageRank completed\n")
}
