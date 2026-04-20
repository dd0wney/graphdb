package api

import (
	"encoding/json"
	"net/http"
	"sort"
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
	rrfK               = 60 // Reciprocal Rank Fusion constant (Cormack et al. 2009)

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
	fts := s.searchIndexes.Get(tenantID)
	lsa := s.lsaIndexes.Get(tenantID)

	start := time.Now()

	overFetchK := (req.Limit + req.Offset) * 3
	if overFetchK <= 0 {
		overFetchK = rrfK
	}

	ftsResults, err := fts.SearchTopK(req.Query, overFetchK)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "hybrid search fts"))
		return
	}

	// Rank maps (0-based). 1-based in RRF formula below.
	ftsRank := make(map[uint64]int, len(ftsResults))
	for i, r := range ftsResults {
		ftsRank[r.NodeID] = i
	}
	nodeFromFTS := make(map[uint64]*NodeResponse, len(ftsResults))
	for _, r := range ftsResults {
		if r.Node != nil {
			nodeFromFTS[r.NodeID] = s.nodeToResponse(r.Node)
		}
	}

	var lsaResults []search.LSAResult
	var degraded string
	if lsa == nil {
		degraded = "no-lsa-index"
	} else {
		qvec, _, foldErr := lsa.FoldQuery(req.Query)
		if foldErr != nil {
			degraded = "query-out-of-vocabulary"
		} else {
			lsaResults, err = lsa.TopKByVector(qvec, overFetchK)
			if err != nil {
				s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "hybrid search lsa"))
				return
			}
		}
	}

	lsaRank := make(map[uint64]int, len(lsaResults))
	for i, r := range lsaResults {
		lsaRank[r.NodeID] = i
	}

	// RRF merge over union of candidates.
	candidates := make(map[uint64]struct{}, len(ftsRank)+len(lsaRank))
	for id := range ftsRank {
		candidates[id] = struct{}{}
	}
	for id := range lsaRank {
		candidates[id] = struct{}{}
	}

	type merged struct {
		id      uint64
		score   float64
		ftsR    int
		lsaR    int
		ftsNode *NodeResponse
	}
	mergedList := make([]merged, 0, len(candidates))
	for id := range candidates {
		m := merged{id: id, ftsR: -1, lsaR: -1, ftsNode: nodeFromFTS[id]}
		if r, ok := ftsRank[id]; ok {
			m.score += alpha / float64(rrfK+r+1)
			m.ftsR = r
		}
		if r, ok := lsaRank[id]; ok {
			m.score += (1 - alpha) / float64(rrfK+r+1)
			m.lsaR = r
		}
		// At alpha=1.0 (or 0.0), one stage contributes nothing to any
		// candidate's score. Candidates that only appear in the
		// contribution-free stage have score=0 and would otherwise
		// trail the meaningful results as zero-score noise. Drop them.
		if m.score <= 0 {
			continue
		}
		mergedList = append(mergedList, m)
	}

	sort.Slice(mergedList, func(i, j int) bool {
		if mergedList[i].score != mergedList[j].score {
			return mergedList[i].score > mergedList[j].score
		}
		return mergedList[i].id < mergedList[j].id
	})

	// Label post-filter — requires the FTS-hydrated Node. Results that
	// came from LSA only (not in FTS) don't have a node here; if a label
	// filter is requested we drop them rather than fetch (keeps hot path
	// I/O-free; future revision can hydrate on demand).
	if len(req.Labels) > 0 {
		filtered := mergedList[:0]
		for _, m := range mergedList {
			if m.ftsNode == nil {
				continue
			}
			labels := make([]string, len(m.ftsNode.Labels))
			copy(labels, m.ftsNode.Labels)
			if hasAnyLabel(labels, req.Labels) {
				filtered = append(filtered, m)
			}
		}
		mergedList = filtered
	}

	// Paginate.
	total := len(mergedList)
	if req.Offset >= total {
		mergedList = nil
	} else {
		end := req.Offset + req.Limit
		if end > total {
			end = total
		}
		mergedList = mergedList[req.Offset:end]
	}

	// Build response.
	results := make([]HybridSearchResultItem, 0, len(mergedList))
	for _, m := range mergedList {
		item := HybridSearchResultItem{
			NodeID:  m.id,
			Score:   m.score,
			FTSRank: m.ftsR,
			LSARank: m.lsaR,
		}
		if req.IncludeContent {
			if content, ok := fts.NodeContent(m.id); ok {
				item.Snippet = truncateRunes(content, searchSnippetRunes)
			}
		}
		if req.IncludeNodes && m.ftsNode != nil {
			item.Node = m.ftsNode
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
