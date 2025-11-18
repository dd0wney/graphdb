package api

import (
	"encoding/json"
	"fmt"
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

	start := time.Now()

	// Simple BFS traversal
	visited := make(map[uint64]bool)
	nodes := make([]*NodeResponse, 0)
	s.traverseFrom(req.StartNodeID, 0, req.MaxDepth, visited, &nodes)

	response := TraversalResponse{
		Nodes: nodes,
		Count: len(nodes),
		Time:  time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) traverseFrom(nodeID uint64, depth int, maxDepth int, visited map[uint64]bool, nodes *[]*NodeResponse) {
	if depth > maxDepth || visited[nodeID] {
		return
	}

	visited[nodeID] = true

	node, err := s.graph.GetNode(nodeID)
	if err != nil {
		return
	}

	*nodes = append(*nodes, s.nodeToResponse(node))

	edges, err := s.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return
	}

	for _, edge := range edges {
		s.traverseFrom(edge.ToNodeID, depth+1, maxDepth, visited, nodes)
	}
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

	start := time.Now()

	path, err := algorithms.ShortestPath(s.graph, req.StartNodeID, req.EndNodeID)

	response := ShortestPathResponse{
		Path:   path,
		Length: len(path),
		Found:  err == nil && len(path) > 0,
		Time:   time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) handleAlgorithm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req AlgorithmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	start := time.Now()
	var results map[string]interface{}

	switch req.Algorithm {
	case "pagerank":
		iterations := 20
		dampingFactor := 0.85
		if v, ok := req.Parameters["iterations"]; ok {
			if i, ok := v.(float64); ok {
				iterations = int(i)
			}
		}
		if v, ok := req.Parameters["damping_factor"]; ok {
			if d, ok := v.(float64); ok {
				dampingFactor = d
			}
		}

		opts := algorithms.PageRankOptions{
			MaxIterations: iterations,
			DampingFactor: dampingFactor,
			Tolerance:     1e-6,
		}

		pageRankResult, err := algorithms.PageRank(s.graph, opts)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("PageRank error: %v", err))
			return
		}
		results = map[string]interface{}{"scores": pageRankResult.Scores}

	case "betweenness":
		centrality, err := algorithms.BetweennessCentrality(s.graph)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Betweenness error: %v", err))
			return
		}
		results = map[string]interface{}{"centrality": centrality}

	case "detect_cycles":
		// Parse options
		opts := algorithms.CycleDetectionOptions{}
		if v, ok := req.Parameters["min_length"]; ok {
			if i, ok := v.(float64); ok {
				opts.MinCycleLength = int(i)
			}
		}
		if v, ok := req.Parameters["max_length"]; ok {
			if i, ok := v.(float64); ok {
				opts.MaxCycleLength = int(i)
			}
		}

		// Detect cycles
		var cycles []algorithms.Cycle
		var err error
		if opts.MinCycleLength > 0 || opts.MaxCycleLength > 0 {
			cycles, err = algorithms.DetectCyclesWithOptions(s.graph, opts)
		} else {
			cycles, err = algorithms.DetectCycles(s.graph)
		}
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Cycle detection error: %v", err))
			return
		}

		// Compute statistics
		stats := algorithms.AnalyzeCycles(cycles)

		results = map[string]interface{}{
			"cycles": cycles,
			"stats": map[string]interface{}{
				"total_cycles":   stats.TotalCycles,
				"shortest_cycle": stats.ShortestCycle,
				"longest_cycle":  stats.LongestCycle,
				"average_length": stats.AverageLength,
				"self_loops":     stats.SelfLoops,
			},
		}

	case "has_cycle":
		hasCycle, err := algorithms.HasCycle(s.graph)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Cycle check error: %v", err))
			return
		}
		results = map[string]interface{}{"has_cycle": hasCycle}

	default:
		s.respondError(w, http.StatusBadRequest, "Unknown algorithm (supported: pagerank, betweenness, detect_cycles, has_cycle)")
		return
	}

	response := AlgorithmResponse{
		Algorithm: req.Algorithm,
		Results:   results,
		Time:      time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}
