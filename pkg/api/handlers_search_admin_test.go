package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// adminReq is a small harness for posting admin index requests with
// tenant context injected.
func adminReq(t *testing.T, server *Server, path string, tenantID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	switch path {
	case "/search/index":
		server.handleSearchIndex(rr, req)
	case "/hybrid-search/lsa-index":
		server.handleLSAIndex(rr, req)
	}
	return rr
}

// --- POST /search/index ---

func TestSearchIndex_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/search/index", nil)
	rr := httptest.NewRecorder()
	server.handleSearchIndex(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestSearchIndex_ValidationErrors(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		body SearchIndexRequest
	}{
		{"empty labels", SearchIndexRequest{Labels: nil, Properties: []string{"body"}}},
		{"empty properties", SearchIndexRequest{Labels: []string{"Doc"}, Properties: nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := adminReq(t, server, "/search/index", "default", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("want 400, got %d", rr.Code)
			}
		})
	}
}

func TestSearchIndex_HappyPathEnablesSearch(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("metempsychosis bookseller"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("bloom hears it from the bookseller"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Before indexing, /search returns 0.
	_, resp := searchRequest(t, server, "default", SearchRequest{Query: "bookseller"})
	if len(resp.Results) != 0 {
		t.Errorf("pre-index: want 0 results, got %d", len(resp.Results))
	}

	// Build the index.
	rr := adminReq(t, server, "/search/index", "default", SearchIndexRequest{
		Labels:     []string{"Doc"},
		Properties: []string{"body"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("index build: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var built SearchIndexResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &built); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if built.IndexedNodes != 2 {
		t.Errorf("want 2 indexed, got %d", built.IndexedNodes)
	}

	// After indexing, /search finds both.
	_, resp = searchRequest(t, server, "default", SearchRequest{Query: "bookseller"})
	if len(resp.Results) != 2 {
		t.Errorf("post-index: want 2 results, got %d", len(resp.Results))
	}
}

func TestSearchIndex_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if _, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("alpha content"),
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("beta content"),
	}); err != nil {
		t.Fatalf("create B: %v", err)
	}

	// Index only tenant-A.
	rr := adminReq(t, server, "/search/index", "tenant-A", SearchIndexRequest{
		Labels:     []string{"Doc"},
		Properties: []string{"body"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("index A: %s", rr.Body.String())
	}

	// Tenant A's index should only contain tenant A's node.
	_, respA := searchRequest(t, server, "tenant-A", SearchRequest{Query: "alpha"})
	if len(respA.Results) != 1 {
		t.Errorf("tenant A: want 1 result, got %d", len(respA.Results))
	}
	_, respACross := searchRequest(t, server, "tenant-A", SearchRequest{Query: "beta"})
	if len(respACross.Results) != 0 {
		t.Errorf("tenant A should not see 'beta' from tenant B, got %d results", len(respACross.Results))
	}
}

// --- POST /hybrid-search/lsa-index ---

func TestLSAIndex_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/hybrid-search/lsa-index", nil)
	rr := httptest.NewRecorder()
	server.handleLSAIndex(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", rr.Code)
	}
}

func TestLSAIndex_ValidationErrors(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		body LSAIndexRequest
	}{
		{"empty labels", LSAIndexRequest{Labels: nil, BodyProperties: []string{"body"}}},
		{"empty body_properties", LSAIndexRequest{Labels: []string{"Doc"}, BodyProperties: nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := adminReq(t, server, "/hybrid-search/lsa-index", "default", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("want 400, got %d", rr.Code)
			}
		})
	}
}

// TestLSAIndex_CorpusTooSmall returns 422 (Unprocessable) when vocab
// can't cover the requested Dims.
func TestLSAIndex_CorpusTooSmall(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Two trivial docs — vocab will be ~4 terms, less than default Dims=200.
	if _, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("foo bar"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("baz quux"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	rr := adminReq(t, server, "/hybrid-search/lsa-index", "default", LSAIndexRequest{
		Labels:         []string{"Doc"},
		BodyProperties: []string{"body"},
	})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("want 422 for too-small corpus, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestSearchIndexAdmin_RoleGuard exercises the middleware chain for
// the index-admin routes (requireAdmin → withTenant → handler): admin
// users pass, viewer/non-admin users get 403. This is the end-to-end
// check that complements the handler-level tests which bypass middleware.
func TestSearchIndexAdmin_RoleGuard(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	adminUser, err := server.userStore.CreateUser("idx-admin", "pw-admin-12345", auth.RoleAdmin)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	viewerUser, err := server.userStore.CreateUser("idx-viewer", "pw-viewer-12345", auth.RoleViewer)
	if err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	adminTok, err := server.jwtManager.GenerateToken(adminUser.ID, adminUser.Username, adminUser.Role)
	if err != nil {
		t.Fatalf("token admin: %v", err)
	}
	viewerTok, err := server.jwtManager.GenerateToken(viewerUser.ID, viewerUser.Username, viewerUser.Role)
	if err != nil {
		t.Fatalf("token viewer: %v", err)
	}

	chain := server.requireAdmin(server.withTenant(server.handleSearchIndex))

	body, _ := json.Marshal(SearchIndexRequest{
		Labels:     []string{"Doc"},
		Properties: []string{"body"},
	})

	cases := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{"admin 200", adminTok, http.StatusOK},
		{"viewer 403", viewerTok, http.StatusForbidden},
		{"no token 401", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/search/index", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rr := httptest.NewRecorder()
			chain(rr, req)
			if rr.Code != tc.wantStatus {
				t.Errorf("want %d, got %d: %s", tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestLSAIndex_HappyPathEnablesHybrid builds an LSA index via the admin
// endpoint, then verifies /hybrid-search no longer reports degraded.
func TestLSAIndex_HappyPathEnablesHybrid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	corpus := []string{
		"graph databases store nodes edges relationships efficiently",
		"knowledge graphs model entities relationships facts linked",
		"vector embeddings enable semantic similarity nearest neighbor",
		"hybrid retrieval combines keyword search with dense retrieval",
		"reciprocal rank fusion merges rankings from sources",
		"embedding the query into latent space enables semantic matching",
		"cooking recipes and meal preparation planning",
	}
	for i, body := range corpus {
		_, err := server.graph.CreateNodeWithTenant("default", []string{"Doc"}, map[string]storage.Value{
			"title": storage.StringValue("doc-" + string(rune('A'+i))),
			"body":  storage.StringValue(body),
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	// Seed FTS too so /hybrid-search has a working FTS stage.
	if err := server.searchIndexes.IndexForTenant("default", []string{"Doc"}, []string{"title", "body"}); err != nil {
		t.Fatalf("IndexForTenant: %v", err)
	}

	// Before LSA build → /hybrid-search reports degraded=no-lsa-index.
	_, respDegraded := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph"})
	if respDegraded.Degraded != "no-lsa-index" {
		t.Errorf("pre-build: want degraded='no-lsa-index', got %q", respDegraded.Degraded)
	}

	// Build LSA with a test-scale Dims that the 7-doc corpus can satisfy.
	rr := adminReq(t, server, "/hybrid-search/lsa-index", "default", LSAIndexRequest{
		Labels:         []string{"Doc"},
		TitleProperty:  "title",
		BodyProperties: []string{"body"},
		Dims:           6,
		MinDocFreq:     1,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("lsa build: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var built LSAIndexResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &built); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if built.IndexedDocs != 7 {
		t.Errorf("want 7 indexed docs, got %d", built.IndexedDocs)
	}
	if built.Dimensions != 6 {
		t.Errorf("want Dimensions=6, got %d", built.Dimensions)
	}

	// After build → /hybrid-search no longer degraded.
	_, respOK := hybridRequest(t, server, "default", HybridSearchRequest{Query: "graph"})
	if respOK.Degraded != "" {
		t.Errorf("post-build: want no degradation, got %q", respOK.Degraded)
	}
}
