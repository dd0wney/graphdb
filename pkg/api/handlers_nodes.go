package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/validation"
)

// claimLabel + claimUniquePropertyKey mirror pkg/graphql/mutations_resolvers.go's
// B-lite uniqueness rule for REST callers. Both sites enforce: at most one
// :Claim per (tenant, for_task). Duplicated intentionally so REST POST /nodes
// can't silently bypass the check; both sites retire together when the
// configurable uniqueness-rules registry (COORD_DEPLOY_SPIKE option B-full)
// lands.
const (
	claimLabel             = "Claim"
	claimUniquePropertyKey = "for_task"
)

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listNodes(w, r) }).
		Head(func() { s.countNodes(w, r) }).
		Post(func() { s.createNode(w, r) }).
		Delete(func() { s.deleteAllNodes(w, r) }).
		NotAllowed()
}

// countNodes responds to HEAD /v1/nodes with the X-Total-Count header
// holding the count of caller-tenant nodes (optionally filtered by
// ?label=). No response body — RFC 9110 §9.3.2 contract.
//
// The unfiltered path uses the O(1) CountNodesForTenant primitive
// (maintained as a counter via increment/decrement on create/delete); the
// filtered path uses CountNodesByLabelForTenant, which reads len(index)
// directly rather than cloning the whole label bucket to take its length
// (audit M1). Neither serializes a response body.
func (s *Server) countNodes(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantFromContext(r)
	var count uint64
	if label := r.URL.Query().Get("label"); label != "" {
		count = uint64(s.graph.CountNodesByLabelForTenant(tenantID, label))
	} else {
		count = s.graph.CountNodesForTenant(tenantID)
	}
	w.Header().Set("X-Total-Count", strconv.FormatUint(count, 10))
	w.WriteHeader(http.StatusOK)
}

// deleteAllNodes removes all nodes and edges for the CALLER's tenant only.
// Used by single-tenant consumers (e.g. wiki-graph) before a full reload.
// Tenant-scoped (audit/ROADMAP B1): the previous global DeleteAllNodes let any
// authenticated tenant wipe every tenant's data.
func (s *Server) deleteAllNodes(w http.ResponseWriter, r *http.Request) {
	if err := s.graph.DeleteAllNodesForTenant(getTenantFromContext(r)); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete all nodes"))
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	// Tenant-scoped per audit Security CRIT #2 (2026-05-06). Previously this
	// called GetAllNodes which dumped every tenant's nodes — any
	// authenticated user could read the full multi-tenant corpus in a single
	// request. The endpoint is registered with requireAuth (server.go:42)
	// but not yet withTenant (audit task A5); getTenantFromContext returns
	// the default tenant when no context is set, which matches existing
	// single-tenant deployment behaviour.
	tenantID := getTenantFromContext(r)

	// Parse pagination first so a malformed ?cursor= / ?limit= surfaces as
	// 400 before we materialize the full tenant list.
	page, status, msg := parsePageRequest(r)
	if status != 0 {
		s.respondError(w, status, msg)
		return
	}

	// Route through the index-level page methods so only the requested page
	// is cloned, not the whole tenant set. Empty ?label= is treated as absent
	// (same as omitting the parameter) — a typo like `?label=` returns zero
	// results rather than the full unfiltered corpus.
	var pageItems []*storage.Node
	var next uint64
	if label := r.URL.Query().Get("label"); label != "" {
		pageItems, next = s.graph.NodesByLabelPageForTenant(tenantID, label, page.cursor, page.limit)
	} else {
		pageItems, next = s.graph.NodesPageForTenant(tenantID, page.cursor, page.limit)
	}
	writeNextCursor(w, next)

	nodes := make([]*NodeResponse, 0, len(pageItems))
	for _, node := range pageItems {
		nodes = append(nodes, s.nodeToResponse(r.Context(), node))
	}

	s.respondJSON(w, http.StatusOK, nodes)
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var req NodeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req).ValidateNode(&req)
	if decoder.RespondError() {
		return
	}

	// Convert and sanitize properties
	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	// Audit A6a: create with the request's tenant context. Without this,
	// every create lands in the default tenant regardless of the caller's
	// JWT claim — re-introducing the cross-tenant write the audit closed
	// at the storage layer.
	tenantID := getTenantFromContext(r)

	// H4.4: B-lite mirror. Route single-label :Claim creation through the
	// unique-property helper so REST callers can't bypass the at-most-one-
	// active-Claim-per-(tenant, for_task) rule that the GraphQL resolver
	// enforces. Single-label labels==[claimLabel] is the same gate the
	// resolver uses (pkg/graphql/mutations_resolvers.go:78) — multi-label
	// nodes retain freedom to add secondary labels without inheriting
	// uniqueness semantics.
	var (
		node *storage.Node
		err  error
	)
	if len(req.Labels) == 1 && req.Labels[0] == claimLabel {
		if _, ok := props[claimUniquePropertyKey]; !ok {
			s.respondError(w, http.StatusBadRequest,
				":Claim creation requires a "+claimUniquePropertyKey+" property")
			return
		}
		node, err = s.graph.CreateNodeWithUniquePropertyForTenant(
			tenantID, req.Labels, props, claimLabel, claimUniquePropertyKey,
		)
	} else {
		node, err = s.graph.CreateNodeWithTenant(tenantID, req.Labels, props)
	}
	if err != nil {
		if errors.Is(err, storage.ErrUniqueConstraintViolation) {
			s.respondError(w, http.StatusConflict, err.Error())
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create node"))
		return
	}

	response := s.nodeToResponse(r.Context(), node)
	s.respondJSON(w, http.StatusCreated, response)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	extractor := s.NewPathExtractor(w, r)
	nodeID, ok := extractor.ExtractUint64("/nodes/")
	if !ok {
		return
	}

	s.NewMethodRouter(w, r).
		Get(func() { s.getNode(w, r, nodeID) }).
		Put(func() { s.updateNode(w, r, nodeID) }).
		Delete(func() { s.deleteNode(w, r, nodeID) }).
		NotAllowed()
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	tenantID := getTenantFromContext(r)
	node, err := s.graph.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		// ErrNodeNotFound covers both "doesn't exist" and "exists in
		// another tenant" — the unified error is intentional (no
		// existence-leak side channel). 404 is the right status either way.
		s.respondError(w, http.StatusNotFound, "Node not found")
		return
	}

	response := s.nodeToResponse(r.Context(), node)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	var req NodeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req)
	if decoder.RespondError() {
		return
	}

	// Validate properties if present
	if req.Properties != nil {
		validationReq := validation.NodeRequest{
			Labels:     []string{"Placeholder"}, // Labels not required for update
			Properties: req.Properties,
		}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Convert and sanitize properties
	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	tenantID := getTenantFromContext(r)
	if err := s.graph.UpdateNodeForTenant(nodeID, props, tenantID); err != nil {
		// Cross-tenant update or genuinely-missing node both surface as
		// ErrNodeNotFound — return 404 to avoid an existence-leak side
		// channel. Only true storage errors should 500.
		if errors.Is(err, storage.ErrNodeNotFound) {
			s.respondError(w, http.StatusNotFound, "Node not found")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "update node"))
		return
	}

	node, err := s.graph.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		// Update succeeded but couldn't retrieve the updated node — unusual
		// but not fatal. Don't leak the cause; return success-with-id.
		s.respondJSON(w, http.StatusOK, map[string]any{"updated": nodeID})
		return
	}
	response := s.nodeToResponse(r.Context(), node)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	tenantID := getTenantFromContext(r)
	if err := s.graph.DeleteNodeForTenant(nodeID, tenantID); err != nil {
		// Cross-tenant or missing → 404 (no existence leak).
		if errors.Is(err, storage.ErrNodeNotFound) {
			s.respondError(w, http.StatusNotFound, "Node not found")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete node"))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]any{"deleted": nodeID})
}

func (s *Server) handleBatchNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req BatchNodeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req)
	if decoder.RespondError() {
		return
	}

	// Validate batch size
	if err := validation.ValidateBatchSize(len(req.Nodes)); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	tenantID := getTenantFromContext(r)
	nodes := make([]*NodeResponse, 0, len(req.Nodes))
	converter := newPropertyConverter()

	// #455: track skipped items so callers can tell "dropped" from
	// "never sent" instead of a bare Created count. Index refers to the
	// item's position in req.Nodes (the request), not the response —
	// dropped items never make it into `nodes`, so a response-relative
	// index would be meaningless. This is purely additive: the existing
	// Nodes/Created/Time fields and partial-success behaviour (CC7,
	// consumer_contract_jailgraph_test.go) are untouched.
	var errs []BatchItemError

	for i, nodeReq := range req.Nodes {
		// Validate each node request
		validationReq := validation.NodeRequest{
			Labels:     nodeReq.Labels,
			Properties: nodeReq.Properties,
		}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: err.Error()})
			continue // Skip invalid nodes
		}

		// Convert and sanitize properties
		props := converter.ConvertAndSanitize(nodeReq.Properties, s.convertToValue)

		// Audit A6a: scoped create.
		node, err := s.graph.CreateNodeWithTenant(tenantID, nodeReq.Labels, props)
		if err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: err.Error()})
			continue
		}

		nodes = append(nodes, s.nodeToResponse(r.Context(), node))
	}

	response := BatchNodeResponse{
		Nodes:   nodes,
		Created: len(nodes),
		Time:    time.Since(start).String(),
		Failed:  len(errs),
		Errors:  errs,
	}

	s.respondJSON(w, http.StatusCreated, response)
}
