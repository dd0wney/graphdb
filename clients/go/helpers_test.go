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

func TestEmbeddingsParsesOpenAIEnvelope(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"lsa"}`))
	})
	res, err := c.Embeddings(context.Background(), []string{"hi"})
	if err != nil {
		t.Fatalf("embeddings: %v", err)
	}
	vecs := res.Vectors()
	if len(vecs) != 1 || len(vecs[0]) != 2 || vecs[0][0] != 0.1 {
		t.Fatalf("vectors = %v, want [[0.1 0.2]]", vecs)
	}
}

func TestRetrieveParsesLangChainEnvelope(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/retrieve" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"documents":[{"page_content":"hello","metadata":{"node_id":7,"score":0.9}}],"took_ms":3}`))
	})
	res, err := c.Retrieve(context.Background(), "q", 5)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(res.Documents) != 1 || res.Documents[0].PageContent != "hello" ||
		res.Documents[0].Metadata.NodeID != 7 || res.Documents[0].Metadata.Score != 0.9 {
		t.Fatalf("documents = %+v, want one hello/node7/0.9", res.Documents)
	}
}
