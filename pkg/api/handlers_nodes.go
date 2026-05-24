package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
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
		NotAllowed()
}

// countNodes responds to HEAD /v1/nodes with the X-Total-Count header
// holding the count of caller-tenant nodes (optionally filtered by
// ?label=). No response body — RFC 9110 §9.3.2 contract.
//
// The unfiltered path uses the O(1) CountNodesForTenant primitive
// (maintained as a counter via increment/decrement on create/delete);
// the filtered path falls back to len(GetNodesByLabelForTenant) since
// no indexed count-by-label primitive exists. Still cheaper than GET +
// counting in the client because the response body is never serialized.
func (s *Server) countNodes(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantFromContext(r)
	var count uint64
	if label := r.URL.Query().Get("label"); label != "" {
		count = uint64(len(s.graph.GetNodesByLabelForTenant(tenantID, label)))
	} else {
		count = s.graph.CountNodesForTenant(tenantID)
	}
	w.Header().Set("X-Total-Count", strconv.FormatUint(count, 10))
	w.WriteHeader(http.StatusOK)
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

	// Optional ?label= filter routes through the typed storage primitive
	// (GetNodesByLabelForTenant) so the indexed lookup is used instead of
	// scanning + post-filtering. Empty value is treated as absent — same
	// as omitting the parameter — so a typo like `?label=` doesn't silently
	// return zero results to the caller.
	var allNodes []*storage.Node
	if label := r.URL.Query().Get("label"); label != "" {
		allNodes = s.graph.GetNodesByLabelForTenant(tenantID, label)
	} else {
		allNodes = s.graph.GetAllNodesForTenant(tenantID)
	}

	pageItems, next := paginateNodes(allNodes, page)
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

	// Three-way create dispatch:
	//
	//  1. Explicit `unique_property` set on the request — generalised B-lite
	//     uniqueness, label gate = the (single) request label. Wins over
	//     the hardcoded :Claim fallback so callers can request uniqueness
	//     for arbitrary labels.
	//  2. No `unique_property`, but labels == [:Claim] — historical
	//     H4.4 hardcoded path: enforces the at-most-one-active-Claim-
	//     per-(tenant, for_task) rule that the GraphQL resolver and
	//     graphdb-coord rely on. Preserved for backwards compatibility.
	//  3. Otherwise — vanilla CreateNodeWithTenant.
	var (
		node *storage.Node
		err  error
	)
	switch {
	case req.UniqueProperty != "":
		if len(req.Labels) != 1 {
			s.respondError(w, http.StatusBadRequest,
				"unique_property requires exactly one label (the uniqueness label)")
			return
		}
		if _, ok := props[req.UniqueProperty]; !ok {
			s.respondError(w, http.StatusBadRequest,
				"unique_property "+req.UniqueProperty+" must be present in properties")
			return
		}
		node, err = s.graph.CreateNodeWithUniquePropertyForTenant(
			tenantID, req.Labels, props, req.Labels[0], req.UniqueProperty,
		)
	case len(req.Labels) == 1 && req.Labels[0] == claimLabel:
		if _, ok := props[claimUniquePropertyKey]; !ok {
			s.respondError(w, http.StatusBadRequest,
				":Claim creation requires a "+claimUniquePropertyKey+" property")
			return
		}
		node, err = s.graph.CreateNodeWithUniquePropertyForTenant(
			tenantID, req.Labels, props, claimLabel, claimUniquePropertyKey,
		)
	default:
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

	for _, nodeReq := range req.Nodes {
		// Validate each node request
		validationReq := validation.NodeRequest{
			Labels:     nodeReq.Labels,
			Properties: nodeReq.Properties,
		}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			continue // Skip invalid nodes
		}

		// Convert and sanitize properties
		props := converter.ConvertAndSanitize(nodeReq.Properties, s.convertToValue)

		// Per-item dispatch matches createNode's three-way switch.
		// Failures (including uniqueness 409s) currently get swallowed
		// here — the response shape only reports successes via
		// `created`. Owner note: a future revision could surface per-
		// item errors via an `errors` array on BatchNodeResponse, but
		// changing that shape is a wire-compat concern that's out of
		// scope for this gap-closure PR.
		var (
			node *storage.Node
			err  error
		)
		switch {
		case nodeReq.UniqueProperty != "":
			if len(nodeReq.Labels) != 1 {
				log.Printf("batch_create skip: unique_property requires exactly one label, got %d", len(nodeReq.Labels))
				continue
			}
			if _, ok := props[nodeReq.UniqueProperty]; !ok {
				log.Printf("batch_create skip: unique_property %q missing from properties", nodeReq.UniqueProperty)
				continue
			}
			node, err = s.graph.CreateNodeWithUniquePropertyForTenant(
				tenantID, nodeReq.Labels, props, nodeReq.Labels[0], nodeReq.UniqueProperty,
			)
		case len(nodeReq.Labels) == 1 && nodeReq.Labels[0] == claimLabel:
			if _, ok := props[claimUniquePropertyKey]; !ok {
				log.Printf("batch_create skip: :Claim missing %s property", claimUniquePropertyKey)
				continue
			}
			node, err = s.graph.CreateNodeWithUniquePropertyForTenant(
				tenantID, nodeReq.Labels, props, claimLabel, claimUniquePropertyKey,
			)
		default:
			node, err = s.graph.CreateNodeWithTenant(tenantID, nodeReq.Labels, props)
		}
		if err != nil {
			// Uniqueness conflicts and other create errors fall through
			// to the batch's partial-success contract; log so the failure
			// isn't completely silent during debugging.
			if errors.Is(err, storage.ErrUniqueConstraintViolation) {
				log.Printf("batch_create skip: unique constraint violation: %v", err)
			} else {
				log.Printf("batch_create skip: %v", err)
			}
			continue
		}

		nodes = append(nodes, s.nodeToResponse(r.Context(), node))
	}

	response := BatchNodeResponse{
		Nodes:   nodes,
		Created: len(nodes),
		Time:    time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusCreated, response)
}
