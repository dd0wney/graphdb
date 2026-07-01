package graphdb

import (
	"context"
	"net/http"
	"testing"
)

func TestTraverse(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traverse" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"nodes":[{"id":1},{"id":2}]}`))
	})
	got, err := c.Traverse(context.Background(), 1, TraverseOptions{MaxDepth: 2})
	if err != nil || len(got) != 2 {
		t.Fatalf("traverse: %v %+v", err, got)
	}
}

func TestQuery(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"rows":[{"n":"Alice"}]}`))
	})
	res, err := c.Query(context.Background(), "MATCH (n) RETURN n")
	if err != nil || len(res.Rows) != 1 {
		t.Fatalf("query: %v %+v", err, res)
	}
}
