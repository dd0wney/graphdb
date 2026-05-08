package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
)

func (s *Server) handleTraversal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req TraversalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate StartNodeID
	if req.StartNodeID == 0 {
		s.respondError(w, http.StatusBadRequest, "StartNodeID is required and must be positive")
		return
	}

	// Validate and normalize MaxDepth
	if req.MaxDepth < MinTraversalDepth {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("MaxDepth must be >= %d", MinTraversalDepth))
		return
	}
	if req.MaxDepth > MaxTraversalDepth {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("MaxDepth must be <= %d", MaxTraversalDepth))
		return
	}
	if req.MaxDepth == 0 {
		req.MaxDepth = DefaultTraversalDepth
	}

	// Audit A6b: scope start-node check to caller's tenant. A
	// cross-tenant or missing start-node both surface as 404 — same
	// existence-leak guard as A6a's getNode.
	tenantID := getTenantFromContext(r)
	if _, err := s.graph.GetNodeForTenant(req.StartNodeID, tenantID); err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Start node %d not found", req.StartNodeID))
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), DefaultAlgorithmTimeout)
	defer cancel()

	start := time.Now()

	// Simple BFS traversal with context, scoped to caller's tenant.
	visited := make(map[uint64]bool)
	nodes := make([]*NodeResponse, 0)
	if err := s.traverseFromWithContext(ctx, tenantID, req.StartNodeID, 0, req.MaxDepth, visited, &nodes); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			s.respondError(w, http.StatusRequestTimeout, "Traversal timed out")
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "graph traversal"))
		return
	}

	response := TraversalResponse{
		Nodes: nodes,
		Count: len(nodes),
		Time:  time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

// traverseFromWithContext performs BFS traversal with context
// cancellation support, scoped to the given tenant.
//
// Audit A6b: dual-filter — both edges (GetOutgoingEdgesForTenant) and
// nodes (GetNodeForTenant) are scoped. The node filter closes the
// residual gap from the A6a follow-up: even if a foreign-tenant node
// is reachable through a tenant-stamped edge (because verifyNodeExists
// is currently tenant-blind), the per-visit GetNodeForTenant drops it
// from the result set.
//
// Perf note: GetNodeForTenant takes a per-visit shard rlock, so this
// path acquires roughly 2× the locks of a pre-A6b traversal. Bounded
// by MaxTraversalDepth (default 10) so it's a non-issue for the
// /traverse endpoint, but do *not* "optimize" by dropping the node
// filter — that reopens the A6a follow-up gap.
func (s *Server) traverseFromWithContext(ctx context.Context, tenantID string, nodeID uint64, depth int, maxDepth int, visited map[uint64]bool, nodes *[]*NodeResponse) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if depth > maxDepth || visited[nodeID] {
		return nil
	}

	visited[nodeID] = true

	node, err := s.graph.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return nil // Skip missing or cross-tenant nodes (no leak).
	}

	*nodes = append(*nodes, s.nodeToResponse(node))

	edges, err := s.graph.GetOutgoingEdgesForTenant(nodeID, tenantID)
	if err != nil {
		return nil // Skip on edge retrieval error
	}

	for _, edge := range edges {
		if err := s.traverseFromWithContext(ctx, tenantID, edge.ToNodeID, depth+1, maxDepth, visited, nodes); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) handleShortestPath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req ShortestPathRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate node IDs
	if req.StartNodeID == 0 {
		s.respondError(w, http.StatusBadRequest, "StartNodeID is required and must be positive")
		return
	}
	if req.EndNodeID == 0 {
		s.respondError(w, http.StatusBadRequest, "EndNodeID is required and must be positive")
		return
	}

	// Audit A6b: both endpoints must be visible to the caller's
	// tenant. Cross-tenant or missing → 404 (same existence-leak
	// guard as A6a). Easy to forget the second of a pair: see the
	// equivalent A6a TestUpdateNode regression for why.
	tenantID := getTenantFromContext(r)
	if _, err := s.graph.GetNodeForTenant(req.StartNodeID, tenantID); err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Start node %d not found", req.StartNodeID))
		return
	}
	if _, err := s.graph.GetNodeForTenant(req.EndNodeID, tenantID); err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("End node %d not found", req.EndNodeID))
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), DefaultAlgorithmTimeout)
	defer cancel()

	// Check for context cancellation before expensive operation
	select {
	case <-ctx.Done():
		s.respondError(w, http.StatusRequestTimeout, "Request timed out")
		return
	default:
	}

	start := time.Now()

	// Audit A6b: tenant-scoped traversal. The algorithm filters at
	// edge expansion (cannot post-filter the path — the BFS may
	// otherwise pick a shorter cross-tenant route and rejecting it
	// after the fact would deny a path that *does* exist within the
	// caller's subgraph).
	path, err := algorithms.ShortestPathForTenant(s.graph, req.StartNodeID, req.EndNodeID, tenantID)
	if err != nil {
		// Log the error but still return a valid response indicating no path found
		log.Printf("ShortestPath algorithm error: %v", err)
	}

	response := ShortestPathResponse{
		Path:   path,
		Length: len(path),
		Found:  err == nil && len(path) > 0,
		Time:   time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}
