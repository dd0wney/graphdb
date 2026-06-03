package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestVectorSearch_RESTFloatArrayIngestionRoundTrip closes the Track Q / Q2
// gap surfaced by driving the understand-graphdb consumer: every existing
// API-layer vector test (including the Q1 nearest-neighbour identity test)
// creates nodes via server.graph.CreateNode(..., storage.VectorValue(vec)),
// which constructs a TypeVector property *in process*. A pure-REST client —
// which is what every consumer is — cannot do that: JSON has no typed-vector
// encoding, so POST /nodes with a number-array property decodes to
// TypeFloatArray (storage.ValueFromJSON -> FloatArrayValue).
//
// #246 made UpdateNodeVectorIndexes index a TypeFloatArray property when a
// vector index exists for that name, so REST clients can populate an HNSW
// index with no new API surface. That fix is pinned at the storage layer
// (TestStorageVectorSearchFromFloatArrayProperty) but NOT at the REST layer
// it was written for — the exact surface the bug lived on. Before #246 this
// round-trip returned zero results (the float array was silently never
// indexed); this test pins that it no longer does.
//
// Path exercised end-to-end through the real handlers:
//
//	POST /nodes  {"properties":{"embedding":[1,0,0]}}  -> ValueFromJSON
//	  -> TypeFloatArray -> #246 coercion -> HNSW index
//	POST /vector-search {"query_vector":[1,0,0]}        -> returns the node
//
// Well-separated planted clusters give an unambiguous known answer (not the
// synthetic-uniform concentration regime; see memory
// reference_hnsw_construction_cost_data_dependent).
func TestVectorSearch_RESTFloatArrayIngestionRoundTrip(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"

	// The declared vector index is #246's "explicit intent signal" that a
	// float-array property on this name should be indexed as a vector.
	if err := server.graph.CreateVectorIndexForTenant(tenantID, "embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	// Create nodes the ONLY way a REST client can: a JSON number array, which
	// the create handler decodes to TypeFloatArray — NOT storage.VectorValue.
	mk := func(labels []string, vec []float64) uint64 {
		t.Helper()
		rr := httptest.NewRecorder()
		server.createNode(rr, reqWithTenant(t, http.MethodPost, "/nodes", NodeRequest{
			Labels:     labels,
			Properties: map[string]any{"embedding": vec},
		}, tenantID))
		if rr.Code != http.StatusCreated {
			t.Fatalf("createNode: want 201, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal create response: %v", err)
		}
		return resp.ID
	}

	// Guard: the stored property really is a TypeFloatArray, not a TypeVector.
	// If a future change started coercing at create time this test would
	// otherwise silently stop exercising the float-array path.
	docExact := mk([]string{"Document"}, []float64{1, 0, 0})
	if got, _ := server.graph.GetNodeForTenant(docExact, tenantID); got == nil {
		t.Fatalf("planted node %d not found in tenant %q", docExact, tenantID)
	} else if pv, ok := got.Properties["embedding"]; !ok || pv.Type != storage.TypeFloatArray {
		t.Fatalf("embedding stored as %v, want TypeFloatArray — REST path not exercised", pv.Type)
	}

	docNear := mk([]string{"Document"}, []float64{0.9, 0.1, 0})
	_ = mk([]string{"Document"}, []float64{0, 1, 0}) // far from +x
	imgExact := mk([]string{"Image"}, []float64{0, 0, 1})
	_ = mk([]string{"Image"}, []float64{0.1, 0.1, 0.9})

	search := func(query []float32, k int) []VectorSearchResult {
		t.Helper()
		rr := httptest.NewRecorder()
		server.handleVectorSearch(rr, reqWithTenant(t, http.MethodPost, "/vector-search", VectorSearchRequest{
			PropertyName: "embedding",
			QueryVector:  query,
			K:            k,
		}, tenantID))
		if rr.Code != http.StatusOK {
			t.Fatalf("vector-search status %d: %s", rr.Code, rr.Body.String())
		}
		var resp VectorSearchResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode search response: %v", err)
		}
		return resp.Results
	}

	// The headline assertion: float-array-ingested vectors are searchable at
	// all (count > 0 was literally 0 before #246), AND the nearest is correct.
	rs := search([]float32{1, 0, 0}, 2)
	if len(rs) != 2 {
		t.Fatalf("got %d results, want 2 — REST-ingested float-array vectors not indexed (the #246 regression)", len(rs))
	}
	if rs[0].NodeID != docExact {
		t.Errorf("top result NodeID=%d, want exact match %d", rs[0].NodeID, docExact)
	}
	got := map[uint64]bool{rs[0].NodeID: true, rs[1].NodeID: true}
	if !got[docExact] || !got[docNear] {
		t.Errorf("top-2 = %v, want Document cluster {%d,%d} — wrong nodes from REST-ingested vectors", got, docExact, docNear)
	}
	if got[imgExact] {
		t.Errorf("Image-cluster node %d ranked in +x top-2 — ranking regression on REST-ingested vectors", imgExact)
	}
}
