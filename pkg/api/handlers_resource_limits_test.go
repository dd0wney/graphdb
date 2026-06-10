package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TestCreateVectorIndex_RejectsOversizedHNSWParams pins security audit
// finding H-7 (AUDIT_security_2026-06-10.md): the create-index handler
// floored M / ef_construction at their defaults but had no ceiling, so a
// caller could request M=100000 and make every subsequent node insert a
// multi-second O(M*efConstruction) event holding the index write mutex.
//
// RED against pre-fix code: over-cap values are accepted (201).
func TestCreateVectorIndex_RejectsOversizedHNSWParams(t *testing.T) {
	cases := []struct {
		name string
		req  VectorIndexRequest
		want int
	}{
		{
			name: "M over cap",
			req:  VectorIndexRequest{PropertyName: "p_m", Dimensions: 8, M: maxM + 1},
			want: http.StatusBadRequest,
		},
		{
			name: "ef_construction over cap",
			req:  VectorIndexRequest{PropertyName: "p_ef", Dimensions: 8, EfConstruction: maxEfConstruction + 1},
			want: http.StatusBadRequest,
		},
		{
			name: "M at cap is accepted",
			req:  VectorIndexRequest{PropertyName: "p_m_ok", Dimensions: 8, M: maxM},
			want: http.StatusCreated,
		},
		{
			name: "ef_construction at cap is accepted",
			req:  VectorIndexRequest{PropertyName: "p_ef_ok", Dimensions: 8, EfConstruction: maxEfConstruction},
			want: http.StatusCreated,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			body, _ := json.Marshal(tc.req)
			r := httptest.NewRequest(http.MethodPost, "/vector-indexes", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			server.handleVectorIndexes(rr, r)

			if rr.Code != tc.want {
				t.Errorf("want %d, got %d (body: %s)", tc.want, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestVectorSearch_RejectsOversizedEf pins the search-side half of H-7:
// a caller-supplied ef beyond maxEf would force an unboundedly wide
// search-layer scan per query.
//
// RED against pre-fix code: any positive ef is accepted unconditionally.
func TestVectorSearch_RejectsOversizedEf(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("create index: %v", err)
	}
	if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
		"embedding": storage.VectorValue([]float32{1, 0, 0}),
	}); err != nil {
		t.Fatalf("create node: %v", err)
	}

	body, _ := json.Marshal(VectorSearchRequest{
		PropertyName: "embedding",
		QueryVector:  []float32{1, 0, 0},
		K:            1,
		Ef:           maxEf + 1,
	})
	r := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("ef over cap: want 400, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// TestTraverse_NodeCapTruncates pins security audit finding H-8: a dense
// graph must not let a single /traverse materialize the entire reachable
// set. The traversal stops at MaxTraversalNodes, returns 200 with the
// partial set, sets Truncated=true and the X-Truncated header.
//
// RED against pre-fix code: the full reachable set is returned, count
// exceeds the cap, and neither the field nor the header is set.
func TestTraverse_NodeCapTruncates(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Lower the cap so the test exercises truncation with a tiny graph
	// instead of materializing 10k nodes. Restore after.
	origCap := MaxTraversalNodes
	MaxTraversalNodes = 25
	defer func() { MaxTraversalNodes = origCap }()

	const td = tenant.DefaultTenantID
	center, err := server.graph.CreateNodeWithTenant(td, []string{"Hub"}, nil)
	if err != nil {
		t.Fatalf("create center: %v", err)
	}
	// One more leaf than the cap so the result must be truncated.
	leaves := MaxTraversalNodes + 5
	for i := 0; i < leaves; i++ {
		leaf, err := server.graph.CreateNodeWithTenant(td, []string{"Leaf"}, nil)
		if err != nil {
			t.Fatalf("create leaf %d: %v", i, err)
		}
		if _, err := server.graph.CreateEdgeWithTenant(td, center.ID, leaf.ID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("create edge %d: %v", i, err)
		}
	}

	body, _ := json.Marshal(TraversalRequest{StartNodeID: center.ID, MaxDepth: 1})
	r := httptest.NewRequest(http.MethodPost, "/traverse", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleTraversal(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var resp TraversalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != MaxTraversalNodes {
		t.Errorf("count: want exactly %d (capped), got %d", MaxTraversalNodes, resp.Count)
	}
	if !resp.Truncated {
		t.Error("Truncated: want true on a capped traversal")
	}
	if rr.Header().Get("X-Truncated") != "true" {
		t.Errorf("X-Truncated header: want \"true\", got %q", rr.Header().Get("X-Truncated"))
	}
}

// TestTraverse_UnderCapNotTruncated pins that a normal traversal below
// the cap is unaffected: full result, no truncation signal.
func TestTraverse_UnderCapNotTruncated(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const td = tenant.DefaultTenantID
	center, err := server.graph.CreateNodeWithTenant(td, []string{"Hub"}, nil)
	if err != nil {
		t.Fatalf("create center: %v", err)
	}
	const leaves = 5
	for i := 0; i < leaves; i++ {
		leaf, err := server.graph.CreateNodeWithTenant(td, []string{"Leaf"}, nil)
		if err != nil {
			t.Fatalf("create leaf: %v", err)
		}
		if _, err := server.graph.CreateEdgeWithTenant(td, center.ID, leaf.ID, "LINKS", nil, 1.0); err != nil {
			t.Fatalf("create edge: %v", err)
		}
	}

	body, _ := json.Marshal(TraversalRequest{StartNodeID: center.ID, MaxDepth: 1})
	r := httptest.NewRequest(http.MethodPost, "/traverse", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleTraversal(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var resp TraversalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Count != leaves+1 {
		t.Errorf("count: want %d (center + leaves), got %d", leaves+1, resp.Count)
	}
	if resp.Truncated {
		t.Error("Truncated: want false below the cap")
	}
	if rr.Header().Get("X-Truncated") != "" {
		t.Errorf("X-Truncated header: want unset below cap, got %q", rr.Header().Get("X-Truncated"))
	}
}
