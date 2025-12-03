package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestHandleTraversal tests graph traversal endpoint
func TestHandleTraversal(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create test graph: A -> B -> C
	//                     A -> D
	nodeA, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})
	nodeC, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("C"),
	})
	nodeD, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("D"),
	})

	server.graph.CreateEdge(nodeA.ID, nodeB.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeB.ID, nodeC.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeA.ID, nodeD.ID, "LINKED", map[string]storage.Value{}, 1.0)

	tests := []struct {
		name         string
		request      TraversalRequest
		expectStatus int
		expectError  bool
		minNodes     int // minimum nodes expected in traversal
	}{
		{
			name: "Traversal from A with max depth 2",
			request: TraversalRequest{
				StartNodeID: nodeA.ID,
				MaxDepth:    2,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			minNodes:     3, // Should find A, B, D at minimum
		},
		{
			name: "Traversal with depth limit 1",
			request: TraversalRequest{
				StartNodeID: nodeA.ID,
				MaxDepth:    1,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			minNodes:     2, // Should find A and its immediate neighbors
		},
		{
			name: "Traversal from leaf node",
			request: TraversalRequest{
				StartNodeID: nodeC.ID,
				MaxDepth:    2,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			minNodes:     1, // Only C itself (no outgoing edges)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/traverse", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleTraversal(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response TraversalResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if len(response.Nodes) < tt.minNodes {
					t.Errorf("Expected at least %d nodes, got %d", tt.minNodes, len(response.Nodes))
				}

				t.Logf("✓ Found %d nodes in traversal", len(response.Nodes))
			}
		})
	}
}

// TestHandleShortestPath tests shortest path algorithm
func TestHandleShortestPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create test graph: A -> B -> C -> D
	//                     A -> E -> D
	nodeA, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})
	nodeC, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("C"),
	})
	nodeD, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("D"),
	})
	nodeE, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("E"),
	})

	server.graph.CreateEdge(nodeA.ID, nodeB.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeB.ID, nodeC.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeC.ID, nodeD.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeA.ID, nodeE.ID, "LINKED", map[string]storage.Value{}, 1.0)
	server.graph.CreateEdge(nodeE.ID, nodeD.ID, "LINKED", map[string]storage.Value{}, 1.0)

	tests := []struct {
		name         string
		request      ShortestPathRequest
		expectStatus int
		expectError  bool
		maxPathLen   int // maximum path length expected
	}{
		{
			name: "Shortest path A to D (via E)",
			request: ShortestPathRequest{
				StartNodeID: nodeA.ID,
				EndNodeID:   nodeD.ID,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			maxPathLen:   3, // A -> E -> D (should be shorter than A -> B -> C -> D)
		},
		{
			name: "Shortest path A to B",
			request: ShortestPathRequest{
				StartNodeID: nodeA.ID,
				EndNodeID:   nodeB.ID,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			maxPathLen:   2, // A -> B
		},
		{
			name: "Same source and target",
			request: ShortestPathRequest{
				StartNodeID: nodeA.ID,
				EndNodeID:   nodeA.ID,
			},
			expectStatus: http.StatusOK,
			expectError:  false,
			maxPathLen:   1, // Just the source node
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/shortest-path", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleShortestPath(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response ShortestPathResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if len(response.Path) > tt.maxPathLen {
					t.Errorf("Expected path length <= %d, got %d", tt.maxPathLen, len(response.Path))
				}

				t.Logf("✓ Found path of length %d", len(response.Path))
			}
		})
	}
}

// TestHandleAlgorithm tests general algorithm endpoint
func TestHandleAlgorithm(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create test graph (circle: A -> B -> C -> D -> E -> A)
	nodes := make([]*storage.Node, 5)
	for i := 0; i < 5; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodes[i] = node
	}

	// Create edges in a circle
	for i := 0; i < 5; i++ {
		nextIdx := (i + 1) % 5
		server.graph.CreateEdge(nodes[i].ID, nodes[nextIdx].ID, "LINKED", map[string]storage.Value{}, 1.0)
	}

	tests := []struct {
		name         string
		request      AlgorithmRequest
		expectStatus int
		expectError  bool
	}{
		{
			name: "PageRank algorithm",
			request: AlgorithmRequest{
				Algorithm: "pagerank",
				Parameters: map[string]any{
					"iterations": 10,
				},
			},
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name: "Betweenness centrality",
			request: AlgorithmRequest{
				Algorithm:  "betweenness",
				Parameters: map[string]any{},
			},
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name: "Detect cycles",
			request: AlgorithmRequest{
				Algorithm:  "detect_cycles",
				Parameters: map[string]any{},
			},
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name: "Has cycle check",
			request: AlgorithmRequest{
				Algorithm:  "has_cycle",
				Parameters: map[string]any{},
			},
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name: "Invalid algorithm name",
			request: AlgorithmRequest{
				Algorithm:  "nonexistent_algorithm",
				Parameters: map[string]any{},
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/algorithms", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleAlgorithm(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response AlgorithmResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				t.Logf("✓ Algorithm %q executed successfully", tt.request.Algorithm)
			}
		})
	}
}

// TestHandleAlgorithm_LargeGraph tests algorithm performance on larger graphs
func TestHandleAlgorithm_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large graph test in short mode")
	}

	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a larger graph (100 nodes)
	nodes := make([]*storage.Node, 100)
	for i := 0; i < 100; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id":    storage.IntValue(int64(i)),
			"value": storage.IntValue(int64(i * 10)),
		})
		nodes[i] = node
	}

	// Create random edges
	for i := 0; i < 200; i++ {
		sourceIdx := i % 100
		targetIdx := (i + 1) % 100
		server.graph.CreateEdge(nodes[sourceIdx].ID, nodes[targetIdx].ID, "LINKED", map[string]storage.Value{}, 1.0)
	}

	request := AlgorithmRequest{
		Algorithm: "pagerank",
		Parameters: map[string]any{
			"iterations": 20,
		},
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest(http.MethodPost, "/algorithms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleAlgorithm(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Algorithm failed on large graph: status %d, body: %s",
			rr.Code, rr.Body.String())
	}

	t.Logf("✓ Algorithm handled 100-node graph successfully")
}
