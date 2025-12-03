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

	// Verify start node exists
	if _, err := s.graph.GetNode(req.StartNodeID); err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Start node %d not found", req.StartNodeID))
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), DefaultAlgorithmTimeout)
	defer cancel()

	start := time.Now()

	// Simple BFS traversal with context
	visited := make(map[uint64]bool)
	nodes := make([]*NodeResponse, 0)
	if err := s.traverseFromWithContext(ctx, req.StartNodeID, 0, req.MaxDepth, visited, &nodes); err != nil {
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

// traverseFromWithContext performs BFS traversal with context cancellation support
func (s *Server) traverseFromWithContext(ctx context.Context, nodeID uint64, depth int, maxDepth int, visited map[uint64]bool, nodes *[]*NodeResponse) error {
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

	node, err := s.graph.GetNode(nodeID)
	if err != nil {
		return nil // Skip missing nodes
	}

	*nodes = append(*nodes, s.nodeToResponse(node))

	edges, err := s.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return nil // Skip on edge retrieval error
	}

	for _, edge := range edges {
		if err := s.traverseFromWithContext(ctx, edge.ToNodeID, depth+1, maxDepth, visited, nodes); err != nil {
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

	// Verify both nodes exist
	if _, err := s.graph.GetNode(req.StartNodeID); err != nil {
		s.respondError(w, http.StatusNotFound, fmt.Sprintf("Start node %d not found", req.StartNodeID))
		return
	}
	if _, err := s.graph.GetNode(req.EndNodeID); err != nil {
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

	path, err := algorithms.ShortestPath(s.graph, req.StartNodeID, req.EndNodeID)
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
