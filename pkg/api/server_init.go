package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/auth/oidc"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/intelligence"
	"github.com/dd0wney/cluso-graphdb/pkg/masking"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/queryutil"
	"github.com/dd0wney/cluso-graphdb/pkg/retrieval"
	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// NewServer creates a new API server
// dataDir is used for persisting auth data (users, API keys)
func NewServer(graph *storage.GraphStorage, port int) (*Server, error) {
	// Get data directory from graph storage or environment
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data/server"
	}
	return NewServerWithDataDir(graph, port, dataDir)
}

// NewServerWithDataDir creates a new API server with explicit data directory
func NewServerWithDataDir(graph *storage.GraphStorage, port int, dataDir string) (*Server, error) {
	// Initialize GraphQL limit config from environment
	limitConfig := &graphql.LimitConfig{
		DefaultLimit: getEnvInt("GRAPHQL_DEFAULT_LIMIT", 100),
		MaxLimit:     getEnvInt("GRAPHQL_MAX_LIMIT", 1000),
	}

	// Initialize GraphQL complexity config from environment
	complexityConfig := &graphql.ComplexityConfig{
		MaxComplexity:    getEnvInt("GRAPHQL_MAX_COMPLEXITY", 5000),
		ListMultiplier:   getEnvInt("GRAPHQL_LIST_MULTIPLIER", 10),
		DefaultListLimit: getEnvInt("GRAPHQL_DEFAULT_LIST_LIMIT", 100),
	}

	// Audit A9 #3 (2026-05-08): no eager schema build at startup.
	// Schemas are now per-tenant and built lazily on first /graphql
	// request per tenant. The Server holds a sync.Map cache plus a
	// singleflight.Group to dedupe concurrent cold-starts. See
	// server_types.go and getGraphQLHandlerForTenant in
	// server_handlers.go.
	log.Printf("✅ GraphQL: per-tenant schemas (limits max: %d, default: %d, complexity max: %d) — built lazily per tenant on first request",
		limitConfig.MaxLimit, limitConfig.DefaultLimit, complexityConfig.MaxComplexity)

	// Initialize authentication components.
	//
	// JWT_SECRET is required in every environment. The previous behaviour
	// (silently generating a random secret unless GRAPHDB_ENV=="production")
	// was a security finding from the 2026-05-06 audit: a misconfigured
	// staging/pre-prod environment would silently rotate the secret on every
	// restart, invalidating all active sessions, rather than refusing to
	// start. Fail-closed prevents that misconfiguration class.
	//
	// For local development, set any non-empty value in your .env or shell
	// environment (e.g. `export JWT_SECRET=dev-only-secret-not-for-production`).
	// Tests set this via TestMain in each test package.
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET environment variable is required (set any non-empty value for local development; tests set it via TestMain)")
	}

	userStore := auth.NewUserStore()
	apiKeyStore := auth.NewAPIKeyStore()

	// Load persisted auth data
	authDataDir := dataDir + "/auth"
	if err := userStore.LoadUsers(authDataDir); err != nil {
		log.Printf("⚠️  Warning: Failed to load users from disk: %v", err)
	} else if len(userStore.ListUsers()) > 0 {
		log.Printf("✅ Loaded %d users from disk", len(userStore.ListUsers()))
	}

	if err := apiKeyStore.LoadAPIKeys(authDataDir); err != nil {
		log.Printf("⚠️  Warning: Failed to load API keys from disk: %v", err)
	} else {
		// Count non-revoked keys
		allUsers := userStore.ListUsers()
		keyCount := 0
		for _, u := range allUsers {
			keyCount += len(apiKeyStore.ListKeys(u.ID))
		}
		if keyCount > 0 {
			log.Printf("✅ Loaded %d API keys from disk", keyCount)
		}
	}

	jwtManager, err := auth.NewJWTManager(jwtSecret, auth.DefaultTokenDuration, auth.DefaultRefreshTokenDuration)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JWT manager: %w", err)
	}
	authHandler := auth.NewAuthHandler(userStore, jwtManager)
	userHandler := auth.NewUserManagementHandler(userStore, jwtManager)

	// Initialize OIDC authentication (optional)
	var oidcHandler *oidc.OIDCHandler
	var oidcConfig *oidc.Config
	var tokenValidator auth.TokenValidator = jwtManager // Default to JWT-only

	oidcConfig, err = oidc.LoadConfigFromEnv()
	if err != nil {
		log.Printf("⚠️  Warning: Failed to load OIDC config: %v", err)
	} else if oidcConfig.Enabled {
		oidcHandler = oidc.NewOIDCHandler(oidcConfig, userStore, jwtManager)

		// Create composite validator that tries JWT first, then OIDC
		oidcValidator := oidc.NewOIDCTokenValidator(oidcConfig)
		tokenValidator = auth.NewCompositeTokenValidator(jwtManager, oidcValidator)

		log.Printf("✅ OIDC authentication enabled (issuer: %s)", oidcConfig.Issuer)
	}

	// Initialize audit logging - always have in-memory for GetEvents/GetRecentEvents API
	inMemoryAuditLogger := audit.NewAuditLogger(10000) // Store last 10,000 events

	// Check if persistent audit logging is enabled
	var auditLogger audit.Logger = inMemoryAuditLogger
	var persistentAudit *audit.PersistentAuditLogger

	if os.Getenv("AUDIT_PERSISTENT") == "true" {
		auditDir := os.Getenv("AUDIT_DIR")
		if auditDir == "" {
			auditDir = "./data/audit" // Default directory
		}

		config := audit.DefaultPersistentConfig()
		config.LogDir = auditDir

		// Allow configuration overrides
		if compress := os.Getenv("AUDIT_COMPRESS"); compress == "false" {
			config.Compress = false
		}

		var err error
		persistentAudit, err = audit.NewPersistentAuditLogger(config)
		if err != nil {
			log.Printf("⚠️  WARNING: Failed to initialize persistent audit logging: %v", err)
			log.Printf("   Falling back to in-memory audit logging")
		} else {
			auditLogger = persistentAudit
			log.Printf("✅ Persistent audit logging enabled (dir: %s)", auditDir)
		}
	}

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

	// Create default admin user if no users exist and ADMIN_PASSWORD is provided
	if len(userStore.ListUsers()) == 0 {
		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword == "" {
			// In production, require explicit admin password
			if os.Getenv("GRAPHDB_ENV") == "production" {
				log.Printf("⚠️  WARNING: No admin user created. Set ADMIN_PASSWORD to create initial admin user.")
			} else {
				// Generate a random password for development only
				randomBytes := make([]byte, 16)
				if _, err := rand.Read(randomBytes); err != nil {
					log.Printf("Warning: Failed to generate admin password: %v", err)
				} else {
					adminPassword = fmt.Sprintf("%x", randomBytes)
					// Write password to a secure file with restrictive permissions
					pwFile := ".graphdb_admin_password"
					if err := os.WriteFile(pwFile, []byte(adminPassword+"\n"), 0600); err != nil {
						log.Printf("Warning: Failed to write admin password file: %v", err)
					} else {
						log.Printf("⚠️  DEVELOPMENT MODE: Admin password written to %s (mode 0600)", pwFile)
						log.Printf("   Set ADMIN_PASSWORD environment variable for production!")
					}
				}
			}
		}

		if adminPassword != "" {
			admin, err := userStore.CreateUser("admin", adminPassword, auth.RoleAdmin)
			if err != nil {
				log.Printf("Warning: Failed to create default admin user: %v", err)
			} else {
				log.Printf("✅ Created admin user: %s", admin.Username)
				// Persist the new user immediately
				if err := userStore.SaveUsers(authDataDir); err != nil {
					log.Printf("⚠️  Warning: Failed to persist admin user: %v", err)
				}
			}
		}
	}

	// Determine server environment for API key enforcement
	serverEnv := "test" // default to test for safety
	if os.Getenv("GRAPHDB_ENV") == "production" {
		serverEnv = "live"
	}

	// Initialize multi-tenant store (enabled by default)
	var tenantStore *tenant.TenantStore
	if os.Getenv("TENANT_ENABLED") != "false" {
		tenantStore = tenant.NewTenantStore()
		log.Printf("✅ Multi-tenancy enabled (default tenant: %s)", tenant.DefaultTenantID)
	}

	// WireCapabilities gives the executor its own (DSL-facing) FullTextIndex.
	// The REST surface uses per-tenant indexes via TenantIndexes instead;
	// tenant-aware DSL search() is a follow-up.
	executor, _ := queryutil.WireCapabilities(query.NewExecutor(graph), graph)

	searchIndexes := search.NewTenantIndexes(graph)
	lsaIndexes := search.NewTenantLSAIndexes()

	// Restore any LSA indexes persisted by a previous run. Missing dir
	// (fresh deployment) is not an error — LoadAll returns nil and the
	// admin bootstrap path / env-bootstrap handles first-time builds.
	// Per-tenant decode errors are logged but don't block boot; the
	// affected tenant simply lacks an index until an admin rebuilds.
	lsaSnapshotDir := filepath.Join(dataDir, "lsa")
	if err := lsaIndexes.LoadAll(lsaSnapshotDir); err != nil {
		log.Printf("LSA snapshot restore: %v (boot continues; affected tenants need rebuild)", err)
	} else if tenants := lsaIndexes.Tenants(); len(tenants) > 0 {
		log.Printf("✅ Restored LSA indexes from disk for %d tenant(s): %v", len(tenants), tenants)
	}

	server := &Server{
		graph:         graph,
		executor:      executor,
		searchIndexes: searchIndexes,
		lsaIndexes:    lsaIndexes,
		retriever:     retrieval.NewRetriever(graph, searchIndexes, lsaIndexes),
		updateJobs:    newUpdateJobManager(),
		// graphqlHandlers + schemaSingleflight zero-value initialised
		// (sync.Map and singleflight.Group both work zero-valued).
		complexityConfig:    complexityConfig,
		limitConfig:         limitConfig,
		authHandler:         authHandler,
		userHandler:         userHandler,
		jwtManager:          jwtManager,
		userStore:           userStore,
		apiKeyStore:         apiKeyStore,
		auditLogger:         auditLogger,
		inMemoryAuditLogger: inMemoryAuditLogger,
		persistentAudit:     persistentAudit,
		maskingPolicyStore:  masking.NewPolicyStore(),
		masker:              masking.NewMasker(masking.DefaultMaskingConfig()),
		metricsRegistry:     metricsRegistry,
		healthChecker:       healthChecker,
		tlsConfig:           nil, // TLS disabled by default
		oidcHandler:         oidcHandler,
		oidcConfig:          oidcConfig,
		tokenValidator:      tokenValidator,
		tenantStore:         tenantStore,
		startTime:           time.Now(),
		version:             "1.0.0",
		port:                port,
		dataDir:             dataDir,
		environment:         serverEnv,
	}

	// Initialize CORS from environment variables
	server.InitCORSFromEnv()

	// Bootstrap tenant indexes from environment if configured. Fails
	// soft — a bad config or corpus-too-small problem logs and continues
	// rather than refusing to boot.
	server.bootstrapIndexesFromEnv()

	// Bootstrap the auto-embed observer (R2.5b). Must run AFTER
	// bootstrapIndexesFromEnv so the LSA index that LSAEmbedder reads
	// is already registered for any tenants the auto-embed wiring will
	// fire for. Fails soft — missing config = no observer registered.
	server.bootstrapAutoEmbedFromEnv()

	return server, nil
}

// bootstrapIndexesFromEnv builds the FTS and/or LSA index for one or
// more tenants at startup when corresponding env vars are set. Intended
// for container deployments that can't easily curl the admin endpoints
// post-boot.
//
// FTS still bootstraps the default tenant only (multi-tenant FTS
// bootstrap is a future extension if requested).
//
// LSA supports multi-tenant bootstrap via GRAPHDB_LSA_BOOTSTRAP_TENANTS;
// if unset, falls back to the single "default" tenant for back-compat
// with the previous single-tenant behavior. All tenants in the list
// share the same labels / title_property / body_properties config —
// per-tenant config blocks are out of scope for the env-bootstrap path.
//
// FTS config (both required to trigger):
//
//	GRAPHDB_FTS_BOOTSTRAP_LABELS     comma-separated, e.g. "Doc,Note"
//	GRAPHDB_FTS_BOOTSTRAP_PROPERTIES comma-separated, e.g. "title,body"
//
// LSA config (labels + body_properties required; title_property optional):
//
//	GRAPHDB_LSA_BOOTSTRAP_LABELS          e.g. "Doc"
//	GRAPHDB_LSA_BOOTSTRAP_TITLE_PROPERTY  e.g. "title" (optional)
//	GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES e.g. "body,summary"
//	GRAPHDB_LSA_BOOTSTRAP_DIMS            e.g. "200" (optional)
//	GRAPHDB_LSA_BOOTSTRAP_TENANTS         e.g. "default,acme,corp" (optional;
//	                                      defaults to "default")
func (s *Server) bootstrapIndexesFromEnv() {
	const defaultTenantID = "default"

	// FTS bootstrap.
	if labels := splitEnvCSV("GRAPHDB_FTS_BOOTSTRAP_LABELS"); len(labels) > 0 {
		props := splitEnvCSV("GRAPHDB_FTS_BOOTSTRAP_PROPERTIES")
		if len(props) == 0 {
			log.Printf("bootstrap: GRAPHDB_FTS_BOOTSTRAP_LABELS set but _PROPERTIES empty; skipping FTS bootstrap")
		} else if err := s.searchIndexes.IndexForTenant(defaultTenantID, labels, props); err != nil {
			log.Printf("bootstrap: FTS IndexForTenant(default) failed: %v", err)
		} else {
			log.Printf("✅ Bootstrapped FTS index for default tenant (labels=%v, properties=%v)", labels, props)
		}
	}

	// LSA bootstrap.
	if labels := splitEnvCSV("GRAPHDB_LSA_BOOTSTRAP_LABELS"); len(labels) > 0 {
		bodyProps := splitEnvCSV("GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES")
		if len(bodyProps) == 0 {
			log.Printf("bootstrap: GRAPHDB_LSA_BOOTSTRAP_LABELS set but _BODY_PROPERTIES empty; skipping LSA bootstrap")
			return
		}
		titleProp := os.Getenv("GRAPHDB_LSA_BOOTSTRAP_TITLE_PROPERTY")

		tenants := splitEnvCSV("GRAPHDB_LSA_BOOTSTRAP_TENANTS")
		if len(tenants) == 0 {
			tenants = []string{defaultTenantID}
		}
		for _, tenantID := range tenants {
			if err := s.buildAndRegisterLSA(tenantID, labels, titleProp, bodyProps); err != nil {
				log.Printf("bootstrap: LSA build for tenant %q failed: %v", tenantID, err)
			}
		}
	}
}

// buildAndRegisterLSA gathers the tenant's nodes, constructs Document
// records, runs BuildLSAIndex, and registers the result. Shared between
// env-bootstrap and the future scheduled-rebuild path.
func (s *Server) buildAndRegisterLSA(tenantID string, labels []string, titleProp string, bodyProps []string) error {
	var nodes []*storage.Node
	for _, label := range labels {
		nodes = append(nodes, s.graph.GetNodesByLabelForTenant(tenantID, label)...)
	}

	docs := make([]search.Document, 0, len(nodes))
	for _, n := range nodes {
		title := stringProperty(n, titleProp)
		var bodyParts []string
		for _, p := range bodyProps {
			if v := stringProperty(n, p); v != "" {
				bodyParts = append(bodyParts, v)
			}
		}
		body := strings.Join(bodyParts, " ")
		if body == "" {
			continue
		}
		docs = append(docs, search.Document{ID: n.ID, Title: title, Body: body})
	}

	cfg := search.DefaultLSAConfig()
	if v := getEnvInt("GRAPHDB_LSA_BOOTSTRAP_DIMS", 0); v > 0 {
		cfg.Dims = v
	}
	if v := getEnvInt("GRAPHDB_LSA_BOOTSTRAP_MIN_DOC_FREQ", 0); v > 0 {
		cfg.MinDocFreq = v
	}
	if v := getEnvInt("GRAPHDB_LSA_BOOTSTRAP_MAX_VOCAB", 0); v > 0 {
		cfg.MaxVocab = v
	}
	if v := getEnvInt("GRAPHDB_LSA_BOOTSTRAP_TITLE_BOOST", 0); v > 0 {
		cfg.TitleBoost = v
	}

	idx, err := search.BuildLSAIndex(docs, cfg)
	if err != nil {
		return err
	}
	s.lsaIndexes.Set(tenantID, idx)
	log.Printf("✅ Bootstrapped LSA index for %s (%d docs, %d dims)", tenantID, idx.NumDocs(), idx.Dimensions())

	// Persist so a subsequent server restart restores this index from
	// disk instead of returning 503 on /v1/embeddings until admin
	// re-triggers a build. Save failure is non-fatal — the in-memory
	// index is still usable; we just lose the post-restart guarantee.
	snapshotPath := filepath.Join(s.dataDir, "lsa", tenantID+".lsa")
	if err := idx.SaveToFile(snapshotPath); err != nil {
		log.Printf("LSA snapshot save for %s: %v (in-memory index still active; restart will require rebuild)", tenantID, err)
	}
	return nil
}

// bootstrapAutoEmbedFromEnv constructs and registers an AutoEmbedObserver
// when GRAPHDB_AUTO_EMBED_ENABLED is "true" / "1" and the required policy
// env vars are set. The observer dispatches embed tasks to a worker Pool
// (stored on s.autoEmbedPool) on every node creation matching the
// configured label, computing an embedding via LSAEmbedder backed by the
// server's TenantLSAIndexes registry.
//
// Required env vars (all three must be non-empty to trigger):
//
//	GRAPHDB_AUTO_EMBED_ENABLED          "true" or "1"
//	GRAPHDB_AUTO_EMBED_LABEL            e.g. "Doc"
//	GRAPHDB_AUTO_EMBED_SOURCE_PROPERTY  e.g. "body"
//	GRAPHDB_AUTO_EMBED_TARGET_PROPERTY  e.g. "embedding"
//
// Optional env vars:
//
//	GRAPHDB_AUTO_EMBED_WORKERS          worker pool size (default 4)
//	GRAPHDB_AUTO_EMBED_QUEUE_DEPTH      pool queue capacity (default 256)
//
// Fails soft: missing required vars or constructor errors log a warning
// and leave s.autoEmbedPool nil. The observer is not registered, so node
// creates pass through unchanged. Operators inspecting logs see exactly
// why auto-embed is inactive.
//
// Bootstrap ordering: this method runs AFTER bootstrapIndexesFromEnv so
// any LSA index built from env vars is already registered. Auto-embed
// fires at node-create time; if the LSA index isn't built yet,
// LSAEmbedder returns ErrNoIndexForTenant and the observer drops the
// task (no panic, no writeback). Operators can build the LSA index
// later via POST /hybrid-search/lsa-index without restarting.
func (s *Server) bootstrapAutoEmbedFromEnv() {
	enabled := os.Getenv("GRAPHDB_AUTO_EMBED_ENABLED")
	if enabled != "true" && enabled != "1" {
		return
	}

	label := os.Getenv("GRAPHDB_AUTO_EMBED_LABEL")
	sourceProp := os.Getenv("GRAPHDB_AUTO_EMBED_SOURCE_PROPERTY")
	targetProp := os.Getenv("GRAPHDB_AUTO_EMBED_TARGET_PROPERTY")
	if label == "" || sourceProp == "" || targetProp == "" {
		log.Printf("bootstrap: GRAPHDB_AUTO_EMBED_ENABLED set but LABEL/SOURCE_PROPERTY/TARGET_PROPERTY missing; skipping auto-embed bootstrap")
		return
	}

	cfg := intelligence.PoolConfig{
		Workers:    getEnvInt("GRAPHDB_AUTO_EMBED_WORKERS", 0),
		QueueDepth: getEnvInt("GRAPHDB_AUTO_EMBED_QUEUE_DEPTH", 0),
	}
	pool := intelligence.NewPool(cfg)

	embedder := intelligence.NewLSAEmbedder(s.lsaIndexes)

	policies := []intelligence.EmbeddingPolicy{{
		Label:          label,
		SourceProperty: sourceProp,
		TargetProperty: targetProp,
	}}

	obs, err := intelligence.NewAutoEmbedObserver(s.graph, embedder, pool, policies)
	if err != nil {
		log.Printf("bootstrap: NewAutoEmbedObserver failed: %v; skipping auto-embed bootstrap", err)
		_ = pool.Shutdown(context.Background())
		return
	}

	s.graph.AddObserver(obs)
	s.autoEmbedPool = pool
	log.Printf("✅ Bootstrapped auto-embed observer (label=%q, source=%q, target=%q, workers=%d, queue=%d)",
		label, sourceProp, targetProp,
		nonZeroOrDefault(cfg.Workers, intelligence.DefaultWorkers),
		nonZeroOrDefault(cfg.QueueDepth, intelligence.DefaultQueueDepth))
}

// nonZeroOrDefault returns v if v > 0, otherwise defaultVal. Used to
// surface the actual configured value in startup logs (NewPool's internal
// defaults are otherwise invisible to operators).
func nonZeroOrDefault(v, defaultVal int) int {
	if v > 0 {
		return v
	}
	return defaultVal
}

// splitEnvCSV reads a comma-separated env var and returns non-empty
// trimmed entries. Returns nil for unset or empty.
func splitEnvCSV(key string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// getEnvInt reads an integer environment variable with a default value
func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}
