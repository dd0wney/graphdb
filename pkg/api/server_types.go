package api

import (
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/graphql"
	"github.com/dd0wney/cluso-graphdb/pkg/health"
	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
	"github.com/dd0wney/cluso-graphdb/pkg/query"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
)

// Server represents the HTTP API server
type Server struct {
	graph               *storage.GraphStorage
	executor            *query.Executor
	graphqlHandler      *graphql.GraphQLHandler
	authHandler         *auth.AuthHandler
	userHandler         *auth.UserManagementHandler
	jwtManager          *auth.JWTManager
	userStore           *auth.UserStore
	apiKeyStore         *auth.APIKeyStore
	auditLogger         audit.Logger                  // Interface for audit logging (in-memory or persistent)
	inMemoryAuditLogger *audit.AuditLogger            // In-memory logger for GetEvents/GetRecentEvents
	persistentAudit     *audit.PersistentAuditLogger  // Persistent logger (nil if disabled)
	metricsRegistry     *metrics.Registry
	healthChecker       *health.HealthChecker
	tlsConfig           *tlspkg.Config
	corsConfig          *CORSConfig                 // CORS configuration for cross-origin requests
	rateLimiter         *RateLimiter                // Rate limiter for API requests
	authRateLimiter     *RateLimiter                // Stricter rate limiter for auth endpoints (brute-force prevention)
	encryptionEngine    encryption.EncryptDecrypter // Handles data encryption/decryption
	keyManager          encryption.KeyProvider      // Manages encryption keys
	startTime           time.Time
	version             string
	port                int
	dataDir             string                      // Data directory for auth persistence
	environment         string                      // "live" or "test" - for API key environment enforcement
}
