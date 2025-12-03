package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health and metrics (public)
	mux.HandleFunc("/health", s.handleHealth)
	mux.Handle("/health/ready", s.healthChecker.ReadinessHandler())
	mux.Handle("/health/live", s.healthChecker.LivenessHandler())
	mux.Handle("/metrics", promhttp.HandlerFor(s.metricsRegistry.GetPrometheusRegistry(), promhttp.HandlerOpts{}))

	// Authentication endpoints (public, but with stricter rate limiting)
	// Auth rate limiter is always enabled to protect against brute-force attacks
	mux.Handle("/auth/", s.authRateLimitMiddleware(s.authHandler))

	// Metrics endpoint (JSON format for dashboard, protected)
	mux.HandleFunc("/api/metrics", s.requireAuth(s.handleMetrics))

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

	// User management endpoints (admin only) - handler does its own auth check
	mux.Handle("/api/users", s.userHandler)
	mux.Handle("/api/users/", s.userHandler)

	// Security management endpoints (admin only - sensitive operations)
	mux.HandleFunc("/api/v1/security/keys/rotate", s.requireAdmin(s.handleSecurityKeyRotate))
	mux.HandleFunc("/api/v1/security/keys/info", s.requireAdmin(s.handleSecurityKeyInfo))
	mux.HandleFunc("/api/v1/security/audit/logs", s.requireAdmin(s.handleSecurityAuditLogs))
	mux.HandleFunc("/api/v1/security/audit/export", s.requireAdmin(s.handleSecurityAuditExport))
	mux.HandleFunc("/api/v1/security/health", s.requireAdmin(s.handleSecurityHealth))

	// API Key management endpoints (admin only)
	mux.HandleFunc("/api/v1/apikeys", s.requireAdmin(s.handleAPIKeys))
	mux.HandleFunc("/api/v1/apikeys/", s.requireAdmin(s.handleAPIKey))

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
	log.Printf("   API Metrics:   GET  %s://%s/api/metrics (requires auth, JSON format)", protocol, addr)
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
	log.Printf("🔑 API Key Management (admin only):")
	log.Printf("   List Keys:     GET  %s://%s/api/v1/apikeys (admin)", protocol, addr)
	log.Printf("   Create Key:    POST %s://%s/api/v1/apikeys (admin)", protocol, addr)
	log.Printf("   Revoke Key:    DELETE %s://%s/api/v1/apikeys/{id} (admin)", protocol, addr)

	// Start background metrics updater
	go s.updateMetricsPeriodically()

	// Create HTTP server with timeouts for production security
	// Middleware chain: metrics -> panicRecovery -> requestID -> rateLimit -> securityHeaders -> inputValidation -> audit -> logging -> CORS -> routes
	server := &http.Server{
		Addr:         addr,
		Handler:      s.metricsMiddleware(s.panicRecoveryMiddleware(s.requestIDMiddleware(s.rateLimitMiddleware(s.securityHeadersMiddleware(s.inputValidationMiddleware(s.auditMiddleware(s.loggingMiddleware(s.corsMiddleware(mux))))))))),
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
