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

	edge, err := s.graph.CreateEdge(req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
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
	edge, err := s.graph.GetEdge(edgeID)
	if err != nil {
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

		edge, err := s.graph.CreateEdge(edgeReq.FromNodeID, edgeReq.ToNodeID, edgeReq.Type, props, edgeReq.Weight)
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
