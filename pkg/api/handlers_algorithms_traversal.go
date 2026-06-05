package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dd0wney/graphdb/pkg/algorithms"
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

	// Direction: which edges to follow. Default "outgoing" preserves the
	// historical behaviour; "incoming"/"both" are now honoured (#331 — they
	// were previously decoded and silently ignored).
	direction := req.Direction
	if direction == "" {
		direction = directionOutgoing
	}
	if direction != directionOutgoing && direction != directionIncoming && direction != directionBoth {
		s.respondError(w, http.StatusBadRequest, "direction must be one of: outgoing, incoming, both")
		return
	}
	// Optional edge-type filter (empty = all types) — also previously ignored.
	var edgeTypes map[string]bool
	if len(req.EdgeTypes) > 0 {
		edgeTypes = make(map[string]bool, len(req.EdgeTypes))
		for _, t := range req.EdgeTypes {
			edgeTypes[t] = true
		}
	}
	opts := traverseOpts{maxDepth: req.MaxDepth, direction: direction, edgeTypes: edgeTypes}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), DefaultAlgorithmTimeout)
	defer cancel()

	start := time.Now()

	// Simple BFS traversal with context, scoped to caller's tenant.
	visited := make(map[uint64]bool)
	nodes := make([]*NodeResponse, 0)
	if err := s.traverseFromWithContext(ctx, tenantID, req.StartNodeID, 0, opts, visited, &nodes); err != nil {
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

// Traversal directions for the /traverse `direction` parameter.
const (
	directionOutgoing = "outgoing"
	directionIncoming = "incoming"
	directionBoth     = "both"
)

// traverseOpts carries the per-request traversal configuration through the
// recursive BFS (#331): how deep, which edge direction(s) to follow, and an
// optional edge-type allowlist (nil/empty = all types).
type traverseOpts struct {
	maxDepth  int
	direction string
	edgeTypes map[string]bool
}

// allowsType reports whether an edge of the given type passes the edge-type
// filter. An empty filter admits every type.
func (o traverseOpts) allowsType(edgeType string) bool {
	return len(o.edgeTypes) == 0 || o.edgeTypes[edgeType]
}

// traverseFromWithContext performs BFS traversal with context
// cancellation support, scoped to the given tenant.
//
// Audit A6b: dual-filter — both edges (GetOutgoingEdgesForTenant /
// GetIncomingEdgesForTenant) and nodes (GetNodeForTenant) are scoped.
//
// As of the A6a follow-up, the node filter is technically belt-and-
// braces against the API surface: CreateEdgeWithTenant is now
// tenant-strict on node verification, so the edge filter alone would
// suffice for edges created through the HTTP API. The node filter is
// retained because (a) it costs almost nothing — BFS already fetches
// each node — and (b) cross-tenant edges can still be created via
// other code paths the API doesn't go through: replication currently
// lands every replicated write in the default tenant regardless of
// original ownership (audit task A8); the LSM-backed storage's
// CreateEdge is also tenant-blind. If any of those graphs end up
// in the in-memory GraphStorage instance the API serves, the node
// filter is the only thing keeping their cross-tenant edges out of
// /traverse results.
func (s *Server) traverseFromWithContext(ctx context.Context, tenantID string, nodeID uint64, depth int, opts traverseOpts, visited map[uint64]bool, nodes *[]*NodeResponse) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if depth > opts.maxDepth || visited[nodeID] {
		return nil
	}

	visited[nodeID] = true

	node, err := s.graph.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return nil // Skip missing or cross-tenant nodes (no leak).
	}

	*nodes = append(*nodes, s.nodeToResponse(ctx, node))

	// Collect neighbours per the requested direction, filtered by edge type.
	// outgoing → edge.ToNodeID; incoming → edge.FromNodeID; both → union.
	var neighbors []uint64
	if opts.direction == directionOutgoing || opts.direction == directionBoth {
		edges, err := s.graph.GetOutgoingEdgesForTenant(nodeID, tenantID)
		if err == nil {
			for _, edge := range edges {
				if opts.allowsType(edge.Type) {
					neighbors = append(neighbors, edge.ToNodeID)
				}
			}
		}
	}
	if opts.direction == directionIncoming || opts.direction == directionBoth {
		edges, err := s.graph.GetIncomingEdgesForTenant(nodeID, tenantID)
		if err == nil {
			for _, edge := range edges {
				if opts.allowsType(edge.Type) {
					neighbors = append(neighbors, edge.FromNodeID)
				}
			}
		}
	}

	for _, nb := range neighbors {
		if err := s.traverseFromWithContext(ctx, tenantID, nb, depth+1, opts, visited, nodes); err != nil {
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
