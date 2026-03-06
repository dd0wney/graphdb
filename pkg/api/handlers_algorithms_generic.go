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

	case "edge_betweenness":
		var err error
		results, err = s.executeEdgeBetweenness(ctx)
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

	case "triangles":
		var err error
		results, err = s.executeTriangles(ctx)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

	case "scc":
		var err error
		results, err = s.executeSCC(ctx)
		if err != nil {
			s.respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

	case "node_similarity":
		var err error
		results, err = s.executeNodeSimilarity(ctx, req.Parameters)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}

	case "link_prediction":
		var err error
		results, err = s.executeLinkPrediction(ctx, req.Parameters)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}

	case "khop":
		var err error
		results, err = s.executeKHop(ctx, req.Parameters)
		if err != nil {
			s.respondError(w, http.StatusBadRequest, err.Error())
			return
		}

	default:
		s.respondError(w, http.StatusBadRequest, "Unknown algorithm (supported: pagerank, betweenness, edge_betweenness, detect_cycles, has_cycle, triangles, scc, node_similarity, link_prediction, khop)")
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

// executeEdgeBetweenness runs the edge betweenness centrality algorithm
func (s *Server) executeEdgeBetweenness(ctx context.Context) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	result, err := algorithms.EdgeBetweennessCentrality(s.graph)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "edge betweenness centrality"))
	}

	// Convert ByNodePair to JSON-friendly string keys
	byNodePair := make(map[string]float64, len(result.ByNodePair))
	for pair, score := range result.ByNodePair {
		key := fmt.Sprintf("%d->%d", pair[0], pair[1])
		byNodePair[key] = score
	}

	return map[string]any{
		"by_edge_id":   result.ByEdgeID,
		"by_node_pair": byNodePair,
		"top_edges":    result.TopEdges,
	}, nil
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

// executeTriangles counts triangles in the graph
func (s *Server) executeTriangles(ctx context.Context) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	result, err := algorithms.CountTriangles(s.graph)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "triangle counting"))
	}
	return map[string]any{
		"per_node":                result.PerNode,
		"global_count":            result.GlobalCount,
		"clustering_coefficients": result.ClusteringCoefficients,
	}, nil
}

// executeSCC finds strongly connected components
func (s *Server) executeSCC(ctx context.Context) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	result, err := algorithms.StronglyConnectedComponents(s.graph)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "SCC"))
	}

	var largestSize int
	if result.LargestSCC != nil {
		largestSize = result.LargestSCC.Size
	}

	return map[string]any{
		"communities":     result.Communities,
		"node_community":  result.NodeCommunity,
		"largest_scc":     largestSize,
		"singleton_count": result.SingletonCount,
	}, nil
}

// executeNodeSimilarity computes pairwise or per-node similarity
func (s *Server) executeNodeSimilarity(ctx context.Context, params map[string]any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	opts := algorithms.DefaultNodeSimilarityOptions()

	if v, ok := params["metric"]; ok {
		if s, ok := v.(string); ok {
			switch s {
			case "jaccard":
				opts.Metric = algorithms.SimilarityJaccard
			case "overlap":
				opts.Metric = algorithms.SimilarityOverlap
			case "cosine":
				opts.Metric = algorithms.SimilarityCosine
			default:
				return nil, fmt.Errorf("unknown metric %q (supported: jaccard, overlap, cosine)", s)
			}
		}
	}

	opts.Direction = parseDirection(params)

	if v, ok := params["top_k"]; ok {
		if k, ok := v.(float64); ok {
			opts.TopK = int(k)
		}
	}

	// Pair mode: both node_a and node_b specified
	nodeA, hasA := params["node_a"]
	nodeB, hasB := params["node_b"]
	if hasA && hasB {
		a, ok1 := nodeA.(float64)
		b, ok2 := nodeB.(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("node_a and node_b must be numeric IDs")
		}
		score, err := algorithms.NodeSimilarityPair(s.graph, uint64(a), uint64(b), opts)
		if err != nil {
			return nil, fmt.Errorf("%s", sanitizeError(err, "node similarity"))
		}
		return map[string]any{"score": score}, nil
	}

	// Single source mode
	if hasA {
		a, ok := nodeA.(float64)
		if !ok {
			return nil, fmt.Errorf("node_a must be a numeric ID")
		}
		result, err := algorithms.NodeSimilarityFor(s.graph, uint64(a), opts)
		if err != nil {
			return nil, fmt.Errorf("%s", sanitizeError(err, "node similarity"))
		}
		return map[string]any{
			"source_node_id": result.SourceNodeID,
			"similar":        result.Similar,
		}, nil
	}

	// All-pairs mode
	results, err := algorithms.NodeSimilarityAll(s.graph, opts)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "node similarity"))
	}
	return map[string]any{"results": results}, nil
}

// executeLinkPrediction predicts missing links
func (s *Server) executeLinkPrediction(ctx context.Context, params map[string]any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	opts := algorithms.DefaultLinkPredictionOptions()

	if v, ok := params["method"]; ok {
		if m, ok := v.(string); ok {
			switch m {
			case "common_neighbours":
				opts.Method = algorithms.LinkPredCommonNeighbours
			case "adamic_adar":
				opts.Method = algorithms.LinkPredAdamicAdar
			case "preferential_attachment":
				opts.Method = algorithms.LinkPredPreferentialAttachment
			default:
				return nil, fmt.Errorf("unknown method %q (supported: common_neighbours, adamic_adar, preferential_attachment)", m)
			}
		}
	}

	opts.Direction = parseDirection(params)

	if v, ok := params["exclude_existing"]; ok {
		if b, ok := v.(bool); ok {
			opts.ExcludeExisting = b
		}
	}
	if v, ok := params["top_k"]; ok {
		if k, ok := v.(float64); ok {
			opts.TopK = int(k)
		}
	}

	// Pair mode
	fromNode, hasFrom := params["from_node"]
	toNode, hasTo := params["to_node"]
	if hasFrom && hasTo {
		f, ok1 := fromNode.(float64)
		t, ok2 := toNode.(float64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("from_node and to_node must be numeric IDs")
		}
		score, err := algorithms.PredictLinkScore(s.graph, uint64(f), uint64(t), opts)
		if err != nil {
			return nil, fmt.Errorf("%s", sanitizeError(err, "link prediction"))
		}
		return map[string]any{"score": score}, nil
	}

	// Source mode
	if hasFrom {
		f, ok := fromNode.(float64)
		if !ok {
			return nil, fmt.Errorf("from_node must be a numeric ID")
		}
		result, err := algorithms.PredictLinksFor(s.graph, uint64(f), opts)
		if err != nil {
			return nil, fmt.Errorf("%s", sanitizeError(err, "link prediction"))
		}
		return map[string]any{
			"source_node_id": result.SourceNodeID,
			"predictions":    result.Predictions,
		}, nil
	}

	return nil, fmt.Errorf("from_node parameter is required")
}

// executeKHop performs k-hop neighbourhood traversal
func (s *Server) executeKHop(ctx context.Context, params map[string]any) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request timed out")
	default:
	}

	opts := algorithms.DefaultKHopOptions()

	if v, ok := params["max_hops"]; ok {
		if h, ok := v.(float64); ok {
			opts.MaxHops = int(h)
		}
	}

	opts.Direction = parseDirection(params)

	if v, ok := params["max_results"]; ok {
		if m, ok := v.(float64); ok {
			opts.MaxResults = int(m)
		}
	}

	sourceNode, ok := params["source_node"]
	if !ok {
		return nil, fmt.Errorf("source_node parameter is required")
	}
	src, ok := sourceNode.(float64)
	if !ok {
		return nil, fmt.Errorf("source_node must be a numeric ID")
	}

	result, err := algorithms.KHopNeighbours(s.graph, uint64(src), opts)
	if err != nil {
		return nil, fmt.Errorf("%s", sanitizeError(err, "k-hop neighbours"))
	}
	return map[string]any{
		"source_node_id":  result.SourceNodeID,
		"by_hop":          result.ByHop,
		"distances":       result.Distances,
		"total_reachable": result.TotalReachable,
	}, nil
}

// parseDirection extracts a NeighborDirection from request parameters.
func parseDirection(params map[string]any) algorithms.NeighborDirection {
	if v, ok := params["direction"]; ok {
		if d, ok := v.(string); ok {
			switch d {
			case "in":
				return algorithms.DirectionIn
			case "both":
				return algorithms.DirectionBoth
			}
		}
	}
	return algorithms.DirectionOut
}
