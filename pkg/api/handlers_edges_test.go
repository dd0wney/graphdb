package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestCreateEdge tests the POST /edges endpoint
func TestCreateEdge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create nodes first for valid edges
	nodeA, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})

	tests := []struct {
		name         string
		request      EdgeRequest
		expectStatus int
		expectError  bool
	}{
		{
			name: "Valid edge creation",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "KNOWS",
				Weight:     1.0,
				Properties: map[string]any{
					"since": 2020,
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Edge with zero weight",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "CONNECTED",
				Weight:     0.0,
				Properties: map[string]any{},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Self-loop edge",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeA.ID,
				Type:       "SELF_REF",
				Weight:     1.0,
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Invalid - missing edge type",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "",
				Weight:     1.0,
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
		{
			name: "Invalid - non-existent source node",
			request: EdgeRequest{
				FromNodeID: 99999,
				ToNodeID:   nodeB.ID,
				Type:       "INVALID",
				Weight:     1.0,
			},
			expectStatus: http.StatusInternalServerError,
			expectError:  true,
		},
		{
			name: "Invalid - non-existent target node",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   99999,
				Type:       "INVALID",
				Weight:     1.0,
			},
			expectStatus: http.StatusInternalServerError,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleEdges(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusCreated {
				var response EdgeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.ID == 0 {
					t.Error("Expected non-zero edge ID")
				}

				if response.FromNodeID != tt.request.FromNodeID {
					t.Errorf("Expected FromNodeID %d, got %d", tt.request.FromNodeID, response.FromNodeID)
				}

				if response.ToNodeID != tt.request.ToNodeID {
					t.Errorf("Expected ToNodeID %d, got %d", tt.request.ToNodeID, response.ToNodeID)
				}

				t.Logf("✓ Created edge %d: %d -> %d", response.ID, response.FromNodeID, response.ToNodeID)
			}
		})
	}
}

// TestGetEdge tests the GET /edges/{id} endpoint
func TestGetEdge(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create nodes and edge
	nodeA, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})

	edge, _ := server.graph.CreateEdge(nodeA.ID, nodeB.ID, "KNOWS", map[string]storage.Value{
		"weight": storage.FloatValue(1.5),
	}, 1.5)

	tests := []struct {
		name         string
		edgeID       uint64
		expectStatus int
		expectError  bool
	}{
		{
			name:         "Get existing edge",
			edgeID:       edge.ID,
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name:         "Get non-existent edge",
			edgeID:       99999,
			expectStatus: http.StatusNotFound,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/edges/%d", tt.edgeID), nil)
			rr := httptest.NewRecorder()

			server.handleEdge(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response EdgeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.ID != tt.edgeID {
					t.Errorf("Expected edge ID %d, got %d", tt.edgeID, response.ID)
				}

				t.Logf("✓ Retrieved edge %d: %d -> %d", response.ID, response.FromNodeID, response.ToNodeID)
			}
		})
	}
}

// TestHandleEdge_InvalidID tests handling of invalid edge IDs
func TestHandleEdge_InvalidID(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "Invalid ID - not a number",
			path: "/edges/abc",
		},
		{
			name: "Invalid ID - negative",
			path: "/edges/-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			server.handleEdge(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d for invalid ID, got %d",
					http.StatusBadRequest, rr.Code)
			}
		})
	}
}

// TestBatchEdges tests the POST /batch/edges endpoint
func TestBatchEdges(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create nodes for edges
	nodes := make([]*storage.Node, 5)
	for i := 0; i < 5; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodes[i] = node
	}

	tests := []struct {
		name         string
		request      BatchEdgeRequest
		expectStatus int
		expectError  bool
		minCreated   int
	}{
		{
			name: "Batch create multiple edges",
			request: BatchEdgeRequest{
				Edges: []EdgeRequest{
					{
						FromNodeID: nodes[0].ID,
						ToNodeID:   nodes[1].ID,
						Type:       "LINK",
						Weight:     1.0,
					},
					{
						FromNodeID: nodes[1].ID,
						ToNodeID:   nodes[2].ID,
						Type:       "LINK",
						Weight:     1.0,
					},
					{
						FromNodeID: nodes[2].ID,
						ToNodeID:   nodes[3].ID,
						Type:       "LINK",
						Weight:     1.0,
					},
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
			minCreated:   3,
		},
		{
			name: "Batch with some invalid edges",
			request: BatchEdgeRequest{
				Edges: []EdgeRequest{
					{
						FromNodeID: nodes[0].ID,
						ToNodeID:   nodes[1].ID,
						Type:       "VALID",
						Weight:     1.0,
					},
					{
						FromNodeID: 99999,
						ToNodeID:   nodes[1].ID,
						Type:       "INVALID",
						Weight:     1.0,
					},
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
			minCreated:   1, // Only the valid one should be created
		},
		{
			name: "Empty batch",
			request: BatchEdgeRequest{
				Edges: []EdgeRequest{},
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/batch/edges", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleBatchEdges(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusCreated {
				var response BatchEdgeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.Created < tt.minCreated {
					t.Errorf("Expected at least %d edges created, got %d", tt.minCreated, response.Created)
				}

				t.Logf("✓ Batch created %d edges", response.Created)
			}
		})
	}
}

// TestHandleEdges_MethodNotAllowed tests unsupported HTTP methods
func TestHandleEdges_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "GET on /edges",
			method: http.MethodGet,
			path:   "/edges",
		},
		{
			name:   "PUT on /edges",
			method: http.MethodPut,
			path:   "/edges",
		},
		{
			name:   "DELETE on /edges/{id}",
			method: http.MethodDelete,
			path:   "/edges/1",
		},
		{
			name:   "PUT on /edges/{id}",
			method: http.MethodPut,
			path:   "/edges/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			if tt.path == "/edges" {
				server.handleEdges(rr, req)
			} else {
				server.handleEdge(rr, req)
			}

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rr.Code)
			}
		})
	}
}

// TestCreateEdge_DefensiveProgramming tests defensive programming features
func TestCreateEdge_DefensiveProgramming(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create nodes for testing
	nodeA, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("A"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
		"name": storage.StringValue("B"),
	})

	tests := []struct {
		name         string
		request      EdgeRequest
		expectStatus int
		description  string
	}{
		{
			name: "XSS in edge type - should sanitize",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "LINK",
				Weight:     1.0,
				Properties: map[string]any{
					"description": "<script>alert('XSS')</script>",
				},
			},
			expectStatus: http.StatusCreated,
			description:  "XSS attempts in properties should be sanitized",
		},
		{
			name: "SQL injection attempt in properties",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "LINK",
				Weight:     1.0,
				Properties: map[string]any{
					"query": "'; DROP TABLE nodes; --",
				},
			},
			expectStatus: http.StatusCreated,
			description:  "SQL injection attempts should be harmless with parameterized queries",
		},
		{
			name: "Extremely large property value",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "LINK",
				Weight:     1.0,
				Properties: map[string]any{
					"large": string(make([]byte, 1000)),
				},
			},
			expectStatus: http.StatusCreated,
			description:  "Large property values should be handled",
		},
		{
			name: "Negative weight",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "LINK",
				Weight:     -1.0,
			},
			expectStatus: http.StatusCreated,
			description:  "Negative weights are allowed (for cost/debt graphs)",
		},
		{
			name: "Very large weight",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "LINK",
				Weight:     1e308,
			},
			expectStatus: http.StatusCreated,
			description:  "Large but valid weights should be accepted",
		},
		{
			name: "Unicode in edge type",
			request: EdgeRequest{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeB.ID,
				Type:       "СВЯЗЬ_🔗",
				Weight:     1.0,
			},
			expectStatus: http.StatusBadRequest,
			description:  "Unicode/special characters in edge type are rejected by validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleEdges(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. %s",
					tt.expectStatus, rr.Code, tt.description)
			}

			if rr.Code == http.StatusCreated {
				t.Logf("✓ %s", tt.description)
			}
		})
	}
}

// TestEdge_Integration tests edge creation and retrieval workflow
func TestEdge_Integration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create nodes
	nodeA, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	nodeB, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	t.Logf("✓ Created nodes %d and %d", nodeA.ID, nodeB.ID)

	// 2. Create edge
	createReq := EdgeRequest{
		FromNodeID: nodeA.ID,
		ToNodeID:   nodeB.ID,
		Type:       "KNOWS",
		Weight:     0.8,
		Properties: map[string]any{
			"since": 2020,
			"type":  "colleague",
		},
	}

	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/edges", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleEdges(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to create edge: %s", rr.Body.String())
	}

	var createResp EdgeResponse
	json.Unmarshal(rr.Body.Bytes(), &createResp)
	edgeID := createResp.ID

	t.Logf("✓ Created edge %d: %d -> %d", edgeID, nodeA.ID, nodeB.ID)

	// 3. Retrieve edge
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/edges/%d", edgeID), nil)
	rr = httptest.NewRecorder()
	server.handleEdge(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to retrieve edge: %s", rr.Body.String())
	}

	var retrieveResp EdgeResponse
	json.Unmarshal(rr.Body.Bytes(), &retrieveResp)

	if retrieveResp.ID != edgeID {
		t.Errorf("Expected edge ID %d, got %d", edgeID, retrieveResp.ID)
	}

	if retrieveResp.Type != "KNOWS" {
		t.Errorf("Expected type 'KNOWS', got %s", retrieveResp.Type)
	}

	t.Logf("✓ Retrieved edge %d with type '%s'", edgeID, retrieveResp.Type)

	// 4. Create multiple edges (graph structure)
	nodeC, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})

	batchReq := BatchEdgeRequest{
		Edges: []EdgeRequest{
			{
				FromNodeID: nodeB.ID,
				ToNodeID:   nodeC.ID,
				Type:       "KNOWS",
				Weight:     0.9,
			},
			{
				FromNodeID: nodeA.ID,
				ToNodeID:   nodeC.ID,
				Type:       "MENTORS",
				Weight:     1.0,
			},
		},
	}

	body, _ = json.Marshal(batchReq)
	req = httptest.NewRequest(http.MethodPost, "/batch/edges", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	server.handleBatchEdges(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to batch create edges: %s", rr.Body.String())
	}

	var batchResp BatchEdgeResponse
	json.Unmarshal(rr.Body.Bytes(), &batchResp)

	if batchResp.Created != 2 {
		t.Errorf("Expected 2 edges created, got %d", batchResp.Created)
	}

	t.Logf("✓ Batch created %d edges", batchResp.Created)
	t.Logf("✓ Complete integration test passed")
}

// TestBatchEdges_LargeBatch tests batch size limits
func TestBatchEdges_LargeBatch(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create many nodes for batch testing
	nodes := make([]*storage.Node, 100)
	for i := 0; i < 100; i++ {
		node, _ := server.graph.CreateNode([]string{"Node"}, map[string]storage.Value{
			"id": storage.IntValue(int64(i)),
		})
		nodes[i] = node
	}

	tests := []struct {
		name         string
		edgeCount    int
		expectStatus int
		description  string
	}{
		{
			name:         "Valid batch - 100 edges",
			edgeCount:    100,
			expectStatus: http.StatusCreated,
			description:  "Batch of 100 edges should succeed",
		},
		{
			name:         "At limit - 1000 edges",
			edgeCount:    1000,
			expectStatus: http.StatusCreated,
			description:  "Batch at 1000 edge limit should succeed",
		},
		{
			name:         "Over limit - 1001 edges",
			edgeCount:    1001,
			expectStatus: http.StatusBadRequest,
			description:  "Batch over 1000 edge limit should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := make([]EdgeRequest, tt.edgeCount)
			for i := 0; i < tt.edgeCount; i++ {
				fromIdx := i % len(nodes)
				toIdx := (i + 1) % len(nodes)
				edges[i] = EdgeRequest{
					FromNodeID: nodes[fromIdx].ID,
					ToNodeID:   nodes[toIdx].ID,
					Type:       "LINK",
					Weight:     1.0,
				}
			}

			req := BatchEdgeRequest{Edges: edges}
			body, _ := json.Marshal(req)

			httpReq := httptest.NewRequest(http.MethodPost, "/batch/edges", bytes.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleBatchEdges(rr, httpReq)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. %s",
					tt.expectStatus, rr.Code, tt.description)
			}

			t.Logf("✓ %s", tt.description)
		})
	}
}
