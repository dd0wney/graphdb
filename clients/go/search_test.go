package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestSearchFullText(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"node_id":3,"score":0.9,"snippet":"hi"}]}`))
	})
	hits, err := c.Search.FullText(context.Background(), "graph", SearchOptions{Limit: 5})
	if err != nil || len(hits) != 1 || hits[0].NodeID != 3 {
		t.Fatalf("fulltext: %v %+v", err, hits)
	}
}

func TestSearchVector(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vector-search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"node_id":4,"distance":0.1,"score":0.9}]}`))
	})
	hits, err := c.Search.Vector(context.Background(), "embedding", []float64{0.1, 0.2}, VectorOptions{K: 5})
	if err != nil || len(hits) != 1 || hits[0].NodeID != 4 {
		t.Fatalf("vector: %v %+v", err, hits)
	}
}

func TestSearchListIndexesParsesEnvelope(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vector-indexes" || r.Method != http.MethodGet {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"indexes":[{"property_name":"embedding","dimensions":384}],"count":1}`))
	})
	got, err := c.Search.ListIndexes(context.Background())
	if err != nil {
		t.Fatalf("listindexes: %v", err)
	}
	if len(got) != 1 || got[0].PropertyName != "embedding" || got[0].Dimensions != 384 {
		t.Fatalf("parsed = %+v, want one index embedding/384", got)
	}
}

func TestSearchHybridSendsAlphaOnlyWhenSet(t *testing.T) {
	var bodies []map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hybrid-search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"results":[{"node_id":5,"score":0.8}],"degraded":"vector index cold"}`))
	})

	res, err := c.Search.Hybrid(context.Background(), "q", HybridOptions{})
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].NodeID != 5 || res.Degraded != "vector index cold" {
		t.Fatalf("result = %+v, want node 5 + degraded reason", res)
	}
	alpha := 0.7
	if _, err := c.Search.Hybrid(context.Background(), "q", HybridOptions{Alpha: &alpha}); err != nil {
		t.Fatalf("hybrid with alpha: %v", err)
	}

	if _, ok := bodies[0]["alpha"]; ok {
		t.Errorf("nil Alpha must be omitted, body=%v", bodies[0])
	}
	if got, ok := bodies[1]["alpha"]; !ok || got != 0.7 {
		t.Errorf("alpha = %v, want 0.7", got)
	}
}

func TestSearchCreateIndex(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/vector-indexes" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["property_name"] != "embedding" || body["dimensions"] != float64(384) {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"property_name":"embedding","dimensions":384,"metric":"cosine"}`))
	})
	idx, err := c.Search.CreateIndex(context.Background(), "embedding", 384)
	if err != nil || idx.Metric != "cosine" {
		t.Fatalf("createindex: %v %+v", err, idx)
	}
}

// A property name containing a slash must be path-escaped, not become an
// extra URL segment.
func TestSearchGetIndexEscapesPropertyName(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/vector-indexes/vec%2Fdim" {
			t.Errorf("escaped path = %q, want /vector-indexes/vec%%2Fdim", got)
		}
		_, _ = w.Write([]byte(`{"property_name":"vec/dim","dimensions":8}`))
	})
	idx, err := c.Search.GetIndex(context.Background(), "vec/dim")
	if err != nil || idx.PropertyName != "vec/dim" {
		t.Fatalf("getindex: %v %+v", err, idx)
	}
}
