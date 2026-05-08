package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
)

// HybridSearchRequest is the JSON body for POST /hybrid-search.
type HybridSearchRequest struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit,omitempty"`
	Offset         int      `json:"offset,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	Alpha          *float64 `json:"alpha,omitempty"` // pointer so {"alpha":0} is distinguishable from unset
	IncludeContent bool     `json:"include_content,omitempty"`
	IncludeNodes   bool     `json:"include_nodes,omitempty"`
}

// HybridSearchResultItem is one ranked hybrid result. FTSRank and LSARank
// expose the per-stage rank so callers can see why a doc scored where it
// did; -1 indicates the stage did not return this doc.
type HybridSearchResultItem struct {
	NodeID  uint64        `json:"node_id"`
	Score   float64       `json:"score"`
	FTSRank int           `json:"fts_rank"`
	LSARank int           `json:"lsa_rank"`
	Snippet string        `json:"snippet,omitempty"`
	Node    *NodeResponse `json:"node,omitempty"`
}

// HybridSearchResponse is the JSON response for POST /hybrid-search.
// Degraded is non-empty when the hybrid path fell back to a single stage;
// values: "no-lsa-index", "query-out-of-vocabulary", "no-fts-match".
type HybridSearchResponse struct {
	Results  []HybridSearchResultItem `json:"results"`
	Count    int                      `json:"count"`
	TookMs   int64                    `json:"took_ms"`
	Degraded string                   `json:"degraded,omitempty"`
}

const (
	hybridDefaultLimit = 20
	hybridMaxLimit     = 100
	hybridDefaultAlpha = 0.5

	// Response header signalling hybrid degradation. Value matches Degraded in body.
	HeaderHybridDegraded = "X-GraphDB-Hybrid-Degraded"
)

// handleHybridSearch serves POST /hybrid-search — RRF-merged full-text +
// LSA semantic search, scoped to the caller's tenant. Degrades
// gracefully (signalled via response header + body field) when the
// tenant has no LSA index or the query is out of LSA vocabulary.
func (s *Server) handleHybridSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req HybridSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		s.respondError(w, http.StatusBadRequest, "query must be non-empty")
		return
	}

	if req.Limit <= 0 {
		req.Limit = hybridDefaultLimit
	}
	if req.Limit > hybridMaxLimit {
		req.Limit = hybridMaxLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	alpha := hybridDefaultAlpha
	if req.Alpha != nil {
		alpha = *req.Alpha
	}
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}

	tenantID := getTenantFromContext(r)
	start := time.Now()

	// Audit F2 #2: RRF merge moved to pkg/search.SearchHybridForTenant
	// so non-handler callers (pkg/retrieval/ for GraphRAG) can compose
	// hybrid search without duplicating the merge logic.
	overFetchK := (req.Limit + req.Offset) * 3
	merged, err := search.SearchHybridForTenant(s.searchIndexes, s.lsaIndexes, tenantID, req.Query, search.HybridSearchOpts{
		OverFetchK: overFetchK,
		Alpha:      alpha,
	})
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "hybrid search"))
		return
	}
	hits := merged.Hits
	degraded := merged.Degraded

	// Hydrate FTS-side NodeResponses up front (the loop below needs
	// them for label filtering and the response body).
	hydrated := make(map[uint64]*NodeResponse, len(hits))
	for _, h := range hits {
		if h.FTSNode != nil {
			hydrated[h.NodeID] = s.nodeToResponse(h.FTSNode)
		}
	}

	// Label post-filter. FTS-stage candidates already carry a hydrated
	// node; LSA-only candidates need an on-demand GetNode. Cost is
	// bounded by overFetchK, which in turn is bounded by 3*(limit+offset).
	// Filter mismatches (and GetNode failures from deleted nodes) are
	// dropped from the result set.
	if len(req.Labels) > 0 {
		filtered := hits[:0]
		for _, h := range hits {
			node := hydrated[h.NodeID]
			if node == nil {
				gnode, gerr := s.graph.GetNode(h.NodeID)
				if gerr != nil || gnode == nil {
					continue
				}
				node = s.nodeToResponse(gnode)
				hydrated[h.NodeID] = node
			}
			if hasAnyLabel(node.Labels, req.Labels) {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}

	// Paginate.
	total := len(hits)
	if req.Offset >= total {
		hits = nil
	} else {
		end := req.Offset + req.Limit
		if end > total {
			end = total
		}
		hits = hits[req.Offset:end]
	}

	// Build response. Per-tenant FTS index is needed here only for
	// snippet extraction (NodeContent is FTS-side state).
	fts := s.searchIndexes.Get(tenantID)
	results := make([]HybridSearchResultItem, 0, len(hits))
	for _, h := range hits {
		item := HybridSearchResultItem{
			NodeID:  h.NodeID,
			Score:   h.Score,
			FTSRank: h.FTSRank,
			LSARank: h.LSARank,
		}
		if req.IncludeContent {
			if content, ok := fts.NodeContent(h.NodeID); ok {
				item.Snippet = truncateRunes(content, searchSnippetRunes)
			}
		}
		if req.IncludeNodes {
			if node := hydrated[h.NodeID]; node != nil {
				item.Node = node
			}
		}
		results = append(results, item)
	}

	if degraded != "" {
		w.Header().Set(HeaderHybridDegraded, degraded)
	}
	elapsed := time.Since(start)
	s.respondJSON(w, http.StatusOK, HybridSearchResponse{
		Results:  results,
		Count:    len(results),
		TookMs:   elapsed.Milliseconds(),
		Degraded: degraded,
	})
}
