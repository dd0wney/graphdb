package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestVectorSearch_NearestNeighbourCorrectness closes the Track Q / Q1 gap: the
// existing REST vector-search tests assert result *count* + monotonic distance
// ordering, but never that the returned nodes are the actually-nearest ones.
// A ranking regression that returns the WRONG nodes (or the farthest instead of
// the nearest — the #243 heap-inversion / k-farthest class of bug) would still
// pass a count-only test. This pins nearest-neighbour *identity* at the REST
// handler layer.
//
// The vectors are two well-separated clusters with a planted exact match per
// cluster, so the correct top result is unambiguous (deterministic known
// answer — not the synthetic-uniform concentration-of-measure regime where
// recall is legitimately fuzzy; see memory reference_hnsw_construction_cost).
// CONSUMER CONTRACT: CC2-vector-nn-identity — understand-graphdb (#283)
func TestVectorSearch_NearestNeighbourCorrectness(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreateVectorIndex("embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndex: %v", err)
	}

	// Planted clusters: "doc" near +x, "img" near +z, each with an exact match.
	mk := func(labels []string, vec []float32) uint64 {
		t.Helper()
		n, err := server.graph.CreateNode(labels, map[string]storage.Value{"embedding": storage.VectorValue(vec)})
		if err != nil {
			t.Fatalf("CreateNode: %v", err)
		}
		return n.ID
	}
	docExact := mk([]string{"Document"}, []float32{1, 0, 0})
	docNear := mk([]string{"Document"}, []float32{0.9, 0.1, 0})
	_ = mk([]string{"Document"}, []float32{0, 1, 0}) // doc, but far from +x
	imgExact := mk([]string{"Image"}, []float32{0, 0, 1})
	imgNear := mk([]string{"Image"}, []float32{0.1, 0.1, 0.9})

	search := func(query []float32, k int) []VectorSearchResult {
		t.Helper()
		body, _ := json.Marshal(VectorSearchRequest{PropertyName: "embedding", QueryVector: query, K: k})
		req := httptest.NewRequest(http.MethodPost, "/vector-search", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.handleVectorSearch(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("search status %d: %s", rr.Code, rr.Body.String())
		}
		var resp VectorSearchResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Results
	}

	ids := func(rs []VectorSearchResult) map[uint64]bool {
		m := make(map[uint64]bool, len(rs))
		for _, r := range rs {
			m[r.NodeID] = true
		}
		return m
	}

	// Query the +x cluster: the exact match must rank #1, and the top-2 must be
	// the two Document-cluster vectors — NOT an Image node.
	t.Run("doc cluster query returns doc-cluster nearest", func(t *testing.T) {
		rs := search([]float32{1, 0, 0}, 2)
		if len(rs) != 2 {
			t.Fatalf("got %d results, want 2", len(rs))
		}
		if rs[0].NodeID != docExact {
			t.Errorf("top result NodeID=%d, want exact match %d (ranking regression — wrong nearest neighbour)", rs[0].NodeID, docExact)
		}
		got := ids(rs)
		if !got[docExact] || !got[docNear] {
			t.Errorf("top-2 = %v, want the Document cluster {%d,%d} — wrong nodes returned", got, docExact, docNear)
		}
	})

	// Query the +z cluster: symmetric assertion.
	t.Run("img cluster query returns img-cluster nearest", func(t *testing.T) {
		rs := search([]float32{0, 0, 1}, 2)
		if len(rs) != 2 {
			t.Fatalf("got %d results, want 2", len(rs))
		}
		if rs[0].NodeID != imgExact {
			t.Errorf("top result NodeID=%d, want exact match %d (ranking regression)", rs[0].NodeID, imgExact)
		}
		got := ids(rs)
		if !got[imgExact] || !got[imgNear] {
			t.Errorf("top-2 = %v, want the Image cluster {%d,%d}", got, imgExact, imgNear)
		}
	})
}
