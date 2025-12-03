package auth

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrAPIKeyNotFound       = errors.New("API key not found")
	ErrAPIKeyExpired        = errors.New("API key has expired")
	ErrAPIKeyRevoked        = errors.New("API key has been revoked")
	ErrAPIKeyWrongEnv       = errors.New("API key environment mismatch")
	ErrInvalidPermission    = errors.New("invalid permission")
	ErrEmptyKeyName         = errors.New("key name cannot be empty")
	ErrEmptyPermissions     = errors.New("permissions cannot be empty")
)

// Valid API key permissions
var validPermissions = map[string]bool{
	"read":  true,
	"write": true,
	"admin": true,
}

// ValidateKey validates an API key and returns the associated APIKey metadata
func (s *APIKeyStore) ValidateKey(keyString string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if keyString == "" {
		return nil, ErrInvalidToken
	}

	// Extract prefix to narrow search
	if !strings.HasPrefix(keyString, "gdb_") {
		return nil, ErrInvalidToken
	}

	// Hash the provided key using HMAC
	keyHash := s.hashAPIKey(keyString)

	// Look up key by hash
	keyID, exists := s.hashToKey[keyHash]
	if !exists {
		return nil, ErrAPIKeyNotFound
	}

	apiKey, exists := s.keys[keyID]
	if !exists {
		return nil, ErrAPIKeyNotFound
	}

	// Check if revoked
	if apiKey.Revoked {
		return nil, ErrAPIKeyRevoked
	}

	// Check expiration
	if !apiKey.ExpiresAt.IsZero() && time.Now().After(apiKey.ExpiresAt) {
		return nil, ErrAPIKeyExpired
	}

	return apiKey, nil
}

// ValidateKeyForEnv validates an API key and checks it matches the required environment.
// serverEnv should be "live" or "test" - typically derived from GRAPHDB_ENV.
// If serverEnv is empty, no environment check is performed (backwards compatible).
func (s *APIKeyStore) ValidateKeyForEnv(keyString, serverEnv string) (*APIKey, error) {
	// First do standard validation
	apiKey, err := s.ValidateKey(keyString)
	if err != nil {
		return nil, err
	}

	// If server environment is specified, enforce it
	if serverEnv != "" && apiKey.Environment != "" {
		if apiKey.Environment != serverEnv {
			return nil, ErrAPIKeyWrongEnv
		}
	}

	return apiKey, nil
}

// HasPermission checks if an API key has a specific permission
func (s *APIKeyStore) HasPermission(apiKey *APIKey, permission string) bool {
	if apiKey == nil {
		return false
	}

	for _, perm := range apiKey.Permissions {
		if perm == permission || perm == "admin" {
			return true
		}
	}

	return false
}

// validateCreateKeyInput validates the inputs for CreateKey
func validateCreateKeyInput(userID, name string, permissions []string) error {
	if userID == "" {
		return ErrEmptyUserID
	}
	if name == "" {
		return ErrEmptyKeyName
	}
	if len(permissions) == 0 {
		return ErrEmptyPermissions
	}

	// Validate permissions
	for _, perm := range permissions {
		if !validPermissions[perm] {
			return ErrInvalidPermission
		}
	}

	return nil
}
