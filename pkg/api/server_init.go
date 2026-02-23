package api

import (
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/auth/oidc"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/queryutil"
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

	// Generate GraphQL schema with limits (includes filtering, sorting, edges)
	schema, err := graphql.GenerateSchemaWithLimits(graph, limitConfig)
	if err != nil {
		log.Printf("Warning: Failed to generate GraphQL schema: %v", err)
	}

	var graphqlHandler *graphql.GraphQLHandler
	if err == nil {
		graphqlHandler = graphql.NewGraphQLHandler(schema)
		log.Printf("✅ GraphQL schema generated with limits (max: %d, default: %d) and complexity validation (max: %d)",
			limitConfig.MaxLimit, limitConfig.DefaultLimit, complexityConfig.MaxComplexity)
	}

	// Initialize authentication components
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		// In production, JWT_SECRET must be set
		if os.Getenv("GRAPHDB_ENV") == "production" {
			return nil, fmt.Errorf("JWT_SECRET environment variable is required in production")
		}
		// Generate a random secret for development only
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			return nil, fmt.Errorf("failed to generate JWT secret: %w", err)
		}
		jwtSecret = fmt.Sprintf("%x", randomBytes)
		log.Printf("⚠️  WARNING: Generated random JWT secret for development. Set JWT_SECRET for production!")
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

	server := &Server{
		graph:               graph,
		executor:            queryutil.WireCapabilities(query.NewExecutor(graph), graph),
		graphqlHandler:      graphqlHandler,
		graphqlSchema:       schema,
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

	return server, nil
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
