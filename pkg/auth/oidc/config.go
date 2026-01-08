package oidc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds OIDC provider configuration
type Config struct {
	// Enabled determines if OIDC authentication is active
	Enabled bool `json:"enabled"`

	// Issuer is the OIDC provider URL (e.g., "https://accounts.google.com")
	Issuer string `json:"issuer"`

	// ClientID is the OAuth2 client ID
	ClientID string `json:"client_id"`

	// ClientSecret is the OAuth2 client secret (required for authorization code flow)
	ClientSecret string `json:"client_secret"`

	// RedirectURI is the callback URL (auto-detected if empty)
	RedirectURI string `json:"redirect_uri"`

	// Scopes to request from the provider (default: openid, profile, email)
	Scopes []string `json:"scopes"`

	// RoleMappings define how to map OIDC claims to GraphDB roles
	RoleMappings []RoleMapping `json:"role_mappings"`

	// DefaultRole is assigned when no role mapping matches (default: viewer)
	DefaultRole string `json:"default_role"`

	// JWKSCacheTTL is how long to cache JWKS (default: 1 hour)
	JWKSCacheTTL time.Duration `json:"jwks_cache_ttl"`

	// AllowedAudiences are additional valid audience values
	AllowedAudiences []string `json:"allowed_audiences"`
}

// Default configuration values
const (
	DefaultJWKSCacheTTL = time.Hour
	DefaultRole         = "viewer"
)

// DefaultScopes returns the default OIDC scopes
func DefaultScopes() []string {
	return []string{"openid", "profile", "email"}
}

// Valid GraphDB roles
var validRoles = map[string]bool{
	"admin":  true,
	"editor": true,
	"viewer": true,
}

// LoadConfigFromEnv loads OIDC configuration from environment variables
func LoadConfigFromEnv() (*Config, error) {
	config := &Config{
		Enabled:      os.Getenv("OIDC_ENABLED") == "true",
		Issuer:       os.Getenv("OIDC_ISSUER"),
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		RedirectURI:  os.Getenv("OIDC_REDIRECT_URI"),
		DefaultRole:  getEnvOrDefault("OIDC_DEFAULT_ROLE", DefaultRole),
		JWKSCacheTTL: parseDuration(os.Getenv("OIDC_JWKS_CACHE_TTL"), DefaultJWKSCacheTTL),
	}

	// Parse scopes (comma-separated)
	if scopes := os.Getenv("OIDC_SCOPES"); scopes != "" {
		config.Scopes = splitAndTrim(scopes, ",")
	} else {
		config.Scopes = DefaultScopes()
	}

	// Parse allowed audiences (comma-separated)
	if audiences := os.Getenv("OIDC_ALLOWED_AUDIENCES"); audiences != "" {
		config.AllowedAudiences = splitAndTrim(audiences, ",")
	}

	// Parse role mappings from JSON env var
	if mappings := os.Getenv("OIDC_ROLE_MAPPINGS"); mappings != "" {
		if err := json.Unmarshal([]byte(mappings), &config.RoleMappings); err != nil {
			return nil, fmt.Errorf("invalid OIDC_ROLE_MAPPINGS JSON: %w", err)
		}
	}

	// Only validate if OIDC is enabled
	if config.Enabled {
		if err := config.Validate(); err != nil {
			return nil, err
		}
	}

	return config, nil
}

// Validate checks that all required configuration is present
func (c *Config) Validate() error {
	if c.Issuer == "" {
		return errors.New("OIDC_ISSUER is required when OIDC is enabled")
	}

	if c.ClientID == "" {
		return errors.New("OIDC_CLIENT_ID is required when OIDC is enabled")
	}

	// ClientSecret is optional for public clients, but warn if missing
	// (required for authorization code flow with confidential clients)

	if !validRoles[c.DefaultRole] {
		return fmt.Errorf("invalid OIDC_DEFAULT_ROLE %q: must be admin, editor, or viewer", c.DefaultRole)
	}

	// Validate role mappings
	for i, mapping := range c.RoleMappings {
		if mapping.ClaimName == "" {
			return fmt.Errorf("role_mapping[%d]: claim_name is required", i)
		}
		if len(mapping.ClaimValues) == 0 {
			return fmt.Errorf("role_mapping[%d]: claim_value must have at least one value", i)
		}
		if !validRoles[mapping.GraphDBRole] {
			return fmt.Errorf("role_mapping[%d]: invalid graphdb_role %q", i, mapping.GraphDBRole)
		}
	}

	if c.JWKSCacheTTL < time.Minute {
		return errors.New("OIDC_JWKS_CACHE_TTL must be at least 1 minute")
	}

	return nil
}

// GetAllowedAudiences returns all valid audience values (ClientID + configured audiences)
func (c *Config) GetAllowedAudiences() []string {
	audiences := []string{c.ClientID}
	audiences = append(audiences, c.AllowedAudiences...)
	return audiences
}

// getEnvOrDefault returns the environment variable value or a default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseDuration parses a duration string with a default value
func parseDuration(s string, defaultValue time.Duration) time.Duration {
	if s == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultValue
	}
	return d
}

// splitAndTrim splits a string and trims whitespace from each part
func splitAndTrim(s string, sep string) []string {
	parts := strings.Split(s, sep)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
