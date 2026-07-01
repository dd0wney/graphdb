package graphdb

import (
	"context"
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
