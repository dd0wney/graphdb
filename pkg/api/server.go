package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/darraghdowney/cluso-graphdb/pkg/algorithms"
	"github.com/darraghdowney/cluso-graphdb/pkg/query"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// Server represents the HTTP API server
type Server struct {
	graph     *storage.GraphStorage
	executor  *query.Executor
	startTime time.Time
	version   string
	port      int
}

// NewServer creates a new API server
func NewServer(graph *storage.GraphStorage, port int) *Server {
	return &Server{
		graph:     graph,
		executor:  query.NewExecutor(graph),
		startTime: time.Now(),
		version:   "1.0.0",
		port:      port,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health and metrics
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Query endpoints
	mux.HandleFunc("/query", s.handleQuery)

	// Node endpoints
	mux.HandleFunc("/nodes", s.handleNodes)
	mux.HandleFunc("/nodes/", s.handleNode) // /nodes/{id}
	mux.HandleFunc("/nodes/batch", s.handleBatchNodes)

	// Edge endpoints
	mux.HandleFunc("/edges", s.handleEdges)
	mux.HandleFunc("/edges/", s.handleEdge) // /edges/{id}
	mux.HandleFunc("/edges/batch", s.handleBatchEdges)

	// Traversal endpoints
	mux.HandleFunc("/traverse", s.handleTraversal)
	mux.HandleFunc("/shortest-path", s.handleShortestPath)

	// Algorithm endpoints
	mux.HandleFunc("/algorithms", s.handleAlgorithm)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🚀 Cluso GraphDB API Server starting on %s", addr)
	log.Printf("📖 API Documentation:")
	log.Printf("   Health:       GET  %s/health", addr)
	log.Printf("   Metrics:      GET  %s/metrics", addr)
	log.Printf("   Query:        POST %s/query", addr)
	log.Printf("   Nodes:        GET/POST %s/nodes", addr)
	log.Printf("   Edges:        GET/POST %s/edges", addr)
	log.Printf("   Traverse:     POST %s/traverse", addr)
	log.Printf("   Shortest Path: POST %s/shortest-path", addr)
	log.Printf("   Algorithms:   POST %s/algorithms", addr)

	return http.ListenAndServe(addr, s.loggingMiddleware(s.corsMiddleware(mux)))
}

// Middleware

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   s.version,
		Uptime:    time.Since(s.startTime).String(),
	}
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	stats := s.graph.GetStatistics()

	response := MetricsResponse{
		NodeCount:    stats.NodeCount,
		EdgeCount:    stats.EdgeCount,
		TotalQueries: stats.TotalQueries,
		AvgQueryTime: stats.AvgQueryTime,
		Uptime:       time.Since(s.startTime).String(),
	}
	s.respondJSON(w, http.StatusOK, response)
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	start := time.Now()

	// Parse query
	lexer := query.NewLexer(req.Query)
	tokens, err := lexer.Tokenize()
	if err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Lexer error: %v", err))
		return
	}

	parser := query.NewParser(tokens)
	parsedQuery, err := parser.Parse()
	if err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Parser error: %v", err))
		return
	}

	// Execute query
	results, err := s.executor.Execute(parsedQuery)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Execution error: %v", err))
		return
	}

	response := QueryResponse{
		Columns: results.Columns,
		Rows:    results.Rows,
		Count:   results.Count,
		Time:    time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

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

	// Convert properties
	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
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

	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
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

	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
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

	start := time.Now()
	nodes := make([]*NodeResponse, 0, len(req.Nodes))

	for _, nodeReq := range req.Nodes {
		props := make(map[string]storage.Value)
		for k, v := range nodeReq.Properties {
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

	start := time.Now()
	edges := make([]*EdgeResponse, 0, len(req.Edges))

	for _, edgeReq := range req.Edges {
		props := make(map[string]storage.Value)
		for k, v := range edgeReq.Properties {
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

	default:
		s.respondError(w, http.StatusBadRequest, "Unknown algorithm (supported: pagerank, betweenness)")
		return
	}

	response := AlgorithmResponse{
		Algorithm: req.Algorithm,
		Results:   results,
		Time:      time.Since(start).String(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

// Helper methods

func (s *Server) nodeToResponse(node *storage.Node) *NodeResponse {
	props := make(map[string]interface{})
	for k, v := range node.Properties {
		props[k] = v.Data
	}

	return &NodeResponse{
		ID:         node.ID,
		Labels:     node.Labels,
		Properties: props,
	}
}

func (s *Server) edgeToResponse(edge *storage.Edge) *EdgeResponse {
	props := make(map[string]interface{})
	for k, v := range edge.Properties {
		props[k] = v.Data
	}

	return &EdgeResponse{
		ID:         edge.ID,
		FromNodeID: edge.FromNodeID,
		ToNodeID:   edge.ToNodeID,
		Type:       edge.Type,
		Properties: props,
		Weight:     edge.Weight,
	}
}

func (s *Server) convertToValue(v interface{}) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case float64:
		// JSON numbers are always float64
		if val == float64(int64(val)) {
			return storage.IntValue(int64(val))
		}
		return storage.FloatValue(val)
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprintf("%v", v))
	}
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    status,
	}
	s.respondJSON(w, status, response)
}
