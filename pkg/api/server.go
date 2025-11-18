package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
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
	authHandler    *auth.AuthHandler
	jwtManager     *auth.JWTManager
	userStore      *auth.UserStore
	startTime      time.Time
	version        string
	port           int
}

// NewServer creates a new API server
func NewServer(graph *storage.GraphStorage, port int) *Server {
	// Generate GraphQL schema with mutations and edges from storage
	schema, err := graphql.GenerateSchemaWithEdges(graph)
	if err != nil {
		log.Printf("Warning: Failed to generate GraphQL schema: %v", err)
	}

	var graphqlHandler *graphql.GraphQLHandler
	if err == nil {
		graphqlHandler = graphql.NewGraphQLHandler(schema)
	}

	// Initialize authentication components
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		// Generate a default secret for development (NOT for production)
		jwtSecret = "default-jwt-secret-key-change-in-production-minimum-32-chars"
		log.Printf("⚠️  WARNING: Using default JWT secret. Set JWT_SECRET environment variable for production!")
	}

	userStore := auth.NewUserStore()
	jwtManager := auth.NewJWTManager(jwtSecret, auth.DefaultTokenDuration, auth.DefaultRefreshTokenDuration)
	authHandler := auth.NewAuthHandler(userStore, jwtManager)

	// Create default admin user if no users exist
	if len(userStore.ListUsers()) == 0 {
		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword == "" {
			adminPassword = "admin123!" // Default password for development
			log.Printf("⚠️  WARNING: Using default admin password. Set ADMIN_PASSWORD environment variable!")
		}

		admin, err := userStore.CreateUser("admin", adminPassword, auth.RoleAdmin)
		if err != nil {
			log.Printf("Warning: Failed to create default admin user: %v", err)
		} else {
			log.Printf("✅ Created default admin user: %s", admin.Username)
		}
	}

	return &Server{
		graph:          graph,
		executor:       query.NewExecutor(graph),
		graphqlHandler: graphqlHandler,
		authHandler:    authHandler,
		jwtManager:     jwtManager,
		userStore:      userStore,
		startTime:      time.Now(),
		version:        "1.0.0",
		port:           port,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health and metrics (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Authentication endpoints (public)
	mux.Handle("/auth/", s.authHandler)

	// Query endpoints (protected)
	mux.HandleFunc("/query", s.requireAuth(s.handleQuery))

	// GraphQL endpoint (protected)
	mux.HandleFunc("/graphql", s.requireAuth(s.handleGraphQL))

	// Node endpoints (protected)
	mux.HandleFunc("/nodes", s.requireAuth(s.handleNodes))
	mux.HandleFunc("/nodes/", s.requireAuth(s.handleNode)) // /nodes/{id}
	mux.HandleFunc("/nodes/batch", s.requireAuth(s.handleBatchNodes))

	// Edge endpoints (protected)
	mux.HandleFunc("/edges", s.requireAuth(s.handleEdges))
	mux.HandleFunc("/edges/", s.requireAuth(s.handleEdge)) // /edges/{id}
	mux.HandleFunc("/edges/batch", s.requireAuth(s.handleBatchEdges))

	// Traversal endpoints (protected)
	mux.HandleFunc("/traverse", s.requireAuth(s.handleTraversal))
	mux.HandleFunc("/shortest-path", s.requireAuth(s.handleShortestPath))

	// Algorithm endpoints (protected)
	mux.HandleFunc("/algorithms", s.requireAuth(s.handleAlgorithm))

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("🚀 Cluso GraphDB API Server starting on %s", addr)
	log.Printf("📖 API Documentation:")
	log.Printf("   Health:        GET  %s/health (public)", addr)
	log.Printf("   Metrics:       GET  %s/metrics (public)", addr)
	log.Printf("   Login:         POST %s/auth/login (public)", addr)
	log.Printf("   Register:      POST %s/auth/register (requires admin)", addr)
	log.Printf("   Refresh:       POST %s/auth/refresh (public)", addr)
	log.Printf("   Current User:  GET  %s/auth/me (requires auth)", addr)
	log.Printf("   Query:         POST %s/query (requires auth)", addr)
	log.Printf("   GraphQL:       POST %s/graphql (requires auth)", addr)
	log.Printf("   Nodes:         GET/POST %s/nodes (requires auth)", addr)
	log.Printf("   Edges:         GET/POST %s/edges (requires auth)", addr)
	log.Printf("   Traverse:      POST %s/traverse (requires auth)", addr)
	log.Printf("   Shortest Path: POST %s/shortest-path (requires auth)", addr)
	log.Printf("   Algorithms:    POST %s/algorithms (requires auth)", addr)

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

	// Sanitize query to prevent injection attacks
	sanitizedQuery, err := query.SanitizeQuery(req.Query)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid query: %v", err))
		return
	}

	start := time.Now()

	// Parse query
	lexer := query.NewLexer(sanitizedQuery)
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
