package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server represents the HTTP API server
type Server struct {
	graph            *storage.GraphStorage
	executor         *query.Executor
	graphqlHandler   *graphql.GraphQLHandler
	authHandler      *auth.AuthHandler
	jwtManager       *auth.JWTManager
	userStore        *auth.UserStore
	apiKeyStore      *auth.APIKeyStore
	auditLogger      *audit.AuditLogger
	metricsRegistry  *metrics.Registry
	healthChecker    *health.HealthChecker
	tlsConfig        *tlspkg.Config
	encryptionEngine interface{} // *encryption.Engine
	keyManager       interface{} // *encryption.KeyManager
	startTime        time.Time
	version          string
	port             int
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
	apiKeyStore := auth.NewAPIKeyStore()
	jwtManager := auth.NewJWTManager(jwtSecret, auth.DefaultTokenDuration, auth.DefaultRefreshTokenDuration)
	authHandler := auth.NewAuthHandler(userStore, jwtManager)
	auditLogger := audit.NewAuditLogger(10000) // Store last 10,000 events

	// Initialize metrics and health monitoring
	metricsRegistry := metrics.DefaultRegistry()
	healthChecker := health.NewHealthChecker()

	// Register basic health checks
	healthChecker.RegisterLivenessCheck("api", func() health.Check {
		return health.SimpleCheck("api")
	})

	healthChecker.RegisterReadinessCheck("storage", health.DatabaseCheck(func() error {
		// Check if storage is accessible
		_ = graph.GetStatistics()
		return nil
	}))

	healthChecker.RegisterCheck("memory", health.MemoryCheck(func() (uint64, uint64) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.Alloc, m.Sys
	}))

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
		graph:           graph,
		executor:        query.NewExecutor(graph),
		graphqlHandler:  graphqlHandler,
		authHandler:     authHandler,
		jwtManager:      jwtManager,
		userStore:       userStore,
		apiKeyStore:     apiKeyStore,
		auditLogger:     auditLogger,
		metricsRegistry: metricsRegistry,
		healthChecker:   healthChecker,
		tlsConfig:       nil, // TLS disabled by default
		startTime:       time.Now(),
		version:         "1.0.0",
		port:            port,
	}
}

// SetTLSConfig sets the TLS configuration for the server
func (s *Server) SetTLSConfig(cfg *tlspkg.Config) {
	s.tlsConfig = cfg
}

// SetEncryption sets the encryption engine and key manager for the server
func (s *Server) SetEncryption(engine, keyManager interface{}) {
	s.encryptionEngine = engine
	s.keyManager = keyManager
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health and metrics (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/health/ready", s.healthChecker.ReadinessHandler())
	mux.Handle("/health/live", s.healthChecker.LivenessHandler())
	mux.Handle("/metrics", promhttp.HandlerFor(s.metricsRegistry.GetPrometheusRegistry(), promhttp.HandlerOpts{}))

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

	// Security management endpoints (protected, admin only for some)
	mux.HandleFunc("/api/v1/security/keys/rotate", s.requireAuth(s.handleSecurityKeyRotate))
	mux.HandleFunc("/api/v1/security/keys/info", s.requireAuth(s.handleSecurityKeyInfo))
	mux.HandleFunc("/api/v1/security/audit/logs", s.requireAuth(s.handleSecurityAuditLogs))
	mux.HandleFunc("/api/v1/security/audit/export", s.requireAuth(s.handleSecurityAuditExport))
	mux.HandleFunc("/api/v1/security/health", s.requireAuth(s.handleSecurityHealth))

	addr := fmt.Sprintf(":%d", s.port)

	// Check if TLS is enabled
	protocol := "http"
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		protocol = "https"
	}

	log.Printf("🚀 Cluso GraphDB API Server starting on %s://%s", protocol, addr)
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		log.Printf("🔒 TLS enabled with secure cipher suites (TLS 1.2+)")
	}
	log.Printf("📖 API Documentation:")
	log.Printf("   Health:        GET  %s://%s/health (public)", protocol, addr)
	log.Printf("   Readiness:     GET  %s://%s/health/ready (public)", protocol, addr)
	log.Printf("   Liveness:      GET  %s://%s/health/live (public)", protocol, addr)
	log.Printf("   Metrics:       GET  %s://%s/metrics (public, Prometheus format)", protocol, addr)
	log.Printf("   Login:         POST %s://%s/auth/login (public)", protocol, addr)
	log.Printf("   Register:      POST %s://%s/auth/register (requires admin)", protocol, addr)
	log.Printf("   Refresh:       POST %s://%s/auth/refresh (public)", protocol, addr)
	log.Printf("   Current User:  GET  %s://%s/auth/me (requires auth)", protocol, addr)
	log.Printf("   Query:         POST %s://%s/query (requires auth)", protocol, addr)
	log.Printf("   GraphQL:       POST %s://%s/graphql (requires auth)", protocol, addr)
	log.Printf("   Nodes:         GET/POST %s://%s/nodes (requires auth)", protocol, addr)
	log.Printf("   Edges:         GET/POST %s://%s/edges (requires auth)", protocol, addr)
	log.Printf("   Traverse:      POST %s://%s/traverse (requires auth)", protocol, addr)
	log.Printf("   Shortest Path: POST %s://%s/shortest-path (requires auth)", protocol, addr)
	log.Printf("   Algorithms:    POST %s://%s/algorithms (requires auth)", protocol, addr)
	log.Printf("📋 Security Management:")
	log.Printf("   Key Rotation:  POST %s://%s/api/v1/security/keys/rotate (requires auth)", protocol, addr)
	log.Printf("   Key Info:      GET  %s://%s/api/v1/security/keys/info (requires auth)", protocol, addr)
	log.Printf("   Audit Logs:    GET  %s://%s/api/v1/security/audit/logs (requires auth)", protocol, addr)
	log.Printf("   Audit Export:  POST %s://%s/api/v1/security/audit/export (requires auth)", protocol, addr)
	log.Printf("   Security Health: GET  %s://%s/api/v1/security/health (requires auth)", protocol, addr)

	// Start background metrics updater
	go s.updateMetricsPeriodically()

	// Create HTTP server with timeouts for production security
	// Middleware chain: metrics -> panicRecovery -> securityHeaders -> inputValidation -> audit -> logging -> CORS -> routes
	server := &http.Server{
		Addr:         addr,
		Handler:      s.metricsMiddleware(s.panicRecoveryMiddleware(s.securityHeadersMiddleware(s.inputValidationMiddleware(s.auditMiddleware(s.loggingMiddleware(s.corsMiddleware(mux))))))),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server with or without TLS
	if s.tlsConfig != nil && s.tlsConfig.Enabled {
		// Load TLS configuration
		serverTLSConfig, err := tlspkg.LoadTLSConfig(s.tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to load TLS config: %w", err)
		}
		server.TLSConfig = serverTLSConfig

		// ListenAndServeTLS with empty cert/key since they're in TLSConfig
		return server.ListenAndServeTLS("", "")
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

	// Get health check results
	healthStatus := s.healthChecker.Check()

	// Determine HTTP status code based on health
	httpStatus := http.StatusOK
	if healthStatus.Status == health.StatusUnhealthy {
		httpStatus = http.StatusServiceUnavailable
	}

	// Convert health checks to interface{} map for JSON serialization
	checks := make(map[string]interface{})
	for name, check := range healthStatus.Checks {
		checks[name] = check
	}

	response := HealthResponse{
		Status:    string(healthStatus.Status),
		Timestamp: healthStatus.Timestamp,
		Version:   s.version,
		Edition:   editions.Current.String(),
		Features:  features,
		Uptime:    time.Since(s.startTime).String(),
		Checks:    checks,
	}
	s.respondJSON(w, httpStatus, response)
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
