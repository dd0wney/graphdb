package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createEdge(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) createEdge(w http.ResponseWriter, r *http.Request) {
	var req EdgeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	var weightPtr *float64
	if req.Weight != 0 {
		weightPtr = &req.Weight
	}
	validationReq := validation.EdgeRequest{
		FromNodeID: req.FromNodeID,
		ToNodeID:   req.ToNodeID,
		Type:       req.Type,
		Weight:     weightPtr,
		Properties: req.Properties,
	}
	if err := validation.ValidateEdgeRequest(&validationReq); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Sanitize properties
	sanitizedProps := storage.SanitizePropertyMap(req.Properties)

	props := make(map[string]storage.Value)
	for k, v := range sanitizedProps {
		props[k] = s.convertToValue(v)
	}

	edge, err := s.graph.CreateEdge(req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create edge: %v", err))
		return
	}

	response := s.edgeToResponse(edge)
	s.respondJSON(w, http.StatusCreated, response)
}

func (s *Server) handleEdge(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/edges/"):]
	edgeID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid edge ID")
		return
	}

	if r.Method == http.MethodGet {
		s.getEdge(w, r, edgeID)
	} else {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate batch size
	if err := validation.ValidateBatchSize(len(req.Edges)); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	edges := make([]*EdgeResponse, 0, len(req.Edges))

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

		// Sanitize properties
		sanitizedProps := storage.SanitizePropertyMap(edgeReq.Properties)

		props := make(map[string]storage.Value)
		for k, v := range sanitizedProps {
			props[k] = s.convertToValue(v)
		}

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
