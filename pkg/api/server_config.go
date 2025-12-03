package api

import (
	"log"
	"os"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	tlspkg "github.com/dd0wney/cluso-graphdb/pkg/tls"
)

// SetTLSConfig sets the TLS configuration for the server
func (s *Server) SetTLSConfig(cfg *tlspkg.Config) {
	s.tlsConfig = cfg
}

// SetCORSConfig sets the CORS configuration for the server
func (s *Server) SetCORSConfig(cfg *CORSConfig) {
	s.corsConfig = cfg
}

// InitCORSFromEnv initializes CORS configuration from environment variables
// CORS_ALLOWED_ORIGINS: comma-separated list of allowed origins (e.g., "https://app.example.com,https://admin.example.com")
// Use "*" to allow all origins (NOT recommended for production)
func (s *Server) InitCORSFromEnv() {
	originsEnv := os.Getenv("CORS_ALLOWED_ORIGINS")
	if originsEnv == "" {
		// No CORS configured - secure default (no cross-origin requests allowed)
		s.corsConfig = DefaultCORSConfig()
		log.Printf("ℹ️  CORS: No origins configured (CORS_ALLOWED_ORIGINS not set). Cross-origin requests disabled.")
		return
	}

	origins := strings.Split(originsEnv, ",")
	for i, o := range origins {
		origins[i] = strings.TrimSpace(o)
	}

	// Check for wildcard and warn
	for _, o := range origins {
		if o == "*" {
			log.Printf("⚠️  WARNING: CORS allows all origins (*). This is NOT recommended for production!")
			break
		}
	}

	s.corsConfig = &CORSConfig{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-API-Key", "X-Request-ID"},
		AllowCredentials: os.Getenv("CORS_ALLOW_CREDENTIALS") == "true",
		MaxAge:           86400, // 24 hours
	}

	log.Printf("✅ CORS configured with %d allowed origins", len(origins))
}

// SetEncryption sets the encryption engine and key manager for the server.
// Uses typed interfaces for compile-time safety.
func (s *Server) SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider) {
	s.encryptionEngine = engine
	s.keyManager = keyManager
}
