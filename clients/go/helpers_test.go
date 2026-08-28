package graphdb

import (
	"context"
	"encoding/json"
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

func TestGraphQLSendsVariablesOnlyWhenSet(t *testing.T) {
	var bodies []map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"data":{"node":{"id":"1"}}}`))
	})

	raw, err := c.GraphQL(context.Background(), `{ node { id } }`, nil)
	if err != nil {
		t.Fatalf("graphql: %v", err)
	}
	if string(raw) != `{"data":{"node":{"id":"1"}}}` {
		t.Errorf("raw = %s", raw)
	}
	if _, err := c.GraphQL(context.Background(), `query($id: ID!) { node(id: $id) { id } }`,
		map[string]any{"id": "1"}); err != nil {
		t.Fatalf("graphql with vars: %v", err)
	}

	if _, ok := bodies[0]["variables"]; ok {
		t.Errorf("nil variables must be omitted, body=%v", bodies[0])
	}
	if vars, ok := bodies[1]["variables"].(map[string]any); !ok || vars["id"] != "1" {
		t.Errorf("variables = %v, want id=1", bodies[1]["variables"])
	}
}
