package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dd0wney/graphdb/pkg/editions"
	"github.com/dd0wney/graphdb/pkg/graphql"
	"github.com/dd0wney/graphdb/pkg/health"
	"github.com/dd0wney/graphdb/pkg/query"
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

// getGraphQLHandlerForTenant returns the per-tenant GraphQL handler,
// building it on first call and caching for subsequent requests.
//
// Audit A9 #3 (2026-05-08): per-tenant schema isolation. The cache
// is sync.Map keyed on tenantID *string* (not tenantid.TenantID —
// mixing types here would silently bucket the same tenant twice).
// singleflight dedupes concurrent cold-starts so a thundering herd
// of N goroutines for the same tenant runs the build once.
//
// Errors are NOT cached: if GenerateSchemaWithLimitsForTenant fails
// (e.g., transient storage error), the next request retries. This
// avoids cache-poisoning on a flaky build.
func (s *Server) getGraphQLHandlerForTenant(tenantID string) (*graphql.GraphQLHandler, error) {
	// Fast path: cache hit.
	if cached, ok := s.graphqlHandlers.Load(tenantID); ok {
		h, ok := cached.(*graphql.GraphQLHandler)
		if !ok {
			return nil, fmt.Errorf("internal: unexpected type %T in graphql handler cache", cached)
		}
		return h, nil
	}

	// Slow path: dedupe concurrent builds via singleflight. Even if
	// 50 goroutines call simultaneously for the same tenantID,
	// exactly one runs the closure; the others wait for its result.
	result, err, _ := s.schemaSingleflight.Do(tenantID, func() (any, error) {
		// Double-check under singleflight: another goroutine may
		// have populated the cache between our Load above and our
		// Do entry.
		if cached, ok := s.graphqlHandlers.Load(tenantID); ok {
			h, ok := cached.(*graphql.GraphQLHandler)
			if !ok {
				return nil, fmt.Errorf("internal: unexpected type %T in graphql handler cache", cached)
			}
			return h, nil
		}

		// F3 PR-3b: pass masking deps so the per-tenant policy applies
		// to GraphQL responses (mirrors the REST applyMaskingPolicy
		// hook at server_helpers.go).
		maskingDeps := &graphql.MaskingDeps{
			Store:  s.maskingPolicyStore,
			Masker: s.masker,
		}
		schema, err := graphql.GenerateSchemaWithLimitsForTenant(s.graph, s.limitConfig, tenantID, maskingDeps)
		if err != nil {
			// Don't cache on error path — retry on next request.
			return nil, err
		}
		h := graphql.NewGraphQLHandler(schema)
		s.graphqlHandlers.Store(tenantID, h)
		return h, nil
	})
	if err != nil {
		return nil, err
	}
	h, ok := result.(*graphql.GraphQLHandler)
	if !ok {
		return nil, fmt.Errorf("internal: unexpected type %T from singleflight result", result)
	}
	return h, nil
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	// Resolve the per-tenant handler. Audit A9 #3: schema is now
	// keyed by tenantID; cold-start path runs through singleflight.
	tenantID := getTenantFromContext(r)
	handler, err := s.getGraphQLHandlerForTenant(tenantID)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "GraphQL schema build"))
		return
	}

	// Handle preflight OPTIONS request
	if r.Method == http.MethodOptions {
		handler.ServeHTTP(w, r)
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
	_ = r.Body.Close()

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

	// Restore the request body for the handler.
	//
	// No schemaLock needed: each tenant's handler is immutable once
	// built. handleSchemaRegenerate invalidates by Delete on the
	// sync.Map; in-flight requests holding the old handler reference
	// finish against the old schema (graceful), and the next request
	// for that tenant lazy-rebuilds.
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	handler.ServeHTTP(w, r)
}

// handleSchemaRegenerate invalidates the caller's tenant's cached
// schema. The next /graphql request for that tenant will lazy-build
// with current labels.
//
// Audit A9 #3: was a global lock + rebuild. Now per-tenant: a
// regenerate by tenant-A doesn't disturb tenant-B's cache.
func (s *Server) handleSchemaRegenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	tenantID := getTenantFromContext(r)
	s.graphqlHandlers.Delete(tenantID)

	s.respondJSON(w, http.StatusOK, map[string]any{
		"status":    "success",
		"message":   "GraphQL schema cache invalidated for tenant; next request rebuilds",
		"tenant_id": tenantID,
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
		if err := json.NewEncoder(w).Encode(spec); err != nil {
			// Response is already partially sent (headers committed);
			// can't respondError cleanly. Log for diagnostics.
			log.Printf("openapi: json encode failed: %v", err)
		}
		return
	}

	// Serve as YAML
	w.Header().Set("Content-Type", "application/x-yaml")
	if _, err := w.Write(specContent); err != nil {
		// Response is already partially sent (headers committed);
		// can't respondError cleanly. Log for diagnostics.
		log.Printf("openapi: yaml write failed: %v", err)
	}
}
