package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/search"
)

// SearchRequest is the JSON body shape for POST /search.
type SearchRequest struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit,omitempty"`
	Offset         int      `json:"offset,omitempty"`
	Labels         []string `json:"labels,omitempty"`          // optional post-filter
	IncludeContent bool     `json:"include_content,omitempty"` // include a snippet of matched content
	IncludeNodes   bool     `json:"include_nodes,omitempty"`   // include full node data
}

// SearchResultItem is a single ranked full-text result.
type SearchResultItem struct {
	NodeID  uint64        `json:"node_id"`
	Score   float64       `json:"score"`
	Snippet string        `json:"snippet,omitempty"`
	Node    *NodeResponse `json:"node,omitempty"`
}

// SearchResponse is the JSON response for POST /search.
type SearchResponse struct {
	Results []SearchResultItem `json:"results"`
	Count   int                `json:"count"`
	TookMs  int64              `json:"took_ms"`
}

const (
	searchDefaultLimit = 20
	searchMaxLimit     = 100
	searchSnippetRunes = 160
)

// handleSearch serves POST /search — ranked full-text search over the
// tenant's FullTextIndex. An un-indexed tenant returns zero results
// rather than erroring; admins populate indexes via a separate path.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req SearchRequest
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
		req.Limit = searchDefaultLimit
	}
	if req.Limit > searchMaxLimit {
		req.Limit = searchMaxLimit
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	tenantID := getTenantFromContext(r)
	idx := s.searchIndexes.Get(tenantID)

	start := time.Now()

	// Over-fetch so the label post-filter + offset slice don't leave us
	// short of the caller's limit. 3x is enough for most label filters;
	// a highly-selective filter can still return fewer than Limit, which
	// is documented behavior.
	overFetchK := (req.Limit + req.Offset) * 3
	if overFetchK <= 0 {
		overFetchK = req.Limit + req.Offset
	}

	ranked, err := idx.SearchTopK(req.Query, overFetchK)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "search"))
		return
	}

	// Post-filter by label if requested. SearchTopK already hydrated Node.
	var filtered []search.SearchResult
	if len(req.Labels) > 0 {
		filtered = make([]search.SearchResult, 0, len(ranked))
		for _, r := range ranked {
			if r.Node != nil && hasAnyLabel(r.Node.Labels, req.Labels) {
				filtered = append(filtered, r)
			}
		}
	} else {
		filtered = ranked
	}

	// Paginate.
	total := len(filtered)
	if req.Offset >= total {
		filtered = nil
	} else {
		end := req.Offset + req.Limit
		if end > total {
			end = total
		}
		filtered = filtered[req.Offset:end]
	}

	results := make([]SearchResultItem, 0, len(filtered))
	for _, r := range filtered {
		item := SearchResultItem{
			NodeID: r.NodeID,
			Score:  r.Score,
		}
		if req.IncludeContent {
			if content, ok := idx.NodeContent(r.NodeID); ok {
				item.Snippet = truncateRunes(content, searchSnippetRunes)
			}
		}
		if req.IncludeNodes && r.Node != nil {
			item.Node = s.nodeToResponse(r.Node)
		}
		results = append(results, item)
	}

	elapsed := time.Since(start)
	s.respondJSON(w, http.StatusOK, SearchResponse{
		Results: results,
		Count:   len(results),
		TookMs:  elapsed.Milliseconds(),
	})
}

// truncateRunes returns s shortened to maxLen runes, with "..." appended
// when truncation occurs. Maintains UTF-8 boundary safety.
func truncateRunes(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
