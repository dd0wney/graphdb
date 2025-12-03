package auth

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// APIKey represents an API key with metadata
type APIKey struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	Environment string    `json:"environment"` // "live" or "test"
	KeyHash     string    `json:"-"`           // Never serialize key hash
	Prefix      string    `json:"prefix"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"` // Zero value means no expiration
	LastUsed    time.Time `json:"last_used,omitempty"`
	Revoked     bool      `json:"revoked"`
}

// APIKeyStore manages API keys
type APIKeyStore struct {
	keys       map[string]*APIKey  // keyID -> APIKey
	hashToKey  map[string]string   // keyHash -> keyID
	userKeys   map[string][]string // userID -> []keyID
	hmacSecret []byte              // Server-side secret for HMAC hashing
	mu         sync.RWMutex
}

// NewAPIKeyStore creates a new API key store with a random HMAC secret
func NewAPIKeyStore() *APIKeyStore {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		// Fall back to a zero secret if random fails (should not happen)
		secret = make([]byte, 32)
	}
	return &APIKeyStore{
		keys:       make(map[string]*APIKey),
		hashToKey:  make(map[string]string),
		userKeys:   make(map[string][]string),
		hmacSecret: secret,
	}
}

// NewAPIKeyStoreWithSecret creates a new API key store with a provided HMAC secret.
// Use this for persistence across restarts - the same secret must be used to validate existing keys.
func NewAPIKeyStoreWithSecret(secret []byte) (*APIKeyStore, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("HMAC secret must be at least 32 bytes")
	}
	return &APIKeyStore{
		keys:       make(map[string]*APIKey),
		hashToKey:  make(map[string]string),
		userKeys:   make(map[string][]string),
		hmacSecret: secret,
	}, nil
}

// CreateKey creates a new API key and returns the APIKey metadata and the actual key string
// The key string is only returned once and cannot be retrieved later
func (s *APIKeyStore) CreateKey(userID, name string, permissions []string, expiresIn time.Duration) (*APIKey, string, error) {
	return s.CreateKeyWithEnv(userID, name, permissions, expiresIn, "")
}

// CreateKeyWithEnv creates a new API key with explicit environment prefix.
// env can be "live", "test", or "" (auto-detect from GRAPHDB_ENV).
func (s *APIKeyStore) CreateKeyWithEnv(userID, name string, permissions []string, expiresIn time.Duration, env string) (*APIKey, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate inputs
	if err := validateCreateKeyInput(userID, name, permissions); err != nil {
		return nil, "", err
	}

	// Generate key with specified environment
	keyString, prefix, err := generateAPIKeyWithEnv(env)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate API key: %w", err)
	}

	// Hash key for storage using HMAC
	keyHash := s.hashAPIKey(keyString)

	// Create API key metadata
	now := time.Now()
	var expiresAt time.Time
	if expiresIn != 0 {
		expiresAt = now.Add(expiresIn)
	}

	// Determine environment from prefix
	keyEnv := "test"
	if prefix == KeyPrefixProduction {
		keyEnv = "live"
	}

	apiKey := &APIKey{
		ID:          generateID(),
		UserID:      userID,
		Name:        name,
		Permissions: permissions,
		Environment: keyEnv,
		KeyHash:     keyHash,
		Prefix:      prefix,
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
		Revoked:     false,
	}

	// Store key
	s.keys[apiKey.ID] = apiKey
	s.hashToKey[keyHash] = apiKey.ID

	// Track user's keys
	s.userKeys[userID] = append(s.userKeys[userID], apiKey.ID)

	return apiKey, keyString, nil
}

// ListKeys returns all API keys for a user
func (s *APIKeyStore) ListKeys(userID string) []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keyIDs := s.userKeys[userID]
	keys := make([]*APIKey, 0, len(keyIDs))

	for _, keyID := range keyIDs {
		if key, exists := s.keys[keyID]; exists {
			keys = append(keys, key)
		}
	}

	return keys
}

// GetKey retrieves a specific API key by ID
func (s *APIKeyStore) GetKey(keyID string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, exists := s.keys[keyID]
	if !exists {
		return nil, ErrAPIKeyNotFound
	}

	return key, nil
}

// RevokeKey revokes an API key
func (s *APIKeyStore) RevokeKey(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[keyID]
	if !exists {
		return ErrAPIKeyNotFound
	}

	key.Revoked = true
	return nil
}

// UpdateKeyName updates the name of an API key
func (s *APIKeyStore) UpdateKeyName(keyID, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newName == "" {
		return ErrEmptyKeyName
	}

	key, exists := s.keys[keyID]
	if !exists {
		return ErrAPIKeyNotFound
	}

	key.Name = newName
	return nil
}

// UpdateLastUsed updates the last used timestamp for a key
func (s *APIKeyStore) UpdateLastUsed(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, exists := s.keys[keyID]
	if !exists {
		return ErrAPIKeyNotFound
	}

	key.LastUsed = time.Now()
	return nil
}
