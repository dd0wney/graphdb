package api

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/auth/oidc"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	gqlpkg "github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/masking"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/retrieval"
	"github.com/dd0wney/cluso-graphdb/pkg/search"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
)

// Server represents the HTTP API server
type Server struct {
	graph         *storage.GraphStorage
	executor      *query.Executor
	searchIndexes *search.TenantIndexes    // Per-tenant full-text indexes; empty until IndexForTenant is called for a given tenant
	lsaIndexes    *search.TenantLSAIndexes // Per-tenant LSA indexes for /hybrid-search; nil entry means LSA unavailable for that tenant
	retriever     *retrieval.Retriever     // Graph-augmented retrieval (F2 GraphRAG); composes searchIndexes + lsaIndexes + graph
	updateJobs    *updateJobManager        // In-memory tracker for /admin/update/apply jobs; lost on restart by design (a successful update IS the restart)

	// Audit A9 #3 (2026-05-08): per-tenant GraphQL schema cache.
	// Keyed by tenantID (string — converted at the validation
	// boundary; do NOT mix tenantid.TenantID here or you'll bucket
	// twice). Lazy build via singleflight to dedupe concurrent
	// cold-starts. /api/v1/schema/regenerate invalidates a single
	// tenant's entry (Delete) so the next request rebuilds.
	graphqlHandlers    sync.Map           // map[string]*gqlpkg.GraphQLHandler
	schemaSingleflight singleflight.Group // dedupes concurrent first-request schema builds per tenant

	complexityConfig    *gqlpkg.ComplexityConfig // GraphQL query complexity limits
	limitConfig         *gqlpkg.LimitConfig      // GraphQL result limits
	authHandler         *auth.AuthHandler
	userHandler         *auth.UserManagementHandler
	jwtManager          *auth.JWTManager
	userStore           *auth.UserStore
	apiKeyStore         *auth.APIKeyStore
	auditLogger         audit.Logger                 // Interface for audit logging (in-memory or persistent)
	inMemoryAuditLogger *audit.AuditLogger           // In-memory logger for GetEvents/GetRecentEvents
	persistentAudit     *audit.PersistentAuditLogger // Persistent logger (nil if disabled)
	maskingPolicyStore  *masking.PolicyStore         // F3: per-tenant masking policies (in-memory; lost on restart)
	masker              *masking.Masker              // F3: shared Masker (holds token cache across requests)
	metricsRegistry     *metrics.Registry
	healthChecker       *health.HealthChecker
	tlsConfig           *tlspkg.Config
	corsConfig          *CORSConfig                 // CORS configuration for cross-origin requests
	rateLimiter         *RateLimiter                // Rate limiter for API requests
	authRateLimiter     *RateLimiter                // Stricter rate limiter for auth endpoints (brute-force prevention)
	encryptionEngine    encryption.EncryptDecrypter // Handles data encryption/decryption
	keyManager          encryption.KeyProvider      // Manages encryption keys
	oidcHandler         *oidc.OIDCHandler           // OIDC authentication handler (nil if disabled)
	oidcConfig          *oidc.Config                // OIDC configuration
	tokenValidator      auth.TokenValidator         // Composite validator for JWT + OIDC
	tenantStore         *tenant.TenantStore         // Multi-tenant store (nil if single-tenant mode)
	startTime           time.Time
	version             string
	port                int
	dataDir             string         // Data directory for auth persistence
	environment         string         // "live" or "test" - for API key environment enforcement
	metricsStopCh       chan struct{}  // Stop channel for metrics goroutine
	metricsWg           sync.WaitGroup // WaitGroup for metrics goroutine
}
