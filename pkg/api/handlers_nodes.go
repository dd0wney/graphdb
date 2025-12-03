package api

import (
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listNodes(w, r) }).
		Post(func() { s.createNode(w, r) }).
		NotAllowed()
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	allNodes := s.graph.GetAllNodes()
	nodes := make([]*NodeResponse, 0, len(allNodes))

	for _, node := range allNodes {
		props := make(map[string]any)
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
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req).ValidateNode(&req)
	if decoder.RespondError() {
		return
	}

	// Convert and sanitize properties
	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	node, err := s.graph.CreateNode(req.Labels, props)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create node"))
		return
	}

	response := s.nodeToResponse(node)
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

	if err := s.graph.UpdateNode(nodeID, props); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "update node"))
		return
	}

	node, err := s.graph.GetNode(nodeID)
	if err != nil {
		// Update succeeded but couldn't retrieve the updated node - unusual but not fatal
		s.respondJSON(w, http.StatusOK, map[string]any{"updated": nodeID})
		return
	}
	response := s.nodeToResponse(node)
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request, nodeID uint64) {
	if err := s.graph.DeleteNode(nodeID); err != nil {
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
