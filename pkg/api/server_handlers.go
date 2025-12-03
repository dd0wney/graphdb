package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/editions"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
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

	// Delegate to GraphQL handler
	s.graphqlHandler.ServeHTTP(w, r)
}
