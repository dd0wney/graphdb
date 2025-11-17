package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Server represents the HTTP API server
type Server struct {
	graph          *storage.GraphStorage
	executor       *query.Executor
	graphqlHandler *graphql.GraphQLHandler
	startTime      time.Time
	version        string
	port           int
}

// NewServer creates a new API server
func NewServer(graph *storage.GraphStorage, port int) *Server {
	// Generate GraphQL schema from storage
	schema, err := graphql.GenerateSchema(graph)
	if err != nil {
		log.Printf("Warning: Failed to generate GraphQL schema: %v", err)
	}

	var graphqlHandler *graphql.GraphQLHandler
	if err == nil {
		graphqlHandler = graphql.NewGraphQLHandler(schema)
	}

	return &Server{
		graph:          graph,
		executor:       query.NewExecutor(graph),
		graphqlHandler: graphqlHandler,
		startTime:      time.Now(),
		version:        "1.0.0",
		port:           port,
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

	// GraphQL endpoint
	mux.HandleFunc("/graphql", s.handleGraphQL)

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
	log.Printf("   GraphQL:      POST %s/graphql", addr)
	log.Printf("   Nodes:        GET/POST %s/nodes", addr)
	log.Printf("   Edges:        GET/POST %s/edges", addr)
	log.Printf("   Traverse:     POST %s/traverse", addr)
	log.Printf("   Shortest Path: POST %s/shortest-path", addr)
	log.Printf("   Algorithms:   POST %s/algorithms", addr)

	// Create HTTP server with timeouts for production security
	server := &http.Server{
		Addr:         addr,
		Handler:      s.loggingMiddleware(s.corsMiddleware(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return server.ListenAndServe()
}

// Handlers

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Get enabled features
	features := make([]string, 0)
	for _, feature := range editions.GetEnabledFeatures() {
		features = append(features, string(feature))
	}

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   s.version,
		Edition:   editions.Current.String(),
		Features:  features,
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

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	// Check if GraphQL handler is initialized
	if s.graphqlHandler == nil {
		s.respondError(w, http.StatusServiceUnavailable, "GraphQL endpoint not available")
		return
	}

	// Delegate to GraphQL handler
	s.graphqlHandler.ServeHTTP(w, r)
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
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    status,
	}
	s.respondJSON(w, status, response)
}
