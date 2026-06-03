package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// CONSUMER CONTRACT: CC5-label-filtered-vector-search — understand-graphdb, coi-screen (Track Q/Q4)
//
// TestVectorSearch_RESTFloatArrayLabelFilter pins the label-filtered vector path
// on the consumer's real ingestion path: nodes created via POST /nodes with a
// JSON number array (decoded to TypeFloatArray, indexed via #246) and queried
// with filter_labels. Existing label-filter coverage uses in-process
// storage.VectorValue (TypeVector); CC1 covers float-array ingest without a
// label filter. This composes both, exactly as understand-graphdb's neural
// search (filter_labels) and coi-screen exercise it.
//
// docHit is the exact match; the Image is deliberately the nearest WRONG-LABEL
// node (ranked above docNear). The filter must exclude it and return only the
// two Document nodes — proving the post-filter runs on float-array-ingested
// vectors, not just TypeVector ones.
func TestVectorSearch_RESTFloatArrayLabelFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	if err := server.graph.CreateVectorIndexForTenant(tenantID, "embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	mk := func(labels []string, vec []float64) uint64 {
		t.Helper()
		rr := httptest.NewRecorder()
		server.createNode(rr, reqWithTenant(t, http.MethodPost, "/nodes", NodeRequest{
			Labels:     labels,
			Properties: map[string]any{"embedding": vec},
		}, tenantID))
		if rr.Code != http.StatusCreated {
			t.Fatalf("createNode: want 201, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		return resp.ID
	}

	docHit := mk([]string{"Document"}, []float64{1, 0, 0})        // exact match
	imgNearest := mk([]string{"Image"}, []float64{0.97, 0.03, 0}) // nearest wrong-label node
	docNear := mk([]string{"Document"}, []float64{0.9, 0.1, 0})   // near, correct label

	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, reqWithTenant(t, http.MethodPost, "/vector-search", VectorSearchRequest{
		PropertyName: "embedding",
		QueryVector:  []float32{1, 0, 0},
		K:            5,
		FilterLabels: []string{"Document"},
	}, tenantID))
	if rr.Code != http.StatusOK {
		t.Fatalf("vector-search status %d: %s", rr.Code, rr.Body.String())
	}
	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}

	got := map[uint64]bool{}
	for _, r := range resp.Results {
		got[r.NodeID] = true
	}
	if len(resp.Results) != 2 {
		t.Errorf("got %d results, want 2 Document nodes — label filter on float-array-ingested vectors", len(resp.Results))
	}
	if !got[docHit] || !got[docNear] {
		t.Errorf("top-2 = %v, want Document cluster {%d,%d}", got, docHit, docNear)
	}
	if got[imgNearest] {
		t.Errorf("Image node %d (raw-nearest) leaked past filter_labels=[Document]", imgNearest)
	}
}
