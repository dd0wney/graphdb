package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listNodes(w, r) }).
		Post(func() { s.createNode(w, r) }).
		NotAllowed()
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
	allNodes := s.graph.GetAllNodesForTenant(tenantID)
	nodes := make([]*NodeResponse, 0, len(allNodes))

	for _, node := range allNodes {
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
	node, err := s.graph.CreateNodeWithTenant(tenantID, req.Labels, props)
	if err != nil {
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

		// Audit A6a: scoped create.
		node, err := s.graph.CreateNodeWithTenant(tenantID, nodeReq.Labels, props)
		if err != nil {
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
