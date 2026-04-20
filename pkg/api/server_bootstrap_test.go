package api

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestBootstrapIndexesFromEnv_FTS exercises the FTS env bootstrap
// path. Sets GRAPHDB_FTS_BOOTSTRAP_LABELS / _PROPERTIES, seeds a
// default-tenant corpus, invokes bootstrap, then asserts /search
// returns results.
func TestBootstrapIndexesFromEnv_FTS(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNode([]string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("graph databases store nodes and edges"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	t.Setenv("GRAPHDB_FTS_BOOTSTRAP_LABELS", "Article")
	t.Setenv("GRAPHDB_FTS_BOOTSTRAP_PROPERTIES", "body")

	server.bootstrapIndexesFromEnv()

	_, resp := searchRequest(t, server, "default", SearchRequest{Query: "graph"})
	if len(resp.Results) != 1 {
		t.Errorf("after bootstrap: want 1 result for 'graph', got %d", len(resp.Results))
	}
}

// TestBootstrapIndexesFromEnv_FTSPartial: labels without properties is
// a misconfiguration; bootstrap should log and skip rather than crash.
func TestBootstrapIndexesFromEnv_FTSPartial(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	t.Setenv("GRAPHDB_FTS_BOOTSTRAP_LABELS", "Article")
	t.Setenv("GRAPHDB_FTS_BOOTSTRAP_PROPERTIES", "")

	server.bootstrapIndexesFromEnv() // must not panic

	// Index remains empty for default tenant.
	_, resp := searchRequest(t, server, "default", SearchRequest{Query: "anything"})
	if len(resp.Results) != 0 {
		t.Errorf("want 0 results with no bootstrap, got %d", len(resp.Results))
	}
}

// TestBootstrapIndexesFromEnv_LSA exercises the LSA env bootstrap
// with a corpus large enough to clear the T >= Dims guard.
func TestBootstrapIndexesFromEnv_LSA(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	bodies := []string{
		"graph databases store nodes edges relationships efficiently",
		"knowledge graphs model entities relationships facts linked",
		"vector embeddings enable semantic similarity neighbor retrieval",
		"hybrid retrieval combines keyword search with dense retrieval",
		"reciprocal rank fusion merges rankings from sources",
		"embedding the query into latent space enables semantic matching",
		"cooking recipes and meal preparation planning",
	}
	for _, body := range bodies {
		if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
			"title": storage.StringValue("t-" + body[:5]),
			"body":  storage.StringValue(body),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_LABELS", "Doc")
	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_TITLE_PROPERTY", "title")
	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES", "body")
	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_DIMS", "6")
	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_MIN_DOC_FREQ", "1") // 7-doc corpus; default 3 is too selective

	server.bootstrapIndexesFromEnv()

	idx := server.lsaIndexes.Get("default")
	if idx == nil {
		t.Fatal("LSA index not registered after bootstrap")
	}
	if idx.NumDocs() != len(bodies) {
		t.Errorf("want %d docs indexed, got %d", len(bodies), idx.NumDocs())
	}
	if idx.Dimensions() != 6 {
		t.Errorf("want dims=6 (from env), got %d", idx.Dimensions())
	}
}

// TestBootstrapIndexesFromEnv_LSACorpusTooSmall: soft-fails (logs,
// does not panic or refuse to boot) when the corpus is too small for
// the configured dims.
func TestBootstrapIndexesFromEnv_LSACorpusTooSmall(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Two docs, tiny vocab — far below default Dims=200.
	for _, body := range []string{"foo bar", "baz qux"} {
		if _, err := server.graph.CreateNode([]string{"Doc"}, map[string]storage.Value{
			"body": storage.StringValue(body),
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_LABELS", "Doc")
	t.Setenv("GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES", "body")
	// Use default dims (200) to guarantee vocab < dims.

	server.bootstrapIndexesFromEnv() // must not panic; should log and continue

	if server.lsaIndexes.Get("default") != nil {
		t.Error("LSA index should not be registered when build fails")
	}
}

// TestBootstrapIndexesFromEnv_NoEnvVars: no env set means no action.
func TestBootstrapIndexesFromEnv_NoEnvVars(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// No env vars set.
	server.bootstrapIndexesFromEnv()

	if server.lsaIndexes.Get("default") != nil {
		t.Error("LSA index should not be registered with no env config")
	}
	// searchIndexes returns an empty FTS index lazily; can't easily
	// assert "not bootstrapped" there without inspecting internals.
	// The FTS partial test above covers the guard path instead.
}
