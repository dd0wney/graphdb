package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrAPIKeyNotFound     = errors.New("API key not found")
	ErrAPIKeyExpired      = errors.New("API key has expired")
	ErrAPIKeyRevoked      = errors.New("API key has been revoked")
	ErrInvalidPermission  = errors.New("invalid permission")
	ErrEmptyKeyName       = errors.New("key name cannot be empty")
	ErrEmptyPermissions   = errors.New("permissions cannot be empty")
)

const (
	KeyPrefixProduction = "gdb_live_"
	KeyPrefixTest       = "gdb_test_"
	KeyRandomLength     = 32 // bytes of random data
)

// Valid API key permissions
var validPermissions = map[string]bool{
	"read":  true,
	"write": true,
	"admin": true,
}

// APIKey represents an API key with metadata
type APIKey struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	KeyHash     string    `json:"-"` // Never serialize key hash
	Prefix      string    `json:"prefix"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"` // Zero value means no expiration
	LastUsed    time.Time `json:"last_used,omitempty"`
	Revoked     bool      `json:"revoked"`
}

// APIKeyStore manages API keys
type APIKeyStore struct {
	keys       map[string]*APIKey // keyID -> APIKey
	hashToKey  map[string]string  // keyHash -> keyID
	userKeys   map[string][]string // userID -> []keyID
	mu         sync.RWMutex
}

// NewAPIKeyStore creates a new API key store
func NewAPIKeyStore() *APIKeyStore {
	return &APIKeyStore{
		keys:      make(map[string]*APIKey),
		hashToKey: make(map[string]string),
		userKeys:  make(map[string][]string),
	}
}

// CreateKey creates a new API key and returns the APIKey metadata and the actual key string
// The key string is only returned once and cannot be retrieved later
func (s *APIKeyStore) CreateKey(userID, name string, permissions []string, expiresIn time.Duration) (*APIKey, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate inputs
	if userID == "" {
		return nil, "", ErrEmptyUserID
	}
	if name == "" {
		return nil, "", ErrEmptyKeyName
	}
	if len(permissions) == 0 {
		return nil, "", ErrEmptyPermissions
	}

	// Validate permissions
	for _, perm := range permissions {
		if !validPermissions[perm] {
			return nil, "", fmt.Errorf("%w: %s", ErrInvalidPermission, perm)
		}
	}

	// Generate key
	keyString, prefix, err := generateAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate API key: %w", err)
	}

	// Hash key for storage
	keyHash, err := hashAPIKey(keyString)
	if err != nil {
		return nil, "", fmt.Errorf("failed to hash API key: %w", err)
	}

	// Create API key metadata
	now := time.Now()
	var expiresAt time.Time
	if expiresIn != 0 {
		expiresAt = now.Add(expiresIn)
	}

	apiKey := &APIKey{
		ID:          generateID(),
		UserID:      userID,
		Name:        name,
		Permissions: permissions,
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

	// Hash the provided key
	keyHash, err := hashAPIKey(keyString)
	if err != nil {
		return nil, ErrInvalidToken
	}

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

// Helper functions

func generateAPIKey() (string, string, error) {
	// Generate random bytes
	randomBytes := make([]byte, KeyRandomLength)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", "", err
	}

	// Encode to base64 (URL-safe)
	randomPart := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Use production prefix by default
	// In production, you might want to make this configurable
	prefix := KeyPrefixProduction
	keyString := prefix + randomPart

	return keyString, prefix, nil
}

func hashAPIKey(keyString string) (string, error) {
	// Use SHA-256 for deterministic hashing (unlike bcrypt which uses salt)
	// This allows us to look up keys by their hash
	hash := sha256.Sum256([]byte(keyString))
	return hex.EncodeToString(hash[:]), nil
}

func generateID() string {
	// Generate a unique ID for the key metadata
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	return base64.RawURLEncoding.EncodeToString(randomBytes)
}
