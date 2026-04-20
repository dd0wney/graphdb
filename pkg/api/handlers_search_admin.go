package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Admin endpoints for populating the per-tenant search indexes used by
// /search (FullTextIndex) and /hybrid-search (LSAIndex). Without these,
// both query endpoints return empty results until someone populates the
// indexes out-of-band.

// SearchIndexRequest configures the FTS index build for the caller's tenant.
type SearchIndexRequest struct {
	Labels     []string `json:"labels"`
	Properties []string `json:"properties"`
}

// SearchIndexResponse reports build stats.
type SearchIndexResponse struct {
	IndexedNodes int   `json:"indexed_nodes"`
	TookMs       int64 `json:"took_ms"`
}

// handleSearchIndex serves POST /search/index — rebuilds the tenant's
// FullTextIndex from nodes matching the given labels + properties.
//
// Rebuild is synchronous and replaces any prior index content for the
// tenant. Safe to call repeatedly; idempotent for a stable corpus.
func (s *Server) handleSearchIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req SearchIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Labels) == 0 {
		s.respondError(w, http.StatusBadRequest, "labels must be a non-empty array")
		return
	}
	if len(req.Properties) == 0 {
		s.respondError(w, http.StatusBadRequest, "properties must be a non-empty array")
		return
	}

	tenantID := getTenantFromContext(r)

	start := time.Now()
	if err := s.searchIndexes.IndexForTenant(tenantID, req.Labels, req.Properties); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "build search index"))
		return
	}

	// Count indexed nodes by re-iterating the tenant-scoped label lookups.
	// IndexForTenant doesn't return a count today; counting here is a
	// fresh lookup, cheap relative to the build itself.
	count := 0
	for _, label := range req.Labels {
		count += len(s.graph.GetNodesByLabelForTenant(tenantID, label))
	}

	s.respondJSON(w, http.StatusOK, SearchIndexResponse{
		IndexedNodes: count,
		TookMs:       time.Since(start).Milliseconds(),
	})
}

// LSAIndexRequest configures an LSA build for the caller's tenant.
type LSAIndexRequest struct {
	Labels         []string `json:"labels"`
	TitleProperty  string   `json:"title_property,omitempty"`
	BodyProperties []string `json:"body_properties"`

	// Optional tuning — omit to use DefaultLSAConfig values.
	Dims       int   `json:"dims,omitempty"`
	MinDocFreq int   `json:"min_doc_freq,omitempty"`
	MaxVocab   int   `json:"max_vocab,omitempty"`
	TitleBoost int   `json:"title_boost,omitempty"`
	Seed       int64 `json:"seed,omitempty"`
}

// LSAIndexResponse reports build stats.
type LSAIndexResponse struct {
	IndexedDocs int   `json:"indexed_docs"`
	Dimensions  int   `json:"dimensions"`
	TookMs      int64 `json:"took_ms"`
}

// handleLSAIndex serves POST /hybrid-search/lsa-index — builds (or
// rebuilds) the LSA semantic index for the caller's tenant and
// registers it on the server's TenantLSAIndexes.
//
// 422 if the corpus is too small for the requested dims (vocab < dims).
// 400 on malformed / empty input.
//
// The build is synchronous. Halko SVD at corpus scale is O(D × T × l)
// so callers should expect the call to take seconds for ~10k-doc
// corpora and minutes beyond that.
func (s *Server) handleLSAIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req LSAIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(req.Labels) == 0 {
		s.respondError(w, http.StatusBadRequest, "labels must be a non-empty array")
		return
	}
	if len(req.BodyProperties) == 0 {
		s.respondError(w, http.StatusBadRequest, "body_properties must be a non-empty array")
		return
	}

	tenantID := getTenantFromContext(r)

	// Gather tenant-scoped nodes under the requested labels.
	var nodes []*storage.Node
	for _, label := range req.Labels {
		nodes = append(nodes, s.graph.GetNodesByLabelForTenant(tenantID, label)...)
	}

	// Build []search.Document from node properties. Skip nodes that
	// have no body content (all body props empty/missing) — they would
	// contribute no vocabulary and just inflate the doc count.
	docs := make([]search.Document, 0, len(nodes))
	for _, n := range nodes {
		title := stringProperty(n, req.TitleProperty)
		var bodyParts []string
		for _, p := range req.BodyProperties {
			if v := stringProperty(n, p); v != "" {
				bodyParts = append(bodyParts, v)
			}
		}
		body := strings.Join(bodyParts, " ")
		if body == "" {
			continue
		}
		docs = append(docs, search.Document{ID: n.ID, Title: title, Body: body})
	}

	cfg := search.DefaultLSAConfig()
	if req.Dims > 0 {
		cfg.Dims = req.Dims
	}
	if req.MinDocFreq > 0 {
		cfg.MinDocFreq = req.MinDocFreq
	}
	if req.MaxVocab > 0 {
		cfg.MaxVocab = req.MaxVocab
	}
	if req.TitleBoost > 0 {
		cfg.TitleBoost = req.TitleBoost
	}
	if req.Seed != 0 {
		cfg.Seed = req.Seed
	}

	start := time.Now()
	idx, err := search.BuildLSAIndex(docs, cfg)
	if err != nil {
		// Distinguish "corpus too small" (user-correctable) from internal.
		if strings.Contains(err.Error(), "vocabulary size") || strings.Contains(err.Error(), "empty document corpus") {
			s.respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "build lsa index"))
		return
	}

	s.lsaIndexes.Set(tenantID, idx)

	s.respondJSON(w, http.StatusOK, LSAIndexResponse{
		IndexedDocs: idx.NumDocs(),
		Dimensions:  idx.Dimensions(),
		TookMs:      time.Since(start).Milliseconds(),
	})
}

// stringProperty returns the string value for property name on node, or
// "" if missing or non-string.
func stringProperty(n *storage.Node, name string) string {
	if name == "" {
		return ""
	}
	val, ok := n.Properties[name]
	if !ok || val.Type != storage.TypeString {
		return ""
	}
	s, err := val.AsString()
	if err != nil {
		return ""
	}
	return s
}
