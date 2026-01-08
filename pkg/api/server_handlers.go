package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"gopkg.in/yaml.v3"
)

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

	// Convert health checks to any map for JSON serialization
	checks := make(map[string]any)
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
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats := s.graph.GetStatistics()

	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(s.startTime)

	response := MetricsResponse{
		// Database stats
		NodeCount:    stats.NodeCount,
		EdgeCount:    stats.EdgeCount,
		TotalQueries: stats.TotalQueries,
		AvgQueryTime: stats.AvgQueryTime,

		// System stats
		MemoryUsedMB:  m.Alloc / 1024 / 1024,
		MemoryTotalMB: m.Sys / 1024 / 1024,
		NumGoroutines: runtime.NumGoroutine(),
		NumCPU:        runtime.NumCPU(),

		// Server stats
		Uptime:        uptime.String(),
		UptimeSeconds: int64(uptime.Seconds()),
	}
	s.respondJSON(w, http.StatusOK, response)
}

// Per-query timeout constants
const (
	minQueryTimeoutSeconds = 1
	maxQueryTimeoutSeconds = 300 // 5 minutes
)

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

	// Determine query timeout
	timeout := query.DefaultQueryTimeout
	if req.TimeoutSeconds != nil {
		secs := *req.TimeoutSeconds
		if secs < minQueryTimeoutSeconds {
			s.respondError(w, http.StatusBadRequest,
				fmt.Sprintf("timeout_seconds must be at least %d", minQueryTimeoutSeconds))
			return
		}
		if secs > maxQueryTimeoutSeconds {
			s.respondError(w, http.StatusBadRequest,
				fmt.Sprintf("timeout_seconds must be at most %d", maxQueryTimeoutSeconds))
			return
		}
		timeout = time.Duration(secs) * time.Second
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

	// Execute query with timeout context
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	results, err := s.executor.ExecuteWithContext(ctx, parsedQuery)
	if err != nil {
		// Check if it was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			s.respondError(w, http.StatusRequestTimeout,
				fmt.Sprintf("Query timed out after %v", timeout))
			return
		}
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "query execution"))
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

	// Handle preflight OPTIONS request
	if r.Method == http.MethodOptions {
		s.graphqlHandler.ServeHTTP(w, r)
		return
	}

	// Only POST is allowed for queries
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Read and buffer the request body (needed for complexity check + execution)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	r.Body.Close()

	// Parse the GraphQL request to extract query for complexity validation
	var gqlReq graphql.GraphQLRequest
	if err := json.Unmarshal(bodyBytes, &gqlReq); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid GraphQL request")
		return
	}

	// Validate query complexity before execution (DoS protection)
	if s.complexityConfig != nil && s.complexityConfig.MaxComplexity > 0 {
		complexity, err := graphql.ValidateQueryComplexity(gqlReq.Query, s.complexityConfig, gqlReq.Variables)
		if err != nil {
			s.respondJSON(w, http.StatusOK, map[string]any{
				"data": nil,
				"errors": []map[string]string{
					{"message": fmt.Sprintf("Query rejected: %v (complexity: %d, max: %d)",
						err, complexity, s.complexityConfig.MaxComplexity)},
				},
			})
			return
		}
	}

	// Acquire read lock for schema access (allows concurrent reads, blocks during regeneration)
	s.schemaLock.RLock()
	defer s.schemaLock.RUnlock()

	// Restore the request body for the handler
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Delegate to GraphQL handler
	s.graphqlHandler.ServeHTTP(w, r)
}

// handleSchemaRegenerate regenerates the GraphQL schema to reflect new labels/types
func (s *Server) handleSchemaRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Acquire write lock to prevent concurrent schema access during regeneration
	s.schemaLock.Lock()
	defer s.schemaLock.Unlock()

	// Generate new schema with current limit config
	newSchema, err := graphql.GenerateSchemaWithLimits(s.graph, s.limitConfig)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to regenerate schema: %v", err))
		return
	}

	// Update schema and handler atomically (while holding write lock)
	s.graphqlSchema = newSchema
	s.graphqlHandler = graphql.NewGraphQLHandler(newSchema)

	s.respondJSON(w, http.StatusOK, map[string]any{
		"status":  "success",
		"message": "GraphQL schema regenerated successfully",
	})
}

// handleOpenAPISpec serves the OpenAPI specification in YAML or JSON format
func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Find the OpenAPI spec file - check common locations
	specPaths := []string{
		"docs/openapi.yaml",
		"./docs/openapi.yaml",
		filepath.Join(s.dataDir, "../docs/openapi.yaml"),
	}

	var specContent []byte
	var err error
	for _, path := range specPaths {
		specContent, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}

	if err != nil {
		s.respondError(w, http.StatusNotFound, "OpenAPI specification not found")
		return
	}

	// Check if JSON format is requested
	wantsJSON := strings.HasSuffix(r.URL.Path, ".json") ||
		strings.Contains(r.Header.Get("Accept"), "application/json")

	if wantsJSON {
		// Convert YAML to JSON
		var spec any
		if err := yaml.Unmarshal(specContent, &spec); err != nil {
			s.respondError(w, http.StatusInternalServerError, "Failed to parse OpenAPI spec")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(spec)
		return
	}

	// Serve as YAML
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(specContent)
}
