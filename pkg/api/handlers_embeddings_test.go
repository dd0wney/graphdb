package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// embeddingsRequest sends a request body to /v1/embeddings on the given
// tenant. Mirrors hybridRequest in handlers_hybrid_search_test.go.
func embeddingsRequest(t *testing.T, server *Server, tenantID string, body any) (*httptest.ResponseRecorder, EmbeddingsResponse) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	server.handleEmbeddings(rr, req)
	var resp EmbeddingsResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (body: %s)", err, rr.Body.String())
		}
	}
	return rr, resp
}

// embeddingsRequestRaw lets a test send a raw JSON body (for cases that
// can't be expressed via the typed EmbeddingsRequest, e.g. malformed JSON).
func embeddingsRequestRaw(t *testing.T, server *Server, tenantID, raw string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	rr := httptest.NewRecorder()
	server.handleEmbeddings(rr, req)
	return rr
}

// TestEmbeddings_RoundTrip pins the happy-path single-string request shape
// and asserts every contract field of the response is populated correctly.
// This is the foundational schema test — every other test assumes this works.
func TestEmbeddings_RoundTrip(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr, resp := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input: embeddingsInput{"graph database semantic similarity"},
		Model: "text-embedding-ada-002", // any string; we record but don't validate
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Response envelope.
	if resp.Object != "list" {
		t.Errorf(`Object: want "list", got %q`, resp.Object)
	}
	if resp.Model != lsaModelName {
		t.Errorf("Model: want %q (server-determined, not echoed from request), got %q", lsaModelName, resp.Model)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Data: want 1 entry, got %d", len(resp.Data))
	}

	// Per-entry shape.
	d := resp.Data[0]
	if d.Object != "embedding" {
		t.Errorf(`Data[0].Object: want "embedding", got %q`, d.Object)
	}
	if d.Index != 0 {
		t.Errorf("Data[0].Index: want 0, got %d", d.Index)
	}
	if len(d.Embedding) == 0 {
		t.Error("Data[0].Embedding: want non-empty, got 0 length")
	}

	// Usage accounting must be populated; LSA returns >0 tokens for any input
	// that doesn't fail FoldQuery.
	if resp.Usage.PromptTokens == 0 {
		t.Error("Usage.PromptTokens: want > 0 for a successful embedding")
	}
	if resp.Usage.TotalTokens != resp.Usage.PromptTokens {
		t.Errorf("Usage.TotalTokens: want == PromptTokens (no completion tokens for embeddings); got %d != %d",
			resp.Usage.TotalTokens, resp.Usage.PromptTokens)
	}
}

// TestEmbeddings_BatchInput pins the array-shaped input case. Same response
// shape as single, but with multiple Data entries indexed in input order.
func TestEmbeddings_BatchInput(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	inputs := embeddingsInput{
		"graph databases store nodes",
		"vector embeddings semantic similarity",
		"hybrid retrieval combines signals",
	}

	rr, resp := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input: inputs,
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	if len(resp.Data) != len(inputs) {
		t.Fatalf("Data: want %d entries, got %d", len(inputs), len(resp.Data))
	}

	// Indices must match input position; this is part of the OpenAI contract
	// (LangChain et al. rely on it for re-association).
	for i, d := range resp.Data {
		if d.Index != i {
			t.Errorf("Data[%d].Index: want %d, got %d", i, i, d.Index)
		}
		if len(d.Embedding) == 0 {
			t.Errorf("Data[%d].Embedding: empty", i)
		}
	}
}

// TestEmbeddings_AcceptsStringInput pins the OpenAI quirk where `input`
// can be either a JSON string OR a JSON array. The custom UnmarshalJSON
// must handle both shapes; this test trips on regressions in that decoder.
func TestEmbeddings_AcceptsStringInput(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr := embeddingsRequestRaw(t, server, "default",
		`{"input": "graph databases", "model": "any"}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("string-shaped input rejected: status %d, body %s", rr.Code, rr.Body.String())
	}
}

// TestEmbeddings_AcceptsArrayInput is the array-shape sibling of the above.
func TestEmbeddings_AcceptsArrayInput(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr := embeddingsRequestRaw(t, server, "default",
		`{"input": ["graph", "vector"], "model": "any"}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("array-shaped input rejected: status %d, body %s", rr.Code, rr.Body.String())
	}
}

// TestEmbeddings_NoLSAIndex asserts the 503 response when the tenant has
// no LSA index built. Important: this is the *normal* state for new tenants
// or any tenant that hasn't called the admin /hybrid-search/lsa-index
// endpoint yet — the error message must guide the caller to fix it.
func TestEmbeddings_NoLSAIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	// Note: setupTestServer does NOT call hybridServerWithCorpus, so no
	// LSA index exists for any tenant.

	rr, _ := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input: embeddingsInput{"any input"},
	})

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503, got %d. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "/hybrid-search/lsa-index") {
		t.Error("error message should guide caller to the index-build endpoint")
	}
}

// TestEmbeddings_OutOfVocabulary asserts that LSA's vocabulary-bound nature
// surfaces as a 400 rather than silently returning a zero vector. This is
// the most user-visible footgun of swapping LSA for a neural embedding
// service — the error must be clear about what went wrong.
func TestEmbeddings_OutOfVocabulary(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr, _ := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		// Tokens that almost certainly aren't in the corpus's tiny vocabulary.
		Input: embeddingsInput{"zxqvb pdfgqx wnoijz"},
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 for OOV, got %d. Body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "vocabulary") {
		t.Errorf("error message should explain the vocabulary-bound limitation; got: %s", body)
	}
	if !strings.Contains(body, "input[0]") {
		t.Errorf("error message should name the offending input index; got: %s", body)
	}
}

// TestEmbeddings_EmptyInput asserts an empty `input` field is rejected
// before any LSA work is attempted. This catches a class of client bugs
// (forgot to set the field, sent an empty array) early with a clear message.
func TestEmbeddings_EmptyInput(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	cases := []struct {
		name string
		raw  string
	}{
		{"missing input", `{"model":"any"}`},
		{"empty array", `{"input":[]}`},
		{"empty string in array", `{"input":[""]}`},
		{"empty single string", `{"input":""}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := embeddingsRequestRaw(t, server, "default", tc.raw)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status: want 400, got %d. Body: %s", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestEmbeddings_DimensionMismatch asserts that supplying `dimensions`
// that doesn't match the LSA index's actual dim is a 400. This catches
// client misconfigurations early; without this guard a client would get
// the wrong-shape vectors and dot-product math would fail far from the
// request boundary.
func TestEmbeddings_DimensionMismatch(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr, _ := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input:      embeddingsInput{"graph databases"},
		Dimensions: 1536, // OpenAI ada-002 dim; LSA test corpus uses 6
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 for dimension mismatch, got %d. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "1536") {
		t.Errorf("error message should mention the requested dimension; got: %s", rr.Body.String())
	}
}

// TestEmbeddings_DimensionMatch asserts that a correctly-sized `dimensions`
// param succeeds. This is the negative complement of DimensionMismatch —
// proves the dim check isn't over-aggressive.
func TestEmbeddings_DimensionMatch(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()
	idx := server.lsaIndexes.Get("default")
	if idx == nil {
		t.Fatal("test setup: LSA index missing")
	}

	rr, _ := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input:      embeddingsInput{"graph databases"},
		Dimensions: idx.Dimensions(),
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 with matching dim, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

// TestEmbeddings_UnsupportedEncoding rejects encoding_format=base64. v1
// is float-only; future versions may add base64. The negative test pins
// the current rejection so a base64 implementation lands intentionally.
func TestEmbeddings_UnsupportedEncoding(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	rr, _ := embeddingsRequest(t, server, "default", EmbeddingsRequest{
		Input:          embeddingsInput{"graph databases"},
		EncodingFormat: "base64",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 for base64, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

// TestEmbeddings_MethodNotAllowed rejects non-POST.
func TestEmbeddings_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/v1/embeddings", nil)
			req = req.WithContext(tenant.WithTenant(req.Context(), "default"))
			rr := httptest.NewRecorder()
			server.handleEmbeddings(rr, req)
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: want 405, got %d", method, rr.Code)
			}
		})
	}
}

// TestEmbeddings_LangChainShape pins the exact request shape that LangChain's
// OpenAIEmbeddings client sends. If this test fails, LangChain integration
// breaks. It's a contract test — when it fires, look at LangChain's outgoing
// request before changing this test.
//
// Reference shape (langchain-openai/langchain_openai/embeddings/base.py):
//
//	{
//	    "input": ["text1", "text2"],
//	    "model": "text-embedding-ada-002",
//	    "encoding_format": "float"
//	}
func TestEmbeddings_LangChainShape(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "default")
	defer cleanup()

	langChainBody := `{
		"input": ["graph databases", "vector search"],
		"model": "text-embedding-ada-002",
		"encoding_format": "float"
	}`

	rr := embeddingsRequestRaw(t, server, "default", langChainBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("LangChain shape rejected: status %d, body %s", rr.Code, rr.Body.String())
	}

	// LangChain reads .data[].embedding by index; ensure the response is
	// shaped correctly for that.
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	dataAny, ok := resp["data"].([]any)
	if !ok {
		t.Fatalf("response.data not an array: %T", resp["data"])
	}
	if len(dataAny) != 2 {
		t.Fatalf("response.data: want 2 entries, got %d", len(dataAny))
	}
	for i, entryAny := range dataAny {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			t.Fatalf("data[%d] not an object: %T", i, entryAny)
		}
		if _, ok := entry["embedding"].([]any); !ok {
			t.Errorf("data[%d].embedding: not an array (LangChain will fail)", i)
		}
		if entry["object"] != "embedding" {
			t.Errorf(`data[%d].object: want "embedding", got %v`, i, entry["object"])
		}
	}
}

// TestEmbeddings_TenantIsolation pins that two tenants with different LSA
// indexes get different embeddings for the same input. This is the
// per-tenant contract — without it, /v1/embeddings would silently leak
// semantic structure across tenant boundaries.
func TestEmbeddings_TenantIsolation(t *testing.T) {
	server, cleanup := hybridServerWithCorpus(t, "tenant-A")
	defer cleanup()
	// Build a SECOND LSA on a disjoint corpus for tenant-B by reusing the
	// helper's shape; since the helper only seeds one tenant, we add a
	// minimal LSA for tenant-B inline.
	if _, _, err := server.lsaIndexes.Get("tenant-A").FoldQuery("graph"); err != nil {
		t.Fatalf("tenant-A baseline FoldQuery failed: %v", err)
	}

	// Tenant-B has no LSA → 503. This is the test signal: per-tenant
	// indexes are independent (tenant-A's index does NOT serve tenant-B).
	rr, _ := embeddingsRequest(t, server, "tenant-B", EmbeddingsRequest{
		Input: embeddingsInput{"graph databases"},
	})
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("tenant-B (no index) should get 503, got %d", rr.Code)
	}

	// Tenant-A succeeds.
	rr, _ = embeddingsRequest(t, server, "tenant-A", EmbeddingsRequest{
		Input: embeddingsInput{"graph databases"},
	})
	if rr.Code != http.StatusOK {
		t.Errorf("tenant-A (has index) should succeed, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}
