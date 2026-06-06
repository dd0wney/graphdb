package api

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/validation"
)

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listEdges(w, r) }).
		Head(func() { s.countEdges(w, r) }).
		Post(func() { s.createEdge(w, r) }).
		NotAllowed()
}

// edgeFilter captures the parsed ?from=/?to=/?type= query parameters
// for a list/count edge request. Empty hasFromID/hasToID + empty
// edgeType means "no filter, return all tenant edges."
type edgeFilter struct {
	fromID    uint64
	toID      uint64
	hasFromID bool
	hasToID   bool
	edgeType  string
}

// parseEdgeFilter extracts ?from=/?to=/?type= from the request URL.
// Returns an HTTP status + error message pair when a value is present
// but malformed (non-numeric ID); the caller responds with that
// status. Empty values are treated as absent — see listEdges' docstring
// for the "?from= shouldn't silently return zero" rationale.
func parseEdgeFilter(r *http.Request) (edgeFilter, int, string) {
	q := r.URL.Query()
	f := edgeFilter{edgeType: q.Get("type")}
	if s := q.Get("from"); s != "" {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return edgeFilter{}, http.StatusBadRequest, "from must be a positive integer"
		}
		f.fromID, f.hasFromID = id, true
	}
	if s := q.Get("to"); s != "" {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return edgeFilter{}, http.StatusBadRequest, "to must be a positive integer"
		}
		f.toID, f.hasToID = id, true
	}
	return f, 0, ""
}

// filteredEdgesForTenant resolves the parsed filter against the
// tenant-strict storage primitives, applying the in-memory composition
// filter (from+to intersect; from/to+type filter on top). The dispatch
// picks the most-selective primitive available (from > to > type >
// none). Returns a storage error untouched for the caller to map.
func (s *Server) filteredEdgesForTenant(tenantID string, f edgeFilter) ([]*storage.Edge, error) {
	var (
		allEdges []*storage.Edge
		err      error
	)
	switch {
	case f.hasFromID:
		allEdges, err = s.graph.GetOutgoingEdgesForTenant(f.fromID, tenantID)
	case f.hasToID:
		allEdges, err = s.graph.GetIncomingEdgesForTenant(f.toID, tenantID)
	case f.edgeType != "":
		allEdges = s.graph.GetEdgesByTypeForTenant(tenantID, f.edgeType)
	default:
		allEdges = s.graph.GetAllEdgesForTenant(tenantID)
	}
	if err != nil {
		return nil, err
	}
	// Composition filters on top of the dispatched primitive.
	out := allEdges[:0]
	for _, e := range allEdges {
		if f.hasFromID && f.hasToID && e.ToNodeID != f.toID {
			continue
		}
		if f.edgeType != "" && e.Type != f.edgeType {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// listEdges returns tenant-scoped edges. Mirrors handlers_nodes.go::listNodes
// in shape: tenant resolution via context, optional typed-primitive routing
// via query parameters (empty values treated as absent), no existence-leak
// across tenants (the tenant primitive enforces isolation).
//
// Same audit Security CRIT #2 (2026-05-06) framing as listNodes — must
// route through *ForTenant primitives, never GetAllEdges.
//
// Supported query parameters:
//
//   - ?from=<node_id>     outgoing edges from the given node
//   - ?to=<node_id>       incoming edges to the given node
//   - ?type=<edge_type>   edges with the given type
//
// Combinations:
//
//   - ?from=A&to=B        edges from A to B specifically (the "between" query)
//   - ?from=A&type=T      outgoing edges from A filtered by type T
//   - ?to=B&type=T        incoming edges to B filtered by type T
//   - ?from=A&to=B&type=T all three combined
//
// Dispatch precedence: the most-selective primitive is invoked first
// (from > to > type > none), then remaining parameters become in-memory
// filters on top. Invalid integer values for from/to return 400.
func (s *Server) listEdges(w http.ResponseWriter, r *http.Request) {
	f, status, msg := parseEdgeFilter(r)
	if status != 0 {
		s.respondError(w, status, msg)
		return
	}
	page, status, msg := parsePageRequest(r)
	if status != 0 {
		s.respondError(w, status, msg)
		return
	}
	// ?from=/?to= adjacency cases still need the compose+paginate path because
	// filteredEdgesForTenant applies in-memory cross-parameter filtering (from+to
	// intersect; type filter on top). Index-set-only cases (?type= and unfiltered)
	// use the storage page methods to clone only the requested page.
	tenantID := getTenantFromContext(r)
	var pageItems []*storage.Edge
	var next uint64
	switch {
	case f.hasFromID || f.hasToID:
		allEdges, err := s.filteredEdgesForTenant(tenantID, f)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "list edges"))
			return
		}
		pageItems, next = paginateEdges(allEdges, page)
	case f.edgeType != "":
		pageItems, next = s.graph.EdgesByTypePageForTenant(tenantID, f.edgeType, page.cursor, page.limit)
	default:
		pageItems, next = s.graph.EdgesPageForTenant(tenantID, page.cursor, page.limit)
	}
	writeNextCursor(w, next)
	edges := make([]*EdgeResponse, 0, len(pageItems))
	for _, edge := range pageItems {
		edges = append(edges, s.edgeToResponse(r.Context(), edge))
	}
	s.respondJSON(w, http.StatusOK, edges)
}

// countEdges responds to HEAD /v1/edges with the X-Total-Count header
// holding the count of edges matching the filter (or total tenant edges
// when no filter is set). No response body — RFC 9110 §9.3.2 contract.
//
// The unfiltered path uses the O(1) CountEdgesForTenant counter
// primitive (maintained on create/delete); any filter falls back to
// the filteredEdgesForTenant materialization since no
// indexed-count-by-(label/type/from/to) primitive exists. Still cheaper
// than GET + count-in-client because the JSON body is never serialized.
func (s *Server) countEdges(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantFromContext(r)
	f, status, msg := parseEdgeFilter(r)
	if status != 0 {
		s.respondError(w, status, msg)
		return
	}
	var count uint64
	if !f.hasFromID && !f.hasToID && f.edgeType == "" {
		count = s.graph.CountEdgesForTenant(tenantID)
	} else {
		edges, err := s.filteredEdgesForTenant(tenantID, f)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "count edges"))
			return
		}
		count = uint64(len(edges))
	}
	w.Header().Set("X-Total-Count", strconv.FormatUint(count, 10))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) createEdge(w http.ResponseWriter, r *http.Request) {
	var req EdgeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req).ValidateEdge(&req)
	if decoder.RespondError() {
		return
	}

	// Convert and sanitize properties
	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	// Audit A6a (handler) + A6a follow-up (storage): tenant-scoped
	// create. From/to nodes must belong to the caller's tenant —
	// CreateEdgeWithTenant refuses cross-tenant references with
	// ErrNodeNotFound, surfaced here as 404 (no existence-leak).
	tenantID := getTenantFromContext(r)
	edge, err := s.graph.CreateEdgeWithTenant(tenantID, req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
	if err != nil {
		if errors.Is(err, storage.ErrNodeNotFound) {
			s.respondError(w, http.StatusNotFound, "Source or target node not found")
			return
		}
		if errors.Is(err, storage.ErrInvalidEdgeWeight) {
			s.respondError(w, http.StatusBadRequest, "weight must be a finite number")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create edge"))
		return
	}

	response := s.edgeToResponse(r.Context(), edge)
	s.respondJSON(w, http.StatusCreated, response)
}

func (s *Server) handleEdge(w http.ResponseWriter, r *http.Request) {
	extractor := s.NewPathExtractor(w, r)
	edgeID, ok := extractor.ExtractUint64("/edges/")
	if !ok {
		return
	}

	s.NewMethodRouter(w, r).
		Get(func() { s.getEdge(w, r, edgeID) }).
		Put(func() { s.updateEdge(w, r, edgeID) }).
		Delete(func() { s.deleteEdge(w, r, edgeID) }).
		NotAllowed()
}

func (s *Server) getEdge(w http.ResponseWriter, r *http.Request, edgeID uint64) {
	tenantID := getTenantFromContext(r)
	edge, err := s.graph.GetEdgeForTenant(edgeID, tenantID)
	if err != nil {
		// ErrEdgeNotFound covers both "doesn't exist" and "exists in
		// another tenant" — unified error to avoid existence-leak.
		s.respondError(w, http.StatusNotFound, "Edge not found")
		return
	}

	response := s.edgeToResponse(r.Context(), edge)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) updateEdge(w http.ResponseWriter, r *http.Request, edgeID uint64) {
	var req EdgeUpdateRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req)
	if decoder.RespondError() {
		return
	}

	// Reject non-finite weights — the WAL JSON-encodes the edge and cannot
	// marshal ±Inf/NaN (see #328); surface as a 400 rather than a silently
	// dropped (fail-soft) WAL write.
	if req.Weight != nil && (math.IsInf(*req.Weight, 0) || math.IsNaN(*req.Weight)) {
		s.respondError(w, http.StatusBadRequest, "weight must be a finite number")
		return
	}

	// Convert and sanitize properties (nil when absent — UpdateEdge merges,
	// so an absent properties map leaves existing properties untouched).
	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	// Audit A6b: tenant-scoped update. Cross-tenant or missing edge both
	// surface as ErrEdgeNotFound → 404 (no existence-leak side channel).
	// req.Weight is a pointer: nil leaves the weight unchanged.
	tenantID := getTenantFromContext(r)
	if err := s.graph.UpdateEdgeForTenant(edgeID, props, req.Weight, tenantID); err != nil {
		if errors.Is(err, storage.ErrEdgeNotFound) {
			s.respondError(w, http.StatusNotFound, "Edge not found")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "update edge"))
		return
	}

	edge, err := s.graph.GetEdgeForTenant(edgeID, tenantID)
	if err != nil {
		// Update succeeded but re-fetch failed — return success-with-id.
		s.respondJSON(w, http.StatusOK, map[string]any{"updated": edgeID})
		return
	}
	response := s.edgeToResponse(r.Context(), edge)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) deleteEdge(w http.ResponseWriter, r *http.Request, edgeID uint64) {
	tenantID := getTenantFromContext(r)
	if err := s.graph.DeleteEdgeForTenant(edgeID, tenantID); err != nil {
		// Cross-tenant or missing → 404 (no existence leak).
		if errors.Is(err, storage.ErrEdgeNotFound) {
			s.respondError(w, http.StatusNotFound, "Edge not found")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "delete edge"))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]any{"deleted": edgeID})
}

func (s *Server) handleBatchEdges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req BatchEdgeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req)
	if decoder.RespondError() {
		return
	}

	// Validate batch size
	if err := validation.ValidateBatchSize(len(req.Edges)); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	tenantID := getTenantFromContext(r)
	edges := make([]*EdgeResponse, 0, len(req.Edges))
	converter := newPropertyConverter()

	for _, edgeReq := range req.Edges {
		// Validate each edge request
		var weightPtr *float64
		if edgeReq.Weight != 0 {
			weightPtr = &edgeReq.Weight
		}
		validationReq := validation.EdgeRequest{
			FromNodeID: edgeReq.FromNodeID,
			ToNodeID:   edgeReq.ToNodeID,
			Type:       edgeReq.Type,
			Weight:     weightPtr,
			Properties: edgeReq.Properties,
		}
		if err := validation.ValidateEdgeRequest(&validationReq); err != nil {
			continue // Skip invalid edges
		}

		// Convert and sanitize properties
		props := converter.ConvertAndSanitize(edgeReq.Properties, s.convertToValue)

		// Audit A6a: scoped create.
		edge, err := s.graph.CreateEdgeWithTenant(tenantID, edgeReq.FromNodeID, edgeReq.ToNodeID, edgeReq.Type, props, edgeReq.Weight)
		if err != nil {
			continue
		}

		edges = append(edges, s.edgeToResponse(r.Context(), edge))
	}

	response := BatchEdgeResponse{
		Edges:   edges,
		Created: len(edges),
		Time:    time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusCreated, response)
}
