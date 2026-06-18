package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

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
