package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
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

// TestVectorSearch tests the POST /vector-search endpoint
func TestVectorSearch(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a vector index
	err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	// Create nodes with vector embeddings
	testVectors := []struct {
		labels []string
		vec    []float32
	}{
		{[]string{"Document"}, []float32{1.0, 0.0, 0.0}},
		{[]string{"Document"}, []float32{0.9, 0.1, 0.0}},
		{[]string{"Document"}, []float32{0.0, 1.0, 0.0}},
		{[]string{"Image"}, []float32{0.0, 0.0, 1.0}},
		{[]string{"Image"}, []float32{0.1, 0.1, 0.9}},
	}

	for _, tv := range testVectors {
		_, err := server.graph.CreateNode(tv.labels, map[string]storage.Value{
			"embedding": storage.VectorValue(tv.vec),
		})
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	tests := []struct {
		name         string
		request      VectorSearchRequest
		expectStatus int
		expectCount  int
	}{
		{
			name: "Basic k-NN search",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{1.0, 0.0, 0.0},
				K:            3,
			},
			expectStatus: http.StatusOK,
			expectCount:  3,
		},
		{
			name: "Search with include_nodes",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{1.0, 0.0, 0.0},
				K:            2,
				IncludeNodes: true,
			},
			expectStatus: http.StatusOK,
			expectCount:  2,
		},
		{
			name: "Search with label filter",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{0.0, 0.0, 1.0},
				K:            5,
				IncludeNodes: true,
				FilterLabels: []string{"Image"},
			},
			expectStatus: http.StatusOK,
			expectCount:  2, // Only 2 Image nodes
		},
		{
			name: "Search non-existent index",
			request: VectorSearchRequest{
				PropertyName: "nonexistent",
				QueryVector:  []float32{1.0, 0.0, 0.0},
				K:            3,
			},
			expectStatus: http.StatusNotFound,
			expectCount:  0,
		},
		{
			name: "Missing property_name",
			request: VectorSearchRequest{
				QueryVector: []float32{1.0, 0.0, 0.0},
				K:           3,
			},
			expectStatus: http.StatusBadRequest,
			expectCount:  0,
		},
		{
			name: "Empty query_vector",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{},
				K:            3,
			},
			expectStatus: http.StatusBadRequest,
			expectCount:  0,
		},
		{
			name: "Invalid k (zero)",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{1.0, 0.0, 0.0},
				K:            0,
			},
			expectStatus: http.StatusBadRequest,
			expectCount:  0,
		},
		{
			name: "Invalid k (too large)",
			request: VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  []float32{1.0, 0.0, 0.0},
				K:            1001, // Max is 1000
			},
			expectStatus: http.StatusBadRequest,
			expectCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.request)
			req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleVectorSearch(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if rr.Code == http.StatusOK {
				var response VectorSearchResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response.Count != tt.expectCount {
					t.Errorf("Expected %d results, got %d", tt.expectCount, response.Count)
				}

				// Verify results are ordered by distance (ascending)
				for i := 1; i < len(response.Results); i++ {
					if response.Results[i].Distance < response.Results[i-1].Distance {
						t.Error("Results not sorted by distance")
					}
				}

				t.Logf("✓ Vector search returned %d results in %dms",
					response.Count, response.TookMs)
			}
		})
	}
}

// TestVectorSearch_NaNInfValidation tests NaN/Inf rejection
func TestVectorSearch_NaNInfValidation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a vector index
	err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	nan := float32(math.NaN())
	posInf := float32(math.Inf(1))
	negInf := float32(math.Inf(-1))

	tests := []struct {
		name        string
		queryVector []float32
	}{
		{
			name:        "NaN in query vector",
			queryVector: []float32{nan, 0.0, 0.0},
		},
		{
			name:        "Positive Inf in query vector",
			queryVector: []float32{posInf, 0.0, 0.0},
		},
		{
			name:        "Negative Inf in query vector",
			queryVector: []float32{0.0, negInf, 0.0},
		},
		{
			name:        "Mixed invalid values",
			queryVector: []float32{nan, posInf, negInf},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  tt.queryVector,
				K:            3,
			}

			body, _ := json.Marshal(request)
			req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleVectorSearch(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("Expected status %d for invalid vector, got %d",
					http.StatusBadRequest, rr.Code)
			}

			t.Logf("✓ Correctly rejected: %s", tt.name)
		})
	}
}

// TestVectorSearch_ScoreCalculation tests correct score calculation per metric
func TestVectorSearch_ScoreCalculation(t *testing.T) {
	tests := []struct {
		name    string
		metric  string
		vectors [][]float32
		query   []float32
		checkFn func(t *testing.T, results []VectorSearchResult)
	}{
		{
			name:   "Cosine similarity scores",
			metric: "cosine",
			vectors: [][]float32{
				{1.0, 0.0, 0.0}, // Identical direction
				{0.0, 1.0, 0.0}, // Orthogonal
			},
			query: []float32{1.0, 0.0, 0.0},
			checkFn: func(t *testing.T, results []VectorSearchResult) {
				// First result should have score close to 1.0 (identical)
				if results[0].Score < 0.99 {
					t.Errorf("Expected score ~1.0 for identical vector, got %f", results[0].Score)
				}
				// Second result should have score close to 0.0 (orthogonal)
				if results[1].Score > 0.01 {
					t.Errorf("Expected score ~0.0 for orthogonal vector, got %f", results[1].Score)
				}
			},
		},
		{
			name:   "Euclidean scores",
			metric: "euclidean",
			vectors: [][]float32{
				{1.0, 0.0, 0.0}, // Distance 0
				{2.0, 0.0, 0.0}, // Distance 1
			},
			query: []float32{1.0, 0.0, 0.0},
			checkFn: func(t *testing.T, results []VectorSearchResult) {
				// First result should have score = 1/(1+0) = 1.0
				if results[0].Score < 0.99 {
					t.Errorf("Expected score 1.0 for zero distance, got %f", results[0].Score)
				}
				// Second result should have score = 1/(1+1) = 0.5
				if results[1].Score < 0.49 || results[1].Score > 0.51 {
					t.Errorf("Expected score ~0.5 for distance 1, got %f", results[1].Score)
				}
			},
		},
		{
			name:   "Dot product scores",
			metric: "dot_product",
			vectors: [][]float32{
				{1.0, 0.0, 0.0}, // Dot = 1.0
				{0.5, 0.0, 0.0}, // Dot = 0.5
			},
			query: []float32{1.0, 0.0, 0.0},
			checkFn: func(t *testing.T, results []VectorSearchResult) {
				// Dot product: higher is better, first should have higher score
				if results[0].Score < results[1].Score {
					t.Errorf("Expected first result to have higher dot product score")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			// Create index with specific metric
			err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, parseMetric(tt.metric))
			if err != nil {
				t.Fatalf("Failed to create index: %v", err)
			}

			// Create nodes with vectors
			for _, vec := range tt.vectors {
				_, err := server.graph.CreateNode([]string{"Test"}, map[string]storage.Value{
					"embedding": storage.VectorValue(vec),
				})
				if err != nil {
					t.Fatalf("Failed to create node: %v", err)
				}
			}

			request := VectorSearchRequest{
				PropertyName: "embedding",
				QueryVector:  tt.query,
				K:            len(tt.vectors),
			}

			body, _ := json.Marshal(request)
			req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleVectorSearch(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d. Body: %s",
					http.StatusOK, rr.Code, rr.Body.String())
			}

			var response VectorSearchResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			tt.checkFn(t, response.Results)
			t.Logf("✓ Score calculation correct for %s metric", tt.metric)
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

// TestVectorSearch_EmptyIndex tests searching an empty index
func TestVectorSearch_EmptyIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create an empty index
	err := server.graph.CreateVectorIndex("empty", 3, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	request := VectorSearchRequest{
		PropertyName: "empty",
		QueryVector:  []float32{1.0, 0.0, 0.0},
		K:            5,
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Count != 0 {
		t.Errorf("Expected 0 results from empty index, got %d", response.Count)
	}

	t.Logf("✓ Empty index search returned 0 results")
}

// TestVectorSearch_LabelFilterExclusion tests that filtering excludes non-matching labels
func TestVectorSearch_LabelFilterExclusion(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create index
	err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine")
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	// Create nodes with different labels but same vector (to ensure label filter works)
	vec := []float32{1.0, 0.0, 0.0}
	for _, label := range []string{"TypeA", "TypeB", "TypeC"} {
		_, err := server.graph.CreateNode([]string{label}, map[string]storage.Value{
			"embedding": storage.VectorValue(vec),
		})
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	// Search with filter for only TypeA
	request := VectorSearchRequest{
		PropertyName: "embedding",
		QueryVector:  vec,
		K:            10,
		IncludeNodes: true,
		FilterLabels: []string{"TypeA"},
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s",
			http.StatusOK, rr.Code, rr.Body.String())
	}

	var response VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should only have 1 result (TypeA)
	if response.Count != 1 {
		t.Errorf("Expected 1 result with label filter, got %d", response.Count)
	}

	// Verify the result has correct label
	if len(response.Results) > 0 && response.Results[0].Node != nil {
		hasTypeA := false
		for _, label := range response.Results[0].Node.Labels {
			if label == "TypeA" {
				hasTypeA = true
				break
			}
		}
		if !hasTypeA {
			t.Error("Result node does not have expected TypeA label")
		}
	}

	t.Logf("✓ Label filter correctly excluded non-matching nodes")
}

// TestDistanceToScore tests the distanceToScore helper function
func TestDistanceToScore(t *testing.T) {
	tests := []struct {
		name     string
		distance float32
		metric   string
		expected float32
	}{
		{"cosine zero distance", 0.0, "cosine", 1.0},
		{"cosine max distance", 2.0, "cosine", -1.0},
		{"euclidean zero distance", 0.0, "euclidean", 1.0},
		{"euclidean distance 1", 1.0, "euclidean", 0.5},
		{"dot_product zero", 0.0, "dot_product", 0.0},
		{"dot_product negative (similarity)", -1.0, "dot_product", 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric := parseMetric(tt.metric)
			score := distanceToScore(tt.distance, metric)

			// Allow small floating point tolerance
			if diff := score - tt.expected; diff > 0.001 || diff < -0.001 {
				t.Errorf("distanceToScore(%f, %s) = %f, expected %f",
					tt.distance, tt.metric, score, tt.expected)
			}
		})
	}
}

// TestVectorSearch_TenantIsolation asserts that /vector-search returns
// only the caller's tenant's vectors.
//
// Pre-R1.2 the HNSW index was global and the handler post-filtered by
// Node.TenantID. Post-R1.2 isolation is structural: vectors live in
// per-tenant HNSW indexes and search routes through *VectorIndexForTenant.
// This test sets up per-tenant indexes for tenant-A and tenant-B, then
// verifies cross-tenant searches don't see other tenants' vectors. Tenant-C
// has its own (empty) index — search returns 200 with no results.
func TestVectorSearch_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const propertyName = "embedding"
	// Per-tenant indexes (R1.2 model). Tenant-C registers but adds no
	// nodes — its search should return 200 with empty results, not 404.
	for _, tid := range []string{"tenant-A", "tenant-B", "tenant-C"} {
		if err := server.graph.CreateVectorIndexForTenant(tid, propertyName, 3, 16, 200, "cosine"); err != nil {
			t.Fatalf("CreateVectorIndexForTenant(%s): %v", tid, err)
		}
	}

	// Two nodes, near-identical vectors. UpdateNodeVectorIndexes routes
	// each vector into its node.TenantID's per-tenant index (R1.2).
	vecA := []float32{1.0, 0.0, 0.0}
	vecB := []float32{1.0, 0.001, 0.0}

	nodeA, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
		propertyName: storage.VectorValue(vecA),
		"label":      storage.StringValue("A"),
	})
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	nodeB, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, map[string]storage.Value{
		propertyName: storage.VectorValue(vecB),
		"label":      storage.StringValue("B"),
	})
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	tests := []struct {
		name        string
		tenantID    string
		wantNodeIDs []uint64
	}{
		{"tenant-A sees only A's node", "tenant-A", []uint64{nodeA.ID}},
		{"tenant-B sees only B's node", "tenant-B", []uint64{nodeB.ID}},
		{"tenant-C (empty) sees nothing", "tenant-C", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(VectorSearchRequest{
				PropertyName: propertyName,
				QueryVector:  vecA,
				K:            10,
			})
			req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			// Inject tenant context directly (bypasses requireAuth+withTenant
			// chain; the context shape matches what withTenant produces).
			req = req.WithContext(tenant.WithTenant(req.Context(), tc.tenantID))

			rr := httptest.NewRecorder()
			server.handleVectorSearch(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
			}
			var resp VectorSearchResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			gotIDs := make([]uint64, 0, len(resp.Results))
			for _, r := range resp.Results {
				gotIDs = append(gotIDs, r.NodeID)
			}

			if len(gotIDs) != len(tc.wantNodeIDs) {
				t.Fatalf("want %d results (%v), got %d (%v)",
					len(tc.wantNodeIDs), tc.wantNodeIDs, len(gotIDs), gotIDs)
			}
			for i, want := range tc.wantNodeIDs {
				if gotIDs[i] != want {
					t.Errorf("result %d: want NodeID=%d, got %d", i, want, gotIDs[i])
				}
			}
		})
	}
}

// vectorSearchPropertyFilter is a helper that POSTs req to /vector-search
// and returns the recorder. Used by the property_filter test family below.
func vectorSearchPropertyFilter(t *testing.T, server *Server, req VectorSearchRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, httpReq)
	return rr
}

// TestVectorSearch_PropertyFilter_NoOpWhenAbsent pins the no-op behaviour:
// an unset property_filter must not change results vs. a baseline. This
// guards against a future refactor making the field accidentally required.
func TestVectorSearch_PropertyFilter_NoOpWhenAbsent(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}

	vec := []float32{1.0, 0.0, 0.0}
	for i := 0; i < 3; i++ {
		if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
			"embedding": storage.VectorValue(vec),
			"isPublic":  storage.BoolValue(i%2 == 0),
		}); err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
	}

	rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName: "embedding",
		QueryVector:  vec,
		K:            10,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 3 {
		t.Errorf("with no property_filter, want all 3 nodes returned, got %d", resp.Count)
	}
}

// TestVectorSearch_PropertyFilter_ExcludesNonMatching is the core privacy
// case: two near-identical vectors, one public and one private. With
// property_filter: {"isPublic": true} the private node must not appear.
func TestVectorSearch_PropertyFilter_ExcludesNonMatching(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}

	vec := []float32{1.0, 0.0, 0.0}
	publicNode, err := server.graph.CreateNode([]string{"Submission"}, map[string]storage.Value{
		"embedding": storage.VectorValue(vec),
		"isPublic":  storage.BoolValue(true),
	})
	if err != nil {
		t.Fatalf("create public: %v", err)
	}
	privateNode, err := server.graph.CreateNode([]string{"Submission"}, map[string]storage.Value{
		"embedding": storage.VectorValue([]float32{1.0, 0.001, 0.0}),
		"isPublic":  storage.BoolValue(false),
	})
	if err != nil {
		t.Fatalf("create private: %v", err)
	}

	rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName:   "embedding",
		QueryVector:    vec,
		K:              10,
		PropertyFilter: map[string]any{"isPublic": true},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("want 1 result, got %d (%+v)", resp.Count, resp.Results)
	}
	if resp.Results[0].NodeID != publicNode.ID {
		t.Errorf("want public node %d, got %d", publicNode.ID, resp.Results[0].NodeID)
	}
	for _, r := range resp.Results {
		if r.NodeID == privateNode.ID {
			t.Errorf("private node %d leaked into results", privateNode.ID)
		}
	}
}

// TestVectorSearch_PropertyFilter_EmptyResultsNotError verifies that a
// predicate matching nothing returns 200 with count=0, not 500.
func TestVectorSearch_PropertyFilter_EmptyResultsNotError(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}
	vec := []float32{1.0, 0.0, 0.0}
	if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
		"embedding": storage.VectorValue(vec),
		"isPublic":  storage.BoolValue(true),
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName:   "embedding",
		QueryVector:    vec,
		K:              10,
		PropertyFilter: map[string]any{"nonexistent_key": "value"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("want count=0 for unmatched predicate, got %d", resp.Count)
	}
	if len(resp.Results) != 0 {
		t.Errorf("want empty results, got %d", len(resp.Results))
	}
}

// TestVectorSearch_PropertyFilter_AndsWithLabels enforces the security
// contract: filter_labels and property_filter must AND, not OR. This is
// the regression class the feature exists to prevent — a node satisfying
// only one filter must not slip through.
func TestVectorSearch_PropertyFilter_AndsWithLabels(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}

	vec := []float32{1.0, 0.0, 0.0}
	type nodeSpec struct {
		labels   []string
		isPublic bool
	}
	specs := []nodeSpec{
		{[]string{"Submission"}, true},  // (T, T) — only this should pass
		{[]string{"Submission"}, false}, // (T, F) — excluded by property
		{[]string{"Other"}, true},       // (F, T) — excluded by label
		{[]string{"Other"}, false},      // (F, F) — excluded by both
	}
	ids := make([]uint64, len(specs))
	for i, s := range specs {
		node, err := server.graph.CreateNode(s.labels, map[string]storage.Value{
			"embedding": storage.VectorValue(vec),
			"isPublic":  storage.BoolValue(s.isPublic),
		})
		if err != nil {
			t.Fatalf("CreateNode[%d]: %v", i, err)
		}
		ids[i] = node.ID
	}

	rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName:   "embedding",
		QueryVector:    vec,
		K:              10,
		FilterLabels:   []string{"Submission"},
		PropertyFilter: map[string]any{"isPublic": true},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 1 {
		t.Fatalf("AND semantics: want exactly 1 result, got %d (%+v)", resp.Count, resp.Results)
	}
	if resp.Results[0].NodeID != ids[0] {
		t.Errorf("want only the (Submission, isPublic=true) node %d, got %d", ids[0], resp.Results[0].NodeID)
	}
}

// TestVectorSearch_PropertyFilter_BoolRoundTrip pins the storage.Value
// comparison path. Store a BoolValue, send a JSON bool, assert the
// predicate matches. If convertToValue's bool encoding ever changes (or
// matchesPropertyFilter starts comparing differently), this test fires.
func TestVectorSearch_PropertyFilter_BoolRoundTrip(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}
	vec := []float32{1.0, 0.0, 0.0}
	wantNode, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
		"embedding": storage.VectorValue(vec),
		"isPublic":  storage.BoolValue(true),
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// true => match
	rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName:   "embedding",
		QueryVector:    vec,
		K:              10,
		PropertyFilter: map[string]any{"isPublic": true},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("true: status %d. Body: %s", rr.Code, rr.Body.String())
	}
	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("true: unmarshal: %v", err)
	}
	if resp.Count != 1 || resp.Results[0].NodeID != wantNode.ID {
		t.Errorf("bool true predicate did not match BoolValue(true): count=%d results=%+v", resp.Count, resp.Results)
	}

	// false => no match (asymmetry test — confirms the byte comparison
	// distinguishes BoolValue(true) from BoolValue(false)).
	rr = vectorSearchPropertyFilter(t, server, VectorSearchRequest{
		PropertyName:   "embedding",
		QueryVector:    vec,
		K:              10,
		PropertyFilter: map[string]any{"isPublic": false},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("false: status %d. Body: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("false: unmarshal: %v", err)
	}
	if resp.Count != 0 {
		t.Errorf("bool false predicate matched BoolValue(true): count=%d", resp.Count)
	}
}

// TestVectorSearch_PropertyFilter_NonPrimitiveRejected verifies that
// non-primitive predicate values fail closed with 400. Failing closed
// matters: convertToValue's default branch stringifies, which would
// silently produce fuzzy matches that defeat the privacy boundary.
func TestVectorSearch_PropertyFilter_NonPrimitiveRejected(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}
	if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
		"embedding": storage.VectorValue([]float32{1, 0, 0}),
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	cases := []struct {
		name   string
		filter map[string]any
	}{
		{"array", map[string]any{"foo": []string{"a", "b"}}},
		{"nested object", map[string]any{"foo": map[string]any{"nested": 1}}},
		{"null", map[string]any{"foo": nil}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := vectorSearchPropertyFilter(t, server, VectorSearchRequest{
				PropertyName:   "embedding",
				QueryVector:    []float32{1, 0, 0},
				K:              10,
				PropertyFilter: tc.filter,
			})
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d. Body: %s", rr.Code, rr.Body.String())
			}
			if !bytes.Contains(rr.Body.Bytes(), []byte("property_filter")) {
				t.Errorf("error message should name the offending field; got: %s", rr.Body.String())
			}
		})
	}
}
