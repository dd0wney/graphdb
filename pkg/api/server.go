package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	tlspkg "github.com/dd0wney/graphdb/pkg/tls"
)

// Start starts the HTTP server
// registerRoutes wires every HTTP route onto mux. Extracted from Start() so the
// route table — and the auth/tenant middleware each route is wrapped in — is
// testable without binding a listener (see the registration tests).
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Health and metrics (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/health/ready", s.healthChecker.ReadinessHandler())
	mux.Handle("/health/live", s.healthChecker.LivenessHandler())
	mux.Handle("/metrics", promhttp.HandlerFor(s.metricsRegistry.GetPrometheusRegistry(), promhttp.HandlerOpts{}))

	// Authentication endpoints (public, but with stricter rate limiting)
	// Auth rate limiter is always enabled to protect against brute-force attacks
	mux.Handle("/auth/", s.authRateLimitMiddleware(s.authHandler))

	// OIDC authentication endpoints (public, if enabled)
	if s.oidcHandler != nil {
		mux.Handle("/auth/oidc/", s.authRateLimitMiddleware(s.oidcHandler))
	}

	// Metrics endpoint (JSON format for dashboard, admin-only). Returns
	// GLOBAL GetStatistics() (NodeCount/EdgeCount/TotalQueries across all
	// tenants) plus operator system stats (memory/goroutines/CPU), so it is
	// an operator endpoint — gating it behind requireAdmin stops an
	// authenticated tenant user from reading cross-tenant volume signals.
	mux.HandleFunc("/api/metrics", s.requireAdmin(s.handleMetrics))

	// Query endpoints (protected, tenant-scoped — audit A5).
	// withTenant injects request-scoped tenant context so the executor
	// (when migrated in audit task A6) can scope MATCH iteration to the
	// caller's tenant rather than the global graph.
	mux.HandleFunc("/query", s.requireAuth(s.withTenant(s.handleQuery)))

	// GraphQL endpoint (protected, tenant-scoped — audit A5).
	mux.HandleFunc("/graphql", s.requireAuth(s.withTenant(s.handleGraphQL)))

	// Node endpoints (protected, tenant-scoped — audit A5). Closes the
	// route-level half of Security CRIT #1+#2: the storage layer enforces
	// tenant ownership (A3b), but handlers must pass the tenant from
	// request context. withTenant ensures the context is populated; A6
	// migrates the handlers to use *ForTenant variants.
	mux.HandleFunc("/nodes", s.requireAuth(s.withTenant(s.handleNodes)))
	mux.HandleFunc("/nodes/", s.requireAuth(s.withTenant(s.handleNode))) // /nodes/{id}
	mux.HandleFunc("/nodes/batch", s.requireAuth(s.withTenant(s.handleBatchNodes)))

	// Edge endpoints (protected, tenant-scoped — audit A5).
	mux.HandleFunc("/edges", s.requireAuth(s.withTenant(s.handleEdges)))
	mux.HandleFunc("/edges/", s.requireAuth(s.withTenant(s.handleEdge))) // /edges/{id}
	mux.HandleFunc("/edges/batch", s.requireAuth(s.withTenant(s.handleBatchEdges)))

	// Traversal endpoints (protected, tenant-scoped — audit A5).
	mux.HandleFunc("/traverse", s.requireAuth(s.withTenant(s.handleTraversal)))
	mux.HandleFunc("/shortest-path", s.requireAuth(s.withTenant(s.handleShortestPath)))

	// Algorithm endpoints (protected, tenant-scoped — audit A5).
	mux.HandleFunc("/algorithms", s.requireAuth(s.withTenant(s.handleAlgorithm)))

	// Vector search endpoints (protected, tenant-scoped)
	mux.HandleFunc("/vector-indexes", s.requireAuth(s.withTenant(s.handleVectorIndexes)))
	mux.HandleFunc("/vector-indexes/", s.requireAuth(s.withTenant(s.handleVectorIndex)))
	mux.HandleFunc("/vector-search", s.requireAuth(s.withTenant(s.handleVectorSearch)))

	// Full-text search (protected, tenant-scoped)
	mux.HandleFunc("/search", s.requireAuth(s.withTenant(s.handleSearch)))

	// Hybrid (FTS + LSA) search (protected, tenant-scoped)
	mux.HandleFunc("/hybrid-search", s.requireAuth(s.withTenant(s.handleHybridSearch)))

	// OpenAI-compatible embeddings (protected, tenant-scoped). Backed by the
	// per-tenant LSA index so apps that already speak OpenAI (LangChain,
	// Vercel AI SDK, etc.) can drop in by setting api_base.
	mux.HandleFunc("/v1/embeddings", s.requireAuth(s.withTenant(s.handleEmbeddings)))

	// Graph-augmented retrieval (F2). LangChain BaseRetriever shape;
	// composes hybrid search + tenant-scoped BFS expansion. See
	// docs/F2_GRAPHRAG_DESIGN.md.
	mux.HandleFunc("/v1/retrieve", s.requireAuth(s.withTenant(s.handleRetrieve)))

	// Compliance API (F3). Tenant-scoped audit log with admin cross-
	// tenant override (X-Tenant-ID or ?tenant=*). See
	// docs/F3_COMPLIANCE_API_DESIGN.md.
	mux.HandleFunc("/v1/compliance/audit-log", s.requireAuth(s.withTenant(s.handleComplianceAuditLog)))

	// F3 masking-policy CRUD. Single mux entry catches both
	//   POST /v1/compliance/masking-policy           (admin-only Set; target via withTenant)
	//   GET  /v1/compliance/masking-policy/{tenant}  (admin OR self-tenant)
	// Dispatch lives in handleComplianceMaskingPolicy; the trailing
	// slash registration captures path-suffix calls like
	// /v1/compliance/masking-policy/tenant-a.
	mux.HandleFunc("/v1/compliance/masking-policy", s.requireAuth(s.withTenant(s.handleComplianceMaskingPolicy)))
	mux.HandleFunc("/v1/compliance/masking-policy/", s.requireAuth(s.withTenant(s.handleComplianceMaskingPolicy)))

	// Search index population (admin-only, tenant-scoped)
	mux.HandleFunc("/search/index", s.requireAdmin(s.withTenant(s.handleSearchIndex)))
	mux.HandleFunc("/hybrid-search/lsa-index", s.requireAdmin(s.withTenant(s.handleLSAIndex)))

	// User management endpoints (admin only) - handler does its own auth check
	mux.Handle("/api/users", s.userHandler)
	mux.Handle("/api/users/", s.userHandler)

	// Security management endpoints (admin only - sensitive operations)
	mux.HandleFunc("/api/v1/security/keys/rotate", s.requireAdmin(s.handleSecurityKeyRotate))
	mux.HandleFunc("/api/v1/security/keys/info", s.requireAdmin(s.handleSecurityKeyInfo))
	mux.HandleFunc("/api/v1/security/audit/logs", s.requireAdmin(s.handleSecurityAuditLogs))
	mux.HandleFunc("/api/v1/security/audit/export", s.requireAdmin(s.handleSecurityAuditExport))
	mux.HandleFunc("/api/v1/security/health", s.requireAdmin(s.handleSecurityHealth))

	// Software update endpoints (admin only). See pkg/updater for the
	// download/verify/swap pipeline and docs/internals/design/AUDIT_pkg_updater_2026-05-13.md
	// for the threat model this surface implements.
	mux.HandleFunc("/admin/update/check", s.requireAdmin(s.handleUpdateCheck))
	mux.HandleFunc("/admin/update/apply", s.requireAdmin(s.handleUpdateApply))
	mux.HandleFunc("/admin/update/jobs/", s.requireAdmin(s.handleUpdateJob))

	// API Key management endpoints (admin only)
	mux.HandleFunc("/api/v1/apikeys", s.requireAdmin(s.handleAPIKeys))
	mux.HandleFunc("/api/v1/apikeys/", s.requireAdmin(s.handleAPIKey))

	// Multi-tenant management endpoints
	if s.tenantStore != nil {
		// Admin-only tenant management
		mux.HandleFunc("/api/v1/tenants", s.requireAdmin(s.handleTenantsEndpoint))
		// withTenant is required: handleGetTenant's self-access check compares
		// the path tenant against getTenantFromContext(r), which without
		// withTenant falls back to DefaultTenantID — so a non-admin was denied
		// their own tenant AND could read the "default" tenant's metadata.
		// withTenant resolves the caller's tenant from their claims so the
		// self-check is correct. (Tenant-isolation sweep F2.)
		mux.HandleFunc("/api/v1/tenants/", s.requireAuth(s.withTenant(s.handleTenantEndpoint)))
	}

	// Schema management endpoints (admin only).
	// Audit A9 #3: per-tenant schema invalidation. Tenant context is
	// required so the handler knows whose cache entry to drop.
	mux.HandleFunc("/api/v1/schema/regenerate", s.requireAdmin(s.withTenant(s.handleSchemaRegenerate)))

	// OpenAPI documentation (public)
	mux.HandleFunc("/api/docs/openapi.yaml", s.handleOpenAPISpec)
	mux.HandleFunc("/api/docs/openapi.json", s.handleOpenAPISpec)
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

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
	log.Printf("   API Metrics:   GET  %s://%s/api/metrics (admin, JSON format)", protocol, addr)
	log.Printf("   Login:         POST %s://%s/auth/login (public)", protocol, addr)
	log.Printf("   Register:      POST %s://%s/auth/register (requires admin)", protocol, addr)
	log.Printf("   Refresh:       POST %s://%s/auth/refresh (public)", protocol, addr)
	log.Printf("   Current User:  GET  %s://%s/auth/me (requires auth)", protocol, addr)
	if s.oidcHandler != nil {
		log.Printf("🔐 OIDC Authentication (public):")
		log.Printf("   OIDC Login:    GET  %s://%s/auth/oidc/login (redirects to IdP)", protocol, addr)
		log.Printf("   OIDC Callback: GET  %s://%s/auth/oidc/callback (handles IdP response)", protocol, addr)
		log.Printf("   OIDC Token:    POST %s://%s/auth/oidc/token (validates OIDC token)", protocol, addr)
	}
	log.Printf("   Query:         POST %s://%s/query (requires auth)", protocol, addr)
	log.Printf("   GraphQL:       POST %s://%s/graphql (requires auth)", protocol, addr)
	log.Printf("   Nodes:         GET/POST %s://%s/nodes (requires auth)", protocol, addr)
	log.Printf("   Edges:         GET/POST %s://%s/edges (requires auth)", protocol, addr)
	log.Printf("   Traverse:      POST %s://%s/traverse (requires auth)", protocol, addr)
	log.Printf("   Shortest Path: POST %s://%s/shortest-path (requires auth)", protocol, addr)
	log.Printf("   Algorithms:    POST %s://%s/algorithms (requires auth)", protocol, addr)
	log.Printf("🔍 Vector Search (requires auth):")
	log.Printf("   Indexes:       GET/POST %s://%s/vector-indexes", protocol, addr)
	log.Printf("   Index:         GET/DELETE %s://%s/vector-indexes/{name}", protocol, addr)
	log.Printf("   Search:        POST %s://%s/vector-search", protocol, addr)
	log.Printf("👤 User Management (admin only):")
	log.Printf("   List Users:    GET  %s://%s/api/users (admin)", protocol, addr)
	log.Printf("   Create User:   POST %s://%s/api/users (admin)", protocol, addr)
	log.Printf("   Get User:      GET  %s://%s/api/users/{id} (admin)", protocol, addr)
	log.Printf("   Update User:   PUT  %s://%s/api/users/{id} (admin)", protocol, addr)
	log.Printf("   Delete User:   DELETE %s://%s/api/users/{id} (admin)", protocol, addr)
	log.Printf("   Change Password: PUT  %s://%s/api/users/{id}/password (admin)", protocol, addr)
	log.Printf("📋 Security Management (admin only):")
	log.Printf("   Key Rotation:  POST %s://%s/api/v1/security/keys/rotate (admin)", protocol, addr)
	log.Printf("   Key Info:      GET  %s://%s/api/v1/security/keys/info (admin)", protocol, addr)
	log.Printf("   Audit Logs:    GET  %s://%s/api/v1/security/audit/logs (admin)", protocol, addr)
	log.Printf("   Audit Export:  POST %s://%s/api/v1/security/audit/export (admin)", protocol, addr)
	log.Printf("   Security Health: GET  %s://%s/api/v1/security/health (admin)", protocol, addr)
	log.Printf("📑 Compliance API (F3, tenant-scoped):")
	log.Printf("   Audit Log:     GET  %s://%s/v1/compliance/audit-log (tenant; admin: X-Tenant-ID or ?tenant=*)", protocol, addr)
	log.Printf("🔑 API Key Management (admin only):")
	log.Printf("   List Keys:     GET  %s://%s/api/v1/apikeys (admin)", protocol, addr)
	log.Printf("   Create Key:    POST %s://%s/api/v1/apikeys (admin)", protocol, addr)
	log.Printf("   Revoke Key:    DELETE %s://%s/api/v1/apikeys/{id} (admin)", protocol, addr)
	log.Printf("📊 Schema Management (admin only):")
	log.Printf("   Regenerate:    POST %s://%s/api/v1/schema/regenerate (admin)", protocol, addr)
	if s.tenantStore != nil {
		log.Printf("🏢 Tenant Management (multi-tenancy enabled):")
		log.Printf("   List Tenants:  GET  %s://%s/api/v1/tenants (admin)", protocol, addr)
		log.Printf("   Create Tenant: POST %s://%s/api/v1/tenants (admin)", protocol, addr)
		log.Printf("   Get Tenant:    GET  %s://%s/api/v1/tenants/{id} (admin/self)", protocol, addr)
		log.Printf("   Update Tenant: PUT  %s://%s/api/v1/tenants/{id} (admin)", protocol, addr)
		log.Printf("   Delete Tenant: DELETE %s://%s/api/v1/tenants/{id} (admin)", protocol, addr)
		log.Printf("   Tenant Usage:  GET  %s://%s/api/v1/tenants/{id}/usage (admin/self)", protocol, addr)
		log.Printf("   Suspend:       POST %s://%s/api/v1/tenants/{id}/suspend (admin)", protocol, addr)
		log.Printf("   Activate:      POST %s://%s/api/v1/tenants/{id}/activate (admin)", protocol, addr)
	}
	log.Printf("📄 API Documentation (public):")
	log.Printf("   OpenAPI YAML:  GET  %s://%s/api/docs/openapi.yaml", protocol, addr)
	log.Printf("   OpenAPI JSON:  GET  %s://%s/api/docs/openapi.json", protocol, addr)

	// Start background metrics updater
	s.metricsStopCh = make(chan struct{})
	s.metricsWg.Add(1)
	go s.updateMetricsPeriodically()

	// Súil observability (no-op if SUIL_ENDPOINT/SUIL_API_KEY not set)
	suil := newSuilClient()

	// Create HTTP server with timeouts for production security
	// Middleware chain: suil -> metrics -> panicRecovery -> requestID -> rateLimit -> securityHeaders -> bodyLimit -> inputValidation -> auditCollector -> audit -> logging -> CORS -> routes
	// bodyLimit sits ahead of inputValidation so EVERY request — including
	// the /auth/* paths inputValidation skips — has a body bound before
	// anything reads it (security audit M-4).
	// auditCollector must wrap audit so the per-request mutable identity
	// holder is in context before audit emits the event; inner per-route
	// middlewares (requireAuth, withTenant) then write through the
	// pointer. Without this, audit-event UserID/Username/TenantID are
	// always empty because requireAuth/withTenant install claims/tenant
	// via r.WithContext(ctx) — visible only downstream of the wrap.
	server := &http.Server{
		Addr:         addr,
		Handler:      suilMiddleware(suil)(s.metricsMiddleware(s.panicRecoveryMiddleware(s.requestIDMiddleware(s.rateLimitMiddleware(s.securityHeadersMiddleware(s.bodyLimitMiddleware(s.inputValidationMiddleware(s.auditCollectorMiddleware(s.auditMiddleware(s.loggingMiddleware(s.corsMiddleware(mux)))))))))))),
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
