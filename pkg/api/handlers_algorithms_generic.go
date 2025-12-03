package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
)

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

	// Create context with timeout for all algorithms
	ctx, cancel := context.WithTimeout(r.Context(), DefaultAlgorithmTimeout)
	defer cancel()

	start := time.Now()
	var results map[string]any

	switch req.Algorithm {
	case "pagerank":
		var err error
		results, err = s.executePageRank(ctx, req.Parameters)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}

	case "betweenness":
		var err error
		results, err = s.executeBetweenness(ctx)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

	case "detect_cycles":
		var err error
		results, err = s.executeDetectCycles(ctx, req.Parameters)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}

	case "has_cycle":
		var err error
		results, err = s.executeHasCycle(ctx)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

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

// executePageRank runs the PageRank algorithm with validated parameters
func (s *Server) executePageRank(ctx context.Context, params map[string]any) (map[string]any, error) {
	iterations := DefaultPageRankIterations
	dampingFactor := DefaultDampingFactor

	if v, ok := params["iterations"]; ok {
		if i, ok := v.(float64); ok {
			iterations = int(i)
		}
	}
	// Validate iterations
	if iterations < MinPageRankIterations {
		return nil, fmt.Errorf("iterations must be >= %d", MinPageRankIterations)
	}
	if iterations > MaxPageRankIterations {
		return nil, fmt.Errorf("iterations must be <= %d", MaxPageRankIterations)
	}

	if v, ok := params["damping_factor"]; ok {
		if d, ok := v.(float64); ok {
			dampingFactor = d
		}
	}
	// Validate damping factor
	if dampingFactor < MinDampingFactor || dampingFactor > MaxDampingFactor {
		return nil, fmt.Errorf("damping_factor must be between %.1f and %.1f", MinDampingFactor, MaxDampingFactor)
	}

	// Check for context cancellation before expensive operation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	opts := algorithms.PageRankOptions{
		MaxIterations: iterations,
		DampingFactor: dampingFactor,
		Tolerance:     1e-6,
	}

	pageRankResult, err := algorithms.PageRank(s.graph, opts)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "PageRank computation"))
	}
	return map[string]any{"scores": pageRankResult.Scores}, nil
}

// executeBetweenness runs the betweenness centrality algorithm
func (s *Server) executeBetweenness(ctx context.Context) (map[string]any, error) {
	// Check for context cancellation before expensive operation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	centrality, err := algorithms.BetweennessCentrality(s.graph)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "betweenness centrality"))
	}
	return map[string]any{"centrality": centrality}, nil
}

// executeDetectCycles runs cycle detection with validated parameters
func (s *Server) executeDetectCycles(ctx context.Context, params map[string]any) (map[string]any, error) {
	opts := algorithms.CycleDetectionOptions{}
	if v, ok := params["min_length"]; ok {
		if i, ok := v.(float64); ok {
			opts.MinCycleLength = int(i)
		}
	}
	if v, ok := params["max_length"]; ok {
		if i, ok := v.(float64); ok {
			opts.MaxCycleLength = int(i)
		}
	}

	// Validate cycle length parameters
	if opts.MinCycleLength < 0 {
		return nil, fmt.Errorf("min_length must be >= 0")
	}
	if opts.MinCycleLength > MaxCycleLength {
		return nil, fmt.Errorf("min_length must be <= %d", MaxCycleLength)
	}
	if opts.MaxCycleLength < 0 {
		return nil, fmt.Errorf("max_length must be >= 0")
	}
	if opts.MaxCycleLength > MaxCycleLength {
		return nil, fmt.Errorf("max_length must be <= %d", MaxCycleLength)
	}
	if opts.MinCycleLength > 0 && opts.MaxCycleLength > 0 && opts.MinCycleLength > opts.MaxCycleLength {
		return nil, fmt.Errorf("min_length cannot be greater than max_length")
	}

	// Check for context cancellation before expensive operation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
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
		return nil, fmt.Errorf("%s", sanitizeError(err, "cycle detection"))
	}

	// Compute statistics
	stats := algorithms.AnalyzeCycles(cycles)

	return map[string]any{
		"cycles": cycles,
		"stats": map[string]any{
			"total_cycles":   stats.TotalCycles,
			"shortest_cycle": stats.ShortestCycle,
			"longest_cycle":  stats.LongestCycle,
			"average_length": stats.AverageLength,
			"self_loops":     stats.SelfLoops,
		},
	}, nil
}

// executeHasCycle checks if the graph has any cycles
func (s *Server) executeHasCycle(ctx context.Context) (map[string]any, error) {
	// Check for context cancellation before operation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	hasCycle, err := algorithms.HasCycle(s.graph)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "cycle check"))
	}
	return map[string]any{"has_cycle": hasCycle}, nil
}
