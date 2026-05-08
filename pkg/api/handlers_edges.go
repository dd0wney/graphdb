package api

import (
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Post(func() { s.createEdge(w, r) }).
		NotAllowed()
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

	// Audit A6a: tenant-scoped create. NOTE: storage.verifyNodeExists
	// is currently tenant-blind, so a caller can reference from/to
	// node IDs owned by another tenant; the resulting edge is stamped
	// with this caller's tenant. This is a known gap tracked as an
	// A6a follow-up — A3b closed cross-tenant *reads* at the storage
	// layer but the create-side existence check still needs scoping.
	tenantID := getTenantFromContext(r)
	edge, err := s.graph.CreateEdgeWithTenant(tenantID, req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create edge"))
		return
	}

	response := s.edgeToResponse(edge)
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

	response := s.edgeToResponse(edge)
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

		edges = append(edges, s.edgeToResponse(edge))
	}

	response := BatchEdgeResponse{
		Edges:   edges,
		Created: len(edges),
		Time:    time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusCreated, response)
}
