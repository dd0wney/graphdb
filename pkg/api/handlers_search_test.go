package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// searchServerWithCorpus builds a test server, seeds a small corpus
// under tenantID, and indexes it. Returns the server + cleanup func.
func searchServerWithCorpus(t *testing.T, tenantID string) (*Server, func()) {
	t.Helper()
	server, cleanup := setupTestServer(t)

	docs := []struct {
		labels []string
		title  string
		body   string
	}{
		{[]string{"Article"}, "graph database systems", "a graph database stores nodes and edges for fast traversal"},
		{[]string{"Article"}, "machine learning basics", "supervised learning tunes weights using labeled examples"},
		{[]string{"Article"}, "graph neural networks", "graph neural networks propagate embeddings across connected nodes"},
		{[]string{"Note"}, "graph stream thoughts", "streams of graph updates arrive faster than batch processing handles"},
		{[]string{"Note"}, "unrelated topic", "quantum entanglement and the fabric of spacetime"},
	}
	for _, d := range docs {
		if _, err := server.graph.CreateNodeWithTenant(tenantID, d.labels, map[string]storage.Value{
			"title": storage.StringValue(d.title),
			"body":  storage.StringValue(d.body),
		}); err != nil {
			cleanup()
			t.Fatalf("create %q: %v", d.title, err)
		}
	}
	if err := server.searchIndexes.IndexForTenant(tenantID, []string{"Article", "Note"}, []string{"title", "body"}); err != nil {
		cleanup()
		t.Fatalf("IndexForTenant: %v", err)
	}
	return server, cleanup
}

func searchRequest(t *testing.T, server *Server, tenantID string, body SearchRequest) (*httptest.ResponseRecorder, SearchResponse) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/search", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	server.handleSearch(rr, req)
	var resp SearchResponse
	if rr.Code == http.StatusOK {
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	}
	return rr, resp
}

// TestSearch_MethodNotAllowed rejects non-POST.
func TestSearch_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/search", nil)
		rr := httptest.NewRecorder()
		server.handleSearch(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rr.Code)
		}
	}
}

// TestSearch_EmptyQuery returns 400 for whitespace-only query too.
func TestSearch_EmptyQuery(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, q := range []string{"", "   ", "\t\n"} {
		rr, _ := searchRequest(t, server, "default", SearchRequest{Query: q})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("query=%q: want 400, got %d", q, rr.Code)
		}
	}
}

// TestSearch_InvalidBody returns 400 on undecodeable JSON.
func TestSearch_InvalidBody(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/search", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleSearch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

// TestSearch_HappyPath finds ranked results for a common term.
func TestSearch_HappyPath(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	rr, resp := searchRequest(t, server, "default", SearchRequest{Query: "graph"})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(resp.Results) == 0 {
		t.Fatalf("want >= 1 result, got 0")
	}
	// Scores should be descending.
	for i := 1; i < len(resp.Results); i++ {
		if resp.Results[i].Score > resp.Results[i-1].Score {
			t.Errorf("results not ranked by score descending at index %d", i)
		}
	}
	if resp.Count != len(resp.Results) {
		t.Errorf("Count=%d but Results length=%d", resp.Count, len(resp.Results))
	}
	if resp.TookMs < 0 {
		t.Errorf("TookMs should be >= 0, got %d", resp.TookMs)
	}
}

// TestSearch_LimitClamp caps Limit at 100 without erroring.
func TestSearch_LimitClamp(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	rr, resp := searchRequest(t, server, "default", SearchRequest{Query: "graph", Limit: 500})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(resp.Results) > searchMaxLimit {
		t.Errorf("Limit=500 produced %d results, want <= %d", len(resp.Results), searchMaxLimit)
	}
}

// TestSearch_OffsetPagination skips the first Offset results.
func TestSearch_OffsetPagination(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	_, page1 := searchRequest(t, server, "default", SearchRequest{Query: "graph", Limit: 2, Offset: 0})
	_, page2 := searchRequest(t, server, "default", SearchRequest{Query: "graph", Limit: 2, Offset: 2})

	if len(page1.Results) == 0 || len(page2.Results) == 0 {
		t.Fatalf("both pages should have results: page1=%d page2=%d", len(page1.Results), len(page2.Results))
	}
	// Any NodeID in page1 must not appear in page2.
	seen := make(map[uint64]bool, len(page1.Results))
	for _, r := range page1.Results {
		seen[r.NodeID] = true
	}
	for _, r := range page2.Results {
		if seen[r.NodeID] {
			t.Errorf("NodeID %d appears in both page1 and page2 — pagination broken", r.NodeID)
		}
	}
}

// TestSearch_LabelFilterMatch returns only results with a matching label.
func TestSearch_LabelFilterMatch(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	_, resp := searchRequest(t, server, "default", SearchRequest{
		Query:        "graph",
		Labels:       []string{"Article"},
		IncludeNodes: true,
	})
	if len(resp.Results) == 0 {
		t.Fatalf("want >= 1 Article result for 'graph'")
	}
	for _, r := range resp.Results {
		if r.Node == nil {
			t.Errorf("result %d: IncludeNodes=true but Node is nil", r.NodeID)
			continue
		}
		if !hasAnyLabel(r.Node.Labels, []string{"Article"}) {
			t.Errorf("result %d has labels %v; want 'Article'", r.NodeID, r.Node.Labels)
		}
	}
}

// TestSearch_LabelFilterExclude returns zero when no label matches.
func TestSearch_LabelFilterExclude(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	_, resp := searchRequest(t, server, "default", SearchRequest{
		Query:  "graph",
		Labels: []string{"NonExistentLabel"},
	})
	if len(resp.Results) != 0 {
		t.Errorf("want 0 results with impossible label filter, got %d", len(resp.Results))
	}
}

// TestSearch_TenantIsolation is the cross-tenant isolation gate: a query
// against tenant B must not surface tenant A's indexed content.
func TestSearch_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Tenant A: "alpha" appears here only.
	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("alpha secret tenant A content"),
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	// Tenant B: "beta" appears here only.
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Article"}, map[string]storage.Value{
		"body": storage.StringValue("beta public tenant B content"),
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}
	if err := server.searchIndexes.IndexForTenant("tenant-A", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant A: %v", err)
	}
	if err := server.searchIndexes.IndexForTenant("tenant-B", []string{"Article"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant B: %v", err)
	}

	t.Run("tenant A finds its own", func(t *testing.T) {
		_, resp := searchRequest(t, server, "tenant-A", SearchRequest{Query: "alpha"})
		if len(resp.Results) != 1 {
			t.Errorf("tenant A 'alpha': want 1 result, got %d", len(resp.Results))
		}
	})
	t.Run("tenant B does not see A", func(t *testing.T) {
		_, resp := searchRequest(t, server, "tenant-B", SearchRequest{Query: "alpha"})
		if len(resp.Results) != 0 {
			t.Errorf("tenant B 'alpha' (A's content): want 0 (isolation breach), got %d", len(resp.Results))
		}
	})
	t.Run("tenant B finds its own", func(t *testing.T) {
		_, resp := searchRequest(t, server, "tenant-B", SearchRequest{Query: "beta"})
		if len(resp.Results) != 1 {
			t.Errorf("tenant B 'beta': want 1 result, got %d", len(resp.Results))
		}
	})
	t.Run("unindexed tenant returns empty", func(t *testing.T) {
		_, resp := searchRequest(t, server, "tenant-C", SearchRequest{Query: "alpha"})
		if len(resp.Results) != 0 {
			t.Errorf("unindexed tenant: want 0 results, got %d", len(resp.Results))
		}
	})
}

// TestSearch_IncludeContentSnippet returns a rune-safe truncated snippet
// when IncludeContent is true.
func TestSearch_IncludeContentSnippet(t *testing.T) {
	server, cleanup := searchServerWithCorpus(t, "default")
	defer cleanup()

	_, resp := searchRequest(t, server, "default", SearchRequest{
		Query:          "graph",
		Limit:          1,
		IncludeContent: true,
	})
	if len(resp.Results) == 0 {
		t.Fatalf("want 1 result")
	}
	if resp.Results[0].Snippet == "" {
		t.Errorf("IncludeContent=true but snippet is empty")
	}
	if ln := len([]rune(resp.Results[0].Snippet)); ln > searchSnippetRunes {
		t.Errorf("snippet length %d > max %d", ln, searchSnippetRunes)
	}
}
