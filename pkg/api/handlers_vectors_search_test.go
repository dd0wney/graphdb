package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

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
