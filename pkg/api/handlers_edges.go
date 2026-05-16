package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listEdges(w, r) }).
		Post(func() { s.createEdge(w, r) }).
		NotAllowed()
}

// listEdges returns tenant-scoped edges. Mirrors handlers_nodes.go::listNodes
// in shape: tenant resolution via context, optional typed-primitive routing
// via the ?type= query parameter (empty value treated as absent), no
// existence-leak across tenants (the tenant primitive enforces isolation).
//
// Same audit Security CRIT #2 (2026-05-06) framing as listNodes — before this
// handler existed at all, GET /edges returned 405; the implementation here
// must route through the tenant-strict primitive, never GetAllEdges.
func (s *Server) listEdges(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantFromContext(r)

	var allEdges []*storage.Edge
	if edgeType := r.URL.Query().Get("type"); edgeType != "" {
		allEdges = s.graph.GetEdgesByTypeForTenant(tenantID, edgeType)
	} else {
		allEdges = s.graph.GetAllEdgesForTenant(tenantID)
	}

	edges := make([]*EdgeResponse, 0, len(allEdges))
	for _, edge := range allEdges {
		edges = append(edges, s.edgeToResponse(r.Context(), edge))
	}

	s.respondJSON(w, http.StatusOK, edges)
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
