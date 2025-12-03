package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Persistence file names
const (
	usersFileName   = "users.json"
	apiKeysFileName = "apikeys.json"
)

// persistedUser includes the password hash for persistence
type persistedUser struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
}

// persistedAPIKey includes the key hash for persistence
type persistedAPIKey struct {
	ID          string   `json:"id"`
	UserID      string   `json:"user_id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Environment string   `json:"environment"` // "live" or "test"
	KeyHash     string   `json:"key_hash"`
	Prefix      string   `json:"prefix"`
	CreatedAt   int64    `json:"created_at"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
	LastUsed    int64    `json:"last_used,omitempty"`
	Revoked     bool     `json:"revoked"`
}

// apiKeyStoreData represents the full API key store state for persistence
type apiKeyStoreData struct {
	HMACSecret []byte             `json:"hmac_secret"`
	Keys       []*persistedAPIKey `json:"keys"`
}

// SaveUsers persists all users to disk
func (s *UserStore) SaveUsers(dataDir string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create directory if needed
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Convert to persistable format
	users := make([]*persistedUser, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, &persistedUser{
			ID:           user.ID,
			Username:     user.Username,
			PasswordHash: user.PasswordHash,
			Role:         user.Role,
			CreatedAt:    user.CreatedAt,
		})
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(users, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal users: %w", err)
	}

	// Write to file with restrictive permissions (contains password hashes)
	filePath := filepath.Join(dataDir, usersFileName)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write users file: %w", err)
	}

	return nil
}

// LoadUsers loads users from disk
func (s *UserStore) LoadUsers(dataDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(dataDir, usersFileName)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// No file yet, nothing to load
		return nil
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read users file: %w", err)
	}

	// Unmarshal
	var users []*persistedUser
	if err := json.Unmarshal(data, &users); err != nil {
		return fmt.Errorf("failed to unmarshal users: %w", err)
	}

	// Load into store
	for _, pu := range users {
		user := &User{
			ID:           pu.ID,
			Username:     pu.Username,
			PasswordHash: pu.PasswordHash,
			Role:         pu.Role,
			CreatedAt:    pu.CreatedAt,
		}
		s.users[user.ID] = user
		s.usernameMap[user.Username] = user.ID
	}

	return nil
}

// SaveAPIKeys persists all API keys to disk
func (s *APIKeyStore) SaveAPIKeys(dataDir string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create directory if needed
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Convert to persistable format
	keys := make([]*persistedAPIKey, 0, len(s.keys))
	for _, key := range s.keys {
		pk := &persistedAPIKey{
			ID:          key.ID,
			UserID:      key.UserID,
			Name:        key.Name,
			Permissions: key.Permissions,
			Environment: key.Environment,
			KeyHash:     key.KeyHash,
			Prefix:      key.Prefix,
			CreatedAt:   key.CreatedAt.Unix(),
			Revoked:     key.Revoked,
		}
		if !key.ExpiresAt.IsZero() {
			pk.ExpiresAt = key.ExpiresAt.Unix()
		}
		if !key.LastUsed.IsZero() {
			pk.LastUsed = key.LastUsed.Unix()
		}
		keys = append(keys, pk)
	}

	// Include HMAC secret for key validation
	storeData := apiKeyStoreData{
		HMACSecret: s.hmacSecret,
		Keys:       keys,
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(storeData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}

	// Write to file with restrictive permissions (contains secrets)
	filePath := filepath.Join(dataDir, apiKeysFileName)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write API keys file: %w", err)
	}

	return nil
}

// LoadAPIKeys loads API keys from disk
func (s *APIKeyStore) LoadAPIKeys(dataDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath := filepath.Join(dataDir, apiKeysFileName)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// No file yet, nothing to load
		return nil
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read API keys file: %w", err)
	}

	// Unmarshal
	var storeData apiKeyStoreData
	if err := json.Unmarshal(data, &storeData); err != nil {
		return fmt.Errorf("failed to unmarshal API keys: %w", err)
	}

	// Restore HMAC secret (critical for key validation!)
	if len(storeData.HMACSecret) >= 32 {
		s.hmacSecret = storeData.HMACSecret
	}

	// Load keys into store
	for _, pk := range storeData.Keys {
		// Derive environment from prefix if not explicitly stored (backwards compat)
		env := pk.Environment
		if env == "" {
			if pk.Prefix == KeyPrefixProduction {
				env = "live"
			} else {
				env = "test"
			}
		}

		key := &APIKey{
			ID:          pk.ID,
			UserID:      pk.UserID,
			Name:        pk.Name,
			Permissions: pk.Permissions,
			Environment: env,
			KeyHash:     pk.KeyHash,
			Prefix:      pk.Prefix,
			Revoked:     pk.Revoked,
		}
		if pk.CreatedAt > 0 {
			key.CreatedAt = unixToTime(pk.CreatedAt)
		}
		if pk.ExpiresAt > 0 {
			key.ExpiresAt = unixToTime(pk.ExpiresAt)
		}
		if pk.LastUsed > 0 {
			key.LastUsed = unixToTime(pk.LastUsed)
		}

		// Restore all indexes
		s.keys[key.ID] = key
		s.hashToKey[key.KeyHash] = key.ID
		s.userKeys[key.UserID] = append(s.userKeys[key.UserID], key.ID)
	}

	return nil
}

// unixToTime converts Unix timestamp to time.Time
func unixToTime(unix int64) (t time.Time) {
	return time.Unix(unix, 0)
}
