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

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listNodes(w, r)
	case http.MethodPost:
		s.createNode(w, r)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	stats := s.graph.GetStatistics()
	nodes := make([]*NodeResponse, 0)

	for nodeID := uint64(1); nodeID <= stats.NodeCount; nodeID++ {
		node, err := s.graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		props := make(map[string]interface{})
		for k, v := range node.Properties {
			props[k] = v.Data
		}

		nodes = append(nodes, &NodeResponse{
			ID:         node.ID,
			Labels:     node.Labels,
			Properties: props,
		})
	}

	s.respondJSON(w, http.StatusOK, nodes)
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var req NodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	validationReq := validation.NodeRequest{
		Labels:     req.Labels,
		Properties: req.Properties,
	}
	if err := validation.ValidateNodeRequest(&validationReq); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Sanitize properties to prevent XSS
	sanitizedProps := storage.SanitizePropertyMap(req.Properties)

	// Convert properties
	props := make(map[string]storage.Value)
	for k, v := range sanitizedProps {
		props[k] = s.convertToValue(v)
	}

	node, err := s.graph.CreateNode(req.Labels, props)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create node: %v", err))
		return
	}

	response := s.nodeToResponse(node)
	s.respondJSON(w, http.StatusCreated, response)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path
	idStr := r.URL.Path[len("/nodes/"):]
	nodeID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid node ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getNode(w, r, nodeID)
	case http.MethodPut:
		s.updateNode(w, r, nodeID)
	case http.MethodDelete:
		s.deleteNode(w, r, nodeID)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	node, err := s.graph.GetNode(nodeID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "Node not found")
		return
	}

	response := s.nodeToResponse(node)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	var req NodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
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

	// Sanitize properties
	sanitizedProps := storage.SanitizePropertyMap(req.Properties)

	props := make(map[string]storage.Value)
	for k, v := range sanitizedProps {
		props[k] = s.convertToValue(v)
	}

	if err := s.graph.UpdateNode(nodeID, props); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update node: %v", err))
		return
	}

	node, _ := s.graph.GetNode(nodeID)
	response := s.nodeToResponse(node)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	if err := s.graph.DeleteNode(nodeID); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete node: %v", err))
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{"deleted": nodeID})
}

func (s *Server) handleBatchNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req BatchNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate batch size
	if err := validation.ValidateBatchSize(len(req.Nodes)); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	start := time.Now()
	nodes := make([]*NodeResponse, 0, len(req.Nodes))

	for _, nodeReq := range req.Nodes {
		// Validate each node request
		validationReq := validation.NodeRequest{
			Labels:     nodeReq.Labels,
			Properties: nodeReq.Properties,
		}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			continue // Skip invalid nodes
		}

		// Sanitize properties
		sanitizedProps := storage.SanitizePropertyMap(nodeReq.Properties)

		props := make(map[string]storage.Value)
		for k, v := range sanitizedProps {
			props[k] = s.convertToValue(v)
		}

		node, err := s.graph.CreateNode(nodeReq.Labels, props)
		if err != nil {
			continue
		}

		nodes = append(nodes, s.nodeToResponse(node))
	}

	response := BatchNodeResponse{
		Nodes:   nodes,
		Created: len(nodes),
		Time:    time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusCreated, response)
}
