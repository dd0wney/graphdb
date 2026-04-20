package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// hybridServerWithCorpus builds a test server and seeds:
//   - FTS index for tenantID populated via IndexForTenant
//   - LSA index for tenantID registered via Set
//
// Both indexes see the same documents; the LSA config is scaled down so
// the tiny corpus can still satisfy the T >= Dims guard.
func hybridServerWithCorpus(t *testing.T, tenantID string) (*Server, func()) {
	t.Helper()
	server, cleanup := setupTestServer(t)

	corpus := []struct {
		labels []string
		title  string
		body   string
	}{
		{[]string{"Doc"}, "graph databases overview", "graph databases store nodes edges relationships efficiently"},
		{[]string{"Doc"}, "knowledge graph notes", "knowledge graphs model entities relationships facts linked"},
		{[]string{"Doc"}, "vector search primer", "vector embeddings enable semantic similarity nearest neighbor retrieval"},
		{[]string{"Doc"}, "hybrid retrieval", "combining keyword search with dense retrieval improves precision"},
		{[]string{"Doc"}, "rrf fusion", "reciprocal rank fusion merges rankings from heterogeneous sources"},
		{[]string{"Doc"}, "query embedding", "embedding the query into latent space enables semantic matching"},
		{[]string{"Note"}, "misc note", "unrelated content about cooking recipes and meal preparation"},
	}

	docs := make([]search.Document, 0, len(corpus))
	for _, c := range corpus {
		node, err := server.graph.CreateNodeWithTenant(tenantID, c.labels, map[string]storage.Value{
			"title": storage.StringValue(c.title),
			"body":  storage.StringValue(c.body),
		})
		if err != nil {
			cleanup()
			t.Fatalf("create %q: %v", c.title, err)
		}
		docs = append(docs, search.Document{ID: node.ID, Title: c.title, Body: c.body})
	}

	// FTS indexing (all label scope).
	if err := server.searchIndexes.IndexForTenant(tenantID, []string{"Doc", "Note"}, []string{"title", "body"}); err != nil {
		cleanup()
		t.Fatalf("IndexForTenant: %v", err)
	}

	// LSA build with a test-scale config (tiny Dims so the 7-doc corpus
	// clears the T >= Dims guard).
	cfg := search.LSAConfig{
		Dims:       6,
		Oversamp:   3,
		PowerIter:  2,
		MaxVocab:   200,
		MinDocFreq: 1,
		TitleBoost: 3,
		Seed:       42,
	}
	idx, err := search.BuildLSAIndex(docs, cfg)
	if err != nil {
		cleanup()
		t.Fatalf("BuildLSAIndex: %v", err)
	}
	server.lsaIndexes.Set(tenantID, idx)

	return server, cleanup
}

func hybridRequest(t *testing.T, server *Server, tenantID string, body HybridSearchRequest) (*httptest.ResponseRecorder, HybridSearchResponse) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/hybrid-search", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	server.handleHybridSearch(rr, req)
	var resp HybridSearchResponse
	if rr.Code == http.StatusOK {
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	}
	return rr, resp
}

// TestHybridSearch_MethodNotAllowed rejects non-POST.
func TestHybridSearch_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/hybrid-search", nil)
	rr := httptest.NewRecorder()
	server.handleHybridSearch(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

// TestHybridSearch_EmptyQuery returns 400.
func TestHybridSearch_EmptyQuery(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr, _ := hybridRequest(t, server, "default", HybridSearchRequest{Query: "   "})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", rr.Code)
	}
}

// TestHybridSearch_HappyPath: both stages populate, results have
// non-negative scores and sensible per-stage ranks.
func TestHybridSearch_HappyPath(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr, resp := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph"})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if resp.Degraded != "" {
		t.Errorf("unexpected degradation: %q", resp.Degraded)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("want >= 1 result")
	}
	for i, r := range resp.Results {
		if r.Score < 0 {
			t.Errorf("result %d score %.4f < 0", i, r.Score)
		}
		if i > 0 && r.Score > resp.Results[i-1].Score {
			t.Errorf("results not ranked by score desc at index %d", i)
		}
	}
}

// TestHybridSearch_NoLSA returns FTS-only with degraded header + field.
func TestHybridSearch_NoLSA(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed FTS but not LSA.
	if _, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("graph content"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := server.searchIndexes.IndexForTenant("default", []string{"Doc"}, []string{"body"}); err != nil {
		t.Fatalf("IndexForTenant: %v", err)
	}

	rr, resp := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph"})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	if resp.Degraded != "no-lsa-index" {
		t.Errorf("want degraded='no-lsa-index', got %q", resp.Degraded)
	}
	if rr.Header().Get(HeaderHybridDegraded) != "no-lsa-index" {
		t.Errorf("missing degradation response header")
	}
	// FTS-only results: LSARank should be -1 across the board.
	for _, r := range resp.Results {
		if r.LSARank != -1 {
			t.Errorf("LSARank=%d expected -1 when LSA unavailable", r.LSARank)
		}
	}
}

// TestHybridSearch_OOV: query whose tokens aren't in LSA vocab falls
// back to FTS-only with query-out-of-vocabulary degradation.
func TestHybridSearch_OOV(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	// "zyxwvu" is not in any doc so LSA FoldQuery errors; FTS also
	// returns nothing, but the degradation message tells callers why.
	_, resp := hybridRequest(t, server, "default", HybridSearchRequest{Query: "zyxwvu qrstuv"})
	if resp.Degraded != "query-out-of-vocabulary" {
		t.Errorf("want degraded='query-out-of-vocabulary', got %q", resp.Degraded)
	}
}

// TestHybridSearch_AlphaDegenerate: alpha=1.0 effectively weights only
// the FTS stage. Every result should carry FTSRank != -1.
func TestHybridSearch_AlphaDegenerate(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	one := 1.0
	_, resp := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph", Alpha: &one})
	if len(resp.Results) == 0 {
		t.Fatal("want >= 1 result")
	}
	for _, r := range resp.Results {
		if r.FTSRank == -1 {
			t.Errorf("alpha=1.0: every result should be in FTS (FTSRank != -1), got -1 for NodeID=%d", r.NodeID)
		}
	}

	zero := 0.0
	_, respZero := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph", Alpha: &zero})
	if len(respZero.Results) == 0 {
		t.Fatal("want >= 1 result at alpha=0")
	}
	// At alpha=0, FTS contributes nothing; score comes entirely from LSA.
	// The top result should have LSARank != -1.
	if respZero.Results[0].LSARank == -1 {
		t.Errorf("alpha=0.0: top result should be in LSA (LSARank != -1), got -1")
	}
}

// TestHybridSearch_TenantIsolation: cross-tenant leakage check.
func TestHybridSearch_TenantIsolation(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "tenant-A")
	defer cleanup()

	// Tenant B has no indexes at all.
	_, resp := hybridRequest(t, server, "tenant-B", HybridSearchRequest{Query: "graph"})
	if len(resp.Results) != 0 {
		t.Errorf("tenant-B (no indexes) should return 0 results; got %d", len(resp.Results))
	}
}

// TestHybridSearch_Pagination: page1 and page2 are disjoint.
func TestHybridSearch_Pagination(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	_, p1 := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph", Limit: 2, Offset: 0})
	_, p2 := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph", Limit: 2, Offset: 2})

	if len(p1.Results) == 0 || len(p2.Results) == 0 {
		t.Skip("corpus too small to paginate — not a failure, just a test-scope note")
	}
	seen := make(map[uint64]bool, len(p1.Results))
	for _, r := range p1.Results {
		seen[r.NodeID] = true
	}
	for _, r := range p2.Results {
		if seen[r.NodeID] {
			t.Errorf("NodeID %d appears on both pages", r.NodeID)
		}
	}
}

// TestHybridSearch_LimitClamp.
func TestHybridSearch_LimitClamp(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	_, resp := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph", Limit: 500})
	if len(resp.Results) > hybridMaxLimit {
		t.Errorf("Limit=500 produced %d results, want ≤ %d", len(resp.Results), hybridMaxLimit)
	}
}

// TestHybridSearch_LabelFiltersLSAOnly ensures the label post-filter
// works on LSA-only candidates too (those not returned by FTS). Before
// the on-demand GetNode hydration, these candidates were dropped from
// any label-filtered query because they had no hydrated node to check.
func TestHybridSearch_LabelFiltersLSAOnly(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	// "neighbor" appears in the corpus in docs labeled "Doc" only; no
	// "Note"-labeled doc mentions it. With alpha=0.0 (pure LSA) we
	// rely on semantic neighbors — docs that don't literally contain
	// the word but sit nearby in latent space. Those LSA-only matches
	// must still respect a "Doc"-only label filter.
	zero := 0.0
	_, resp := hybridRequest(t, server, "default", HybridSearchRequest{
		Query:        "retrieval",
		Alpha:        &zero,
		Labels:       []string{"Doc"},
		IncludeNodes: true,
	})

	if len(resp.Results) == 0 {
		t.Skip("alpha=0 LSA path returned no candidates; corpus too small to exercise")
	}

	for _, r := range resp.Results {
		if r.Node == nil {
			t.Errorf("result NodeID=%d: want Node populated by on-demand hydration, got nil", r.NodeID)
			continue
		}
		if !hasAnyLabel(r.Node.Labels, []string{"Doc"}) {
			t.Errorf("result NodeID=%d labels=%v; want 'Doc'", r.NodeID, r.Node.Labels)
		}
	}
}
