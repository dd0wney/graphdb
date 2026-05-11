package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/retrieval"
)

// RetrieveRequest is the JSON body for POST /v1/retrieve. Shape
// matches LangChain's BaseRetriever pattern (per F2 spike #28 §2 Q1):
//
//	{ "query": "...", "k": 10, "max_tokens": 4096, "max_hops": 2,
//	  "alpha": 0.7, "beta": 0.3, "tau": 2.0,
//	  "labels": ["Doc"], "include_node": false }
//
// Pointer-typed coefficients (Alpha, Beta, Tau) distinguish "set to
// zero" from "unset" — caller can pin alpha=0.0 (LSA-only seeds)
// without having that read as "use the default."
type RetrieveRequest struct {
	Query       string   `json:"query"`
	K           int      `json:"k,omitempty"`
	MaxTokens   int      `json:"max_tokens,omitempty"`
	MaxHops     int      `json:"max_hops,omitempty"`
	Alpha       *float64 `json:"alpha,omitempty"`
	Beta        *float64 `json:"beta,omitempty"`
	Tau         *float64 `json:"tau,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	IncludeNode bool     `json:"include_node,omitempty"`
}

// RetrieveDocument is one ranked result, shaped for LangChain.
// PageContent is the chunk text; Metadata carries the graph signal
// (NodeID, Score, Source.Path).
type RetrieveDocument struct {
	PageContent string                   `json:"page_content"`
	Metadata    RetrieveDocumentMetadata `json:"metadata"`
}

// RetrieveDocumentMetadata is the metadata block on each document.
// LangChain treats metadata as opaque; we use it to surface the
// graph-specific signals.
type RetrieveDocumentMetadata struct {
	NodeID uint64         `json:"node_id"`
	Score  float64        `json:"score"`
	Source RetrieveSource `json:"source"`
	Node   *NodeResponse  `json:"node,omitempty"` // populated when IncludeNode=true
}

// RetrieveSource captures the citation: which seed contributed the
// chunk, the node's primary label, and the BFS path. The path is the
// load-bearing graph signal — without it, /v1/retrieve is a fancy
// vector retriever (F2 spike §2 Q6).
type RetrieveSource struct {
	NodeID uint64   `json:"node_id"`
	Label  string   `json:"label,omitempty"`
	Path   []uint64 `json:"path"` // [seed, ..., node_id]; length 1 for seeds
}

// RetrieveResponse is the JSON response for POST /v1/retrieve.
// Degraded forwards the hybrid-search degraded flag
// ("no-lsa-index", "query-out-of-vocabulary").
type RetrieveResponse struct {
	Documents []RetrieveDocument `json:"documents"`
	Degraded  string             `json:"degraded,omitempty"`
	TookMs    int64              `json:"took_ms"`
}

const (
	retrieveDefaultTimeout = 30 * time.Second

	// HeaderRetrieveDegraded matches the hybrid-search degradation
	// header pattern (X-GraphDB-Hybrid-Degraded) so callers can
	// inspect the response without parsing the body.
	HeaderRetrieveDegraded = "X-GraphDB-Retrieve-Degraded"
)

// handleRetrieve serves POST /v1/retrieve — graph-augmented
// retrieval, scoped to the caller's tenant. Composes
// pkg/search.SearchHybridForTenant (seeds) + tenant-scoped traversal
// (expansion) + score combination + token-budget drop.
//
// Audit F2 #4 (2026-05-08). Tenant scoping rests on Track A; the
// audit_regression_test.go suite (A7 #27) catches any cross-tenant
// regression at the contract level.
func (s *Server) handleRetrieve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req RetrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		s.respondError(w, http.StatusBadRequest, "query must be non-empty")
		return
	}

	// Build retrieval.Options. Pointer-typed coefficients let the
	// caller pin to zero ("LSA only", "graph signal only") without
	// having that read as "use the default."
	opts := retrieval.Options{
		K:         req.K,
		MaxTokens: req.MaxTokens,
		MaxHops:   req.MaxHops,
		Labels:    req.Labels,
	}
	if req.Alpha != nil {
		opts.Alpha = *req.Alpha
	}
	if req.Beta != nil {
		opts.Beta = *req.Beta
	}
	if req.Tau != nil {
		opts.Tau = *req.Tau
	}

	tenantID := getTenantFromContext(r)

	ctx, cancel := context.WithTimeout(r.Context(), retrieveDefaultTimeout)
	defer cancel()

	result, err := s.retriever.Retrieve(ctx, req.Query, tenantID, opts)
	if err != nil {
		// Context cancellation surfaces as 408. Other errors are 500;
		// sanitize so we don't leak storage internals.
		if errors.Is(err, context.DeadlineExceeded) {
			s.respondError(w, http.StatusRequestTimeout, "Retrieval timed out")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "retrieval"))
		return
	}

	// Map chunks → LangChain documents. IncludeNode hydrates each
	// node via GetNodeForTenant (same tenant guarantee Retrieve used
	// internally; double check is cheap and protects against drift).
	docs := make([]RetrieveDocument, 0, len(result.Chunks))
	for _, c := range result.Chunks {
		doc := RetrieveDocument{
			PageContent: c.Content,
			Metadata: RetrieveDocumentMetadata{
				NodeID: c.NodeID,
				Score:  c.Score,
				Source: RetrieveSource{
					NodeID: c.NodeID,
					Label:  c.Label,
					Path:   c.SourcePath,
				},
			},
		}
		if req.IncludeNode {
			if node, err := s.graph.GetNodeForTenant(c.NodeID, tenantID); err == nil {
				doc.Metadata.Node = s.nodeToResponse(r.Context(), node)
			}
		}
		docs = append(docs, doc)
	}

	if result.Degraded != "" {
		w.Header().Set(HeaderRetrieveDegraded, result.Degraded)
	}

	s.respondJSON(w, http.StatusOK, RetrieveResponse{
		Documents: docs,
		Degraded:  result.Degraded,
		TookMs:    result.TookMs,
	})
}
