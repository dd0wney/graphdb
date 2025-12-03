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

// TestListNodes tests the GET /nodes endpoint
func TestListNodes(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some test nodes
	for i := 0; i < 3; i++ {
		server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("Person%d", i)),
			"age":  storage.IntValue(int64(20 + i)),
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	rr := httptest.NewRecorder()

	server.handleNodes(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var nodes []*NodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(nodes) < 3 {
		t.Errorf("Expected at least 3 nodes, got %d", len(nodes))
	}

	t.Logf("✓ Listed %d nodes", len(nodes))
}

// TestCreateNode tests the POST /nodes endpoint
func TestCreateNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		request      NodeRequest
		expectStatus int
		expectError  bool
	}{
		{
			name: "Valid node creation",
			request: NodeRequest{
				Labels: []string{"Person"},
				Properties: map[string]any{
					"name": "Alice",
					"age":  30,
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Node with multiple labels",
			request: NodeRequest{
				Labels: []string{"Person", "Employee"},
				Properties: map[string]any{
					"name":       "Bob",
					"department": "Engineering",
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Node with empty properties",
			request: NodeRequest{
				Labels:     []string{"Empty"},
				Properties: map[string]any{},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Invalid request - no labels",
			request: NodeRequest{
				Labels: []string{},
				Properties: map[string]any{
					"name": "NoLabel",
				},
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleNodes(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusCreated {
				var response NodeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.ID == 0 {
					t.Error("Expected non-zero node ID")
				}

				t.Logf("✓ Created node with ID %d", response.ID)
			}
		})
	}
}

// TestGetNode tests the GET /nodes/{id} endpoint
func TestGetNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test node
	node, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	tests := []struct {
		name         string
		nodeID       uint64
		expectStatus int
		expectError  bool
	}{
		{
			name:         "Get existing node",
			nodeID:       node.ID,
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name:         "Get non-existent node",
			nodeID:       99999,
			expectStatus: http.StatusNotFound,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", tt.nodeID), nil)
			rr := httptest.NewRecorder()

			server.handleNode(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response NodeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.ID != tt.nodeID {
					t.Errorf("Expected node ID %d, got %d", tt.nodeID, response.ID)
				}

				t.Logf("✓ Retrieved node %d", response.ID)
			}
		})
	}
}

// TestUpdateNode tests the PUT /nodes/{id} endpoint
func TestUpdateNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test node
	node, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	tests := []struct {
		name         string
		nodeID       uint64
		request      NodeRequest
		expectStatus int
		expectError  bool
	}{
		{
			name:   "Update existing node",
			nodeID: node.ID,
			request: NodeRequest{
				Properties: map[string]any{
					"name": "Alice Updated",
					"age":  31,
					"city": "San Francisco",
				},
			},
			expectStatus: http.StatusOK,
			expectError:  false,
		},
		{
			name:   "Update non-existent node",
			nodeID: 99999,
			request: NodeRequest{
				Properties: map[string]any{
					"name": "NonExistent",
				},
			},
			expectStatus: http.StatusInternalServerError,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/nodes/%d", tt.nodeID), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleNode(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusOK {
				var response NodeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				t.Logf("✓ Updated node %d", response.ID)
			}
		})
	}
}

// TestDeleteNode tests the DELETE /nodes/{id} endpoint
func TestDeleteNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test node
	node, _ := server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("ToDelete"),
	})

	tests := []struct {
		name         string
		nodeID       uint64
		expectStatus int
	}{
		{
			name:         "Delete existing node",
			nodeID:       node.ID,
			expectStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/nodes/%d", tt.nodeID), nil)
			rr := httptest.NewRecorder()

			server.handleNode(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if rr.Code == http.StatusOK {
				var response map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if deleted, ok := response["deleted"].(float64); ok {
					if uint64(deleted) != tt.nodeID {
						t.Errorf("Expected deleted ID %d, got %v", tt.nodeID, deleted)
					}
					t.Logf("✓ Deleted node %d", tt.nodeID)
				}

				// Verify node is actually deleted
				_, err := server.graph.GetNode(tt.nodeID)
				if err == nil {
					t.Error("Node should be deleted but still exists")
				}
			}
		})
	}
}

// TestHandleNode_InvalidID tests handling of invalid node IDs
func TestHandleNode_InvalidID(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		path   string
		method string
	}{
		{
			name:   "Invalid ID - not a number",
			path:   "/nodes/abc",
			method: http.MethodGet,
		},
		{
			name:   "Invalid ID - negative",
			path:   "/nodes/-1",
			method: http.MethodGet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			server.handleNode(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d for invalid ID, got %d",
					http.StatusBadRequest, rr.Code)
			}
		})
	}
}

// TestBatchNodes tests the POST /batch/nodes endpoint
func TestBatchNodes(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		request      BatchNodeRequest
		expectStatus int
		expectError  bool
		minCreated   int
	}{
		{
			name: "Batch create multiple nodes",
			request: BatchNodeRequest{
				Nodes: []NodeRequest{
					{
						Labels: []string{"Person"},
						Properties: map[string]any{
							"name": "Alice",
						},
					},
					{
						Labels: []string{"Person"},
						Properties: map[string]any{
							"name": "Bob",
						},
					},
					{
						Labels: []string{"Person"},
						Properties: map[string]any{
							"name": "Charlie",
						},
					},
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
			minCreated:   3,
		},
		{
			name: "Batch with some invalid nodes",
			request: BatchNodeRequest{
				Nodes: []NodeRequest{
					{
						Labels: []string{"Person"},
						Properties: map[string]any{
							"name": "Valid",
						},
					},
					{
						Labels: []string{}, // Invalid - no labels
						Properties: map[string]any{
							"name": "Invalid",
						},
					},
				},
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
			minCreated:   1, // Only the valid one should be created
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/batch/nodes", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleBatchNodes(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusCreated {
				var response BatchNodeResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.Created < tt.minCreated {
					t.Errorf("Expected at least %d nodes created, got %d", tt.minCreated, response.Created)
				}

				t.Logf("✓ Batch created %d nodes", response.Created)
			}
		})
	}
}

// TestHandleNodes_MethodNotAllowed tests unsupported HTTP methods
func TestHandleNodes_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "PUT on /nodes",
			method: http.MethodPut,
			path:   "/nodes",
		},
		{
			name:   "DELETE on /nodes",
			method: http.MethodDelete,
			path:   "/nodes",
		},
		{
			name:   "PATCH on /nodes/{id}",
			method: http.MethodPatch,
			path:   "/nodes/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			if tt.path == "/nodes" {
				server.handleNodes(rr, req)
			} else {
				server.handleNode(rr, req)
			}

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rr.Code)
			}
		})
	}
}

// TestNode_CRUD_Integration tests the full CRUD lifecycle
func TestNode_CRUD_Integration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Create a node
	createReq := NodeRequest{
		Labels: []string{"Person"},
		Properties: map[string]any{
			"name": "Alice",
			"age":  30,
		},
	}

	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleNodes(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to create node: %s", rr.Body.String())
	}

	var createResp NodeResponse
	json.Unmarshal(rr.Body.Bytes(), &createResp)
	nodeID := createResp.ID

	t.Logf("✓ Created node %d", nodeID)

	// 2. Read the node
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", nodeID), nil)
	rr = httptest.NewRecorder()
	server.handleNode(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to read node: %s", rr.Body.String())
	}

	var readResp NodeResponse
	json.Unmarshal(rr.Body.Bytes(), &readResp)

	if readResp.Properties["name"] == nil {
		t.Error("Expected name property to exist")
	}

	t.Logf("✓ Read node %d with properties", nodeID)

	// 3. Update the node
	updateReq := NodeRequest{
		Properties: map[string]any{
			"name": "Alice Updated",
			"age":  31,
		},
	}

	body, _ = json.Marshal(updateReq)
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/nodes/%d", nodeID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	server.handleNode(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to update node: %s", rr.Body.String())
	}

	var updateResp NodeResponse
	json.Unmarshal(rr.Body.Bytes(), &updateResp)

	if updateResp.ID != nodeID {
		t.Errorf("Expected node ID %d after update, got %d", nodeID, updateResp.ID)
	}

	t.Logf("✓ Updated node %d", nodeID)

	// 4. Delete the node
	req = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/nodes/%d", nodeID), nil)
	rr = httptest.NewRecorder()
	server.handleNode(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to delete node: %s", rr.Body.String())
	}

	t.Logf("✓ Deleted node %d", nodeID)

	// 5. Verify deletion
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d", nodeID), nil)
	rr = httptest.NewRecorder()
	server.handleNode(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected node to be deleted, but GET returned %d", rr.Code)
	}

	t.Logf("✓ Verified node %d is deleted", nodeID)
}
