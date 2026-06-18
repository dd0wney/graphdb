package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestCreateVectorIndex tests the POST /vector-indexes endpoint
func TestCreateVectorIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		request      VectorIndexRequest
		expectStatus int
		expectError  bool
	}{
		{
			name: "Valid cosine index",
			request: VectorIndexRequest{
				PropertyName: "embedding",
				Dimensions:   384,
				Metric:       "cosine",
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Valid euclidean index with custom params",
			request: VectorIndexRequest{
				PropertyName:   "features",
				Dimensions:     128,
				M:              32,
				EfConstruction: 400,
				Metric:         "euclidean",
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Valid dot_product index",
			request: VectorIndexRequest{
				PropertyName: "vectors",
				Dimensions:   256,
				Metric:       "dot_product",
			},
			expectStatus: http.StatusCreated,
			expectError:  false,
		},
		{
			name: "Missing property_name",
			request: VectorIndexRequest{
				Dimensions: 128,
				Metric:     "cosine",
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
		{
			name: "Zero dimensions",
			request: VectorIndexRequest{
				PropertyName: "bad_dims",
				Dimensions:   0,
				Metric:       "cosine",
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
		{
			name: "Dimensions too large",
			request: VectorIndexRequest{
				PropertyName: "huge_dims",
				Dimensions:   5000, // Max is 4096
				Metric:       "cosine",
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
		{
			name: "Negative dimensions",
			request: VectorIndexRequest{
				PropertyName: "neg_dims",
				Dimensions:   -1,
				Metric:       "cosine",
			},
			expectStatus: http.StatusBadRequest,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleVectorIndexes(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if !tt.expectError && rr.Code == http.StatusCreated {
				var response VectorIndexResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.PropertyName != tt.request.PropertyName {
					t.Errorf("Expected property_name %q, got %q",
						tt.request.PropertyName, response.PropertyName)
				}

				t.Logf("✓ Created vector index: %s", response.PropertyName)
			}
		})
	}
}

// TestCreateVectorIndex_Conflict tests duplicate index creation
func TestCreateVectorIndex_Conflict(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create initial index
	req1 := VectorIndexRequest{
		PropertyName: "embedding",
		Dimensions:   128,
		Metric:       "cosine",
	}
	body, _ := json.Marshal(req1)
	r := httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleVectorIndexes(rr, r)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to create initial index: %s", rr.Body.String())
	}

	// Try to create duplicate
	body, _ = json.Marshal(req1)
	r = httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	server.handleVectorIndexes(rr, r)

	if rr.Code != http.StatusConflict {
		t.Errorf("Expected status %d for duplicate index, got %d",
			http.StatusConflict, rr.Code)
	}

	t.Logf("✓ Correctly rejected duplicate index creation")
}

// TestListVectorIndexes tests the GET /vector-indexes endpoint
func TestListVectorIndexes(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some indexes
	indexNames := []string{"embedding1", "embedding2", "features"}
	for _, name := range indexNames {
		err := server.graph.CreateVectorIndex(name, 128, 16, 200, "cosine")
		if err != nil {
			t.Fatalf("Failed to create index %s: %v", name, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/vector-indexes", nil)
	rr := httptest.NewRecorder()
	server.handleVectorIndexes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response VectorIndexListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Count != len(indexNames) {
		t.Errorf("Expected %d indexes, got %d", len(indexNames), response.Count)
	}

	t.Logf("✓ Listed %d vector indexes", response.Count)
}

// TestGetVectorIndex tests the GET /vector-indexes/{name} endpoint
func TestGetVectorIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create an index
	err := server.graph.CreateVectorIndex("embedding", 128, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	tests := []struct {
		name         string
		propertyName string
		expectStatus int
	}{
		{
			name:         "Get existing index",
			propertyName: "embedding",
			expectStatus: http.StatusOK,
		},
		{
			name:         "Get non-existent index",
			propertyName: "nonexistent",
			expectStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/vector-indexes/"+tt.propertyName, nil)
			rr := httptest.NewRecorder()
			server.handleVectorIndex(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if rr.Code == http.StatusOK {
				var response VectorIndexResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}
				if response.PropertyName != tt.propertyName {
					t.Errorf("Expected property_name %q, got %q",
						tt.propertyName, response.PropertyName)
				}
				t.Logf("✓ Retrieved index: %s", response.PropertyName)
			}
		})
	}
}

// TestDeleteVectorIndex tests the DELETE /vector-indexes/{name} endpoint
func TestDeleteVectorIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create an index
	err := server.graph.CreateVectorIndex("to_delete", 128, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	tests := []struct {
		name         string
		propertyName string
		expectStatus int
	}{
		{
			name:         "Delete existing index",
			propertyName: "to_delete",
			expectStatus: http.StatusNoContent,
		},
		{
			name:         "Delete non-existent index",
			propertyName: "nonexistent",
			expectStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/vector-indexes/"+tt.propertyName, nil)
			rr := httptest.NewRecorder()
			server.handleVectorIndex(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if rr.Code == http.StatusNoContent {
				// Verify index is actually deleted
				if server.graph.HasVectorIndex(tt.propertyName) {
					t.Error("Index should be deleted but still exists")
				}
				t.Logf("✓ Deleted index: %s", tt.propertyName)
			}
		})
	}
}

// TestVectorIndexes_MethodNotAllowed tests unsupported HTTP methods
func TestVectorIndexes_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "PUT on /vector-indexes",
			method: http.MethodPut,
			path:   "/vector-indexes",
		},
		{
			name:   "DELETE on /vector-indexes",
			method: http.MethodDelete,
			path:   "/vector-indexes",
		},
		{
			name:   "POST on /vector-indexes/{name}",
			method: http.MethodPost,
			path:   "/vector-indexes/embedding",
		},
		{
			name:   "PUT on /vector-indexes/{name}",
			method: http.MethodPut,
			path:   "/vector-indexes/embedding",
		},
		{
			name:   "GET on /vector-search",
			method: http.MethodGet,
			path:   "/vector-search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			switch tt.path {
			case "/vector-indexes":
				server.handleVectorIndexes(rr, req)
			case "/vector-search":
				server.handleVectorSearch(rr, req)
			default:
				server.handleVectorIndex(rr, req)
			}

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d, got %d",
					http.StatusMethodNotAllowed, rr.Code)
			}
		})
	}
}

// TestVectorIndex_CRUD_Integration tests the full lifecycle
func TestVectorIndex_CRUD_Integration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	propertyName := "integration_test_embedding"

	// 1. Create index
	createReq := VectorIndexRequest{
		PropertyName: propertyName,
		Dimensions:   64,
		Metric:       "cosine",
	}

	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleVectorIndexes(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Failed to create index: %s", rr.Body.String())
	}
	t.Logf("✓ Created index: %s", propertyName)

	// 2. List and verify it exists
	req = httptest.NewRequest(http.MethodGet, "/vector-indexes", nil)
	rr = httptest.NewRecorder()
	server.handleVectorIndexes(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to list indexes: %s", rr.Body.String())
	}

	var listResp VectorIndexListResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &listResp)

	found := false
	for _, idx := range listResp.Indexes {
		if idx.PropertyName == propertyName {
			found = true
			break
		}
	}
	if !found {
		t.Error("Created index not found in list")
	}
	t.Logf("✓ Index found in list")

	// 3. Get specific index
	req = httptest.NewRequest(http.MethodGet, "/vector-indexes/"+propertyName, nil)
	rr = httptest.NewRecorder()
	server.handleVectorIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to get index: %s", rr.Body.String())
	}
	t.Logf("✓ Retrieved index")

	// 4. Add nodes and search
	vec := make([]float32, 64)
	vec[0] = 1.0
	_, err := server.graph.CreateNode([]string{"Test"}, map[string]storage.Value{
		propertyName: storage.VectorValue(vec),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	searchReq := VectorSearchRequest{
		PropertyName: propertyName,
		QueryVector:  vec,
		K:            1,
	}
	body, _ = json.Marshal(searchReq)
	req = httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr = httptest.NewRecorder()
	server.handleVectorSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to search: %s", rr.Body.String())
	}

	var searchResp VectorSearchResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &searchResp)

	if searchResp.Count != 1 {
		t.Errorf("Expected 1 result, got %d", searchResp.Count)
	}
	t.Logf("✓ Vector search successful")

	// 5. Delete index
	req = httptest.NewRequest(http.MethodDelete, "/vector-indexes/"+propertyName, nil)
	rr = httptest.NewRecorder()
	server.handleVectorIndex(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Failed to delete index: %s", rr.Body.String())
	}
	t.Logf("✓ Deleted index")

	// 6. Verify deletion
	req = httptest.NewRequest(http.MethodGet, "/vector-indexes/"+propertyName, nil)
	rr = httptest.NewRecorder()
	server.handleVectorIndex(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected index to be deleted, but GET returned %d", rr.Code)
	}
	t.Logf("✓ Verified index deletion")
}
