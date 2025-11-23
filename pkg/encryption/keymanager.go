package encryption

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	ErrKeyNotFound        = fmt.Errorf("key not found")
	ErrInvalidKeyVersion  = fmt.Errorf("invalid key version")
	ErrKeyAlreadyExists   = fmt.Errorf("key already exists")
	ErrNoActiveKey        = fmt.Errorf("no active key version")
)

// KeyMetadata contains metadata about an encryption key
type KeyMetadata struct {
	Version     uint32    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	ActivatedAt time.Time `json:"activated_at,omitempty"`
	RotatedAt   time.Time `json:"rotated_at,omitempty"`
	Status      KeyStatus `json:"status"`
	Algorithm   string    `json:"algorithm"`
	Purpose     string    `json:"purpose"` // "KEK" or "DEK"
}

// KeyStatus represents the status of a key
type KeyStatus string

const (
	KeyStatusActive     KeyStatus = "active"      // Currently in use for new encryption
	KeyStatusRotated    KeyStatus = "rotated"     // Rotated out, but still used for decryption
	KeyStatusDeprecated KeyStatus = "deprecated"  // Scheduled for removal
	KeyStatusRevoked    KeyStatus = "revoked"     // Should not be used
)

// KeyEntry represents a stored key with its metadata
type KeyEntry struct {
	Metadata      KeyMetadata `json:"metadata"`
	EncryptedKey  []byte      `json:"encrypted_key"` // KEK encrypted with MEK
}

// KeyManager manages encryption keys, including rotation and versioning
type KeyManager struct {
	masterEngine *Engine              // Engine with master key for encrypting KEKs
	keys         map[uint32]*KeyEntry // version -> key entry
	activeVersion uint32              // Current active key version
	keyDir       string               // Directory for key storage
	mu           sync.RWMutex         // Protects keys map and activeVersion
}

// KeyManagerConfig holds configuration for the key manager
type KeyManagerConfig struct {
	KeyDir       string // Directory to store key metadata
	MasterKey    []byte // Master encryption key (MEK)
	AutoRotate   bool   // Enable automatic key rotation
	RotateAfter  time.Duration // Rotate keys after this duration
}

// NewKeyManager creates a new key manager
func NewKeyManager(config KeyManagerConfig) (*KeyManager, error) {
	// Create master engine
	masterEngine, err := NewEngine(config.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create master engine: %w", err)
	}

	// Create key directory if it doesn't exist
	if err := os.MkdirAll(config.KeyDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key directory: %w", err)
	}

	km := &KeyManager{
		masterEngine: masterEngine,
		keys:         make(map[uint32]*KeyEntry),
		keyDir:       config.KeyDir,
	}

	// Load existing keys from disk
	if err := km.loadKeys(); err != nil {
		return nil, fmt.Errorf("failed to load keys: %w", err)
	}

	return km, nil
}

// GenerateKEK generates a new Key Encryption Key
func (km *KeyManager) GenerateKEK() (uint32, error) {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Generate new KEK
	kek, err := GenerateKey()
	if err != nil {
		return 0, fmt.Errorf("failed to generate KEK: %w", err)
	}

	// Encrypt KEK with master key
	encryptedKEK, err := km.masterEngine.Encrypt(kek)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt KEK: %w", err)
	}

	// Determine version number
	version := km.getNextVersion()

	// Create key entry
	now := time.Now()
	entry := &KeyEntry{
		Metadata: KeyMetadata{
			Version:     version,
			CreatedAt:   now,
			ActivatedAt: now,
			Status:      KeyStatusActive,
			Algorithm:   "AES-256-GCM",
			Purpose:     "KEK",
		},
		EncryptedKey: encryptedKEK,
	}

	// Deactivate previous active key if exists
	if km.activeVersion != 0 {
		if oldKey, exists := km.keys[km.activeVersion]; exists {
			oldKey.Metadata.Status = KeyStatusRotated
			oldKey.Metadata.RotatedAt = now
		}
	}

	// Store new key
	km.keys[version] = entry
	km.activeVersion = version

	// Persist to disk
	if err := km.saveKey(entry); err != nil {
		return 0, fmt.Errorf("failed to save key: %w", err)
	}

	// Zero out the KEK
	for i := range kek {
		kek[i] = 0
	}

	return version, nil
}

// GetKEK retrieves and decrypts a KEK by version
func (km *KeyManager) GetKEK(version uint32) ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	entry, exists := km.keys[version]
	if !exists {
		return nil, ErrKeyNotFound
	}

	// Check if key is revoked
	if entry.Metadata.Status == KeyStatusRevoked {
		return nil, fmt.Errorf("key version %d is revoked", version)
	}

	// Decrypt KEK
	kek, err := km.masterEngine.Decrypt(entry.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt KEK: %w", err)
	}

	return kek, nil
}

// GetActiveKEK retrieves the currently active KEK
func (km *KeyManager) GetActiveKEK() ([]byte, uint32, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.activeVersion == 0 {
		return nil, 0, ErrNoActiveKey
	}

	entry, exists := km.keys[km.activeVersion]
	if !exists {
		return nil, 0, ErrKeyNotFound
	}

	// Decrypt KEK
	kek, err := km.masterEngine.Decrypt(entry.EncryptedKey)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to decrypt KEK: %w", err)
	}

	return kek, km.activeVersion, nil
}

// GetActiveVersion returns the current active key version
func (km *KeyManager) GetActiveVersion() uint32 {
	km.mu.RLock()
	defer km.mu.RUnlock()
	return km.activeVersion
}

// ListKeys returns metadata for all keys
func (km *KeyManager) ListKeys() []KeyMetadata {
	km.mu.RLock()
	defer km.mu.RUnlock()

	result := make([]KeyMetadata, 0, len(km.keys))
	for _, entry := range km.keys {
		result = append(result, entry.Metadata)
	}

	return result
}

// RotateKey creates a new KEK and marks the old one as rotated
func (km *KeyManager) RotateKey() (uint32, error) {
	// This is essentially the same as GenerateKEK
	// The old key is automatically marked as rotated
	return km.GenerateKEK()
}

// RevokeKey marks a key as revoked (should not be used)
func (km *KeyManager) RevokeKey(version uint32) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	entry, exists := km.keys[version]
	if !exists {
		return ErrKeyNotFound
	}

	// Cannot revoke the active key
	if version == km.activeVersion {
		return fmt.Errorf("cannot revoke active key, rotate first")
	}

	entry.Metadata.Status = KeyStatusRevoked

	// Persist to disk
	if err := km.saveKey(entry); err != nil {
		return fmt.Errorf("failed to save key: %w", err)
	}

	return nil
}

// DeprecateKey marks a key as deprecated (warning, will be removed)
func (km *KeyManager) DeprecateKey(version uint32) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	entry, exists := km.keys[version]
	if !exists {
		return ErrKeyNotFound
	}

	// Cannot deprecate the active key
	if version == km.activeVersion {
		return fmt.Errorf("cannot deprecate active key, rotate first")
	}

	entry.Metadata.Status = KeyStatusDeprecated

	// Persist to disk
	if err := km.saveKey(entry); err != nil {
		return fmt.Errorf("failed to save key: %w", err)
	}

	return nil
}

// GetKeyMetadata returns metadata for a specific key version
func (km *KeyManager) GetKeyMetadata(version uint32) (*KeyMetadata, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	entry, exists := km.keys[version]
	if !exists {
		return nil, ErrKeyNotFound
	}

	metadata := entry.Metadata
	return &metadata, nil
}

// GetKeyAge returns the age of a key version
func (km *KeyManager) GetKeyAge(version uint32) (time.Duration, error) {
	metadata, err := km.GetKeyMetadata(version)
	if err != nil {
		return 0, err
	}

	return time.Since(metadata.CreatedAt), nil
}

// ShouldRotate checks if the active key should be rotated based on age
func (km *KeyManager) ShouldRotate(maxAge time.Duration) bool {
	km.mu.RLock()
	defer km.mu.RUnlock()

	if km.activeVersion == 0 {
		return true // No active key, should generate one
	}

	entry, exists := km.keys[km.activeVersion]
	if !exists {
		return true
	}

	age := time.Since(entry.Metadata.CreatedAt)
	return age > maxAge
}

// CleanupDeprecatedKeys removes deprecated keys older than the specified age
func (km *KeyManager) CleanupDeprecatedKeys(olderThan time.Duration) error {
	km.mu.Lock()
	defer km.mu.Unlock()

	now := time.Now()
	for version, entry := range km.keys {
		// Skip active key
		if version == km.activeVersion {
			continue
		}

		// Only cleanup deprecated or revoked keys
		if entry.Metadata.Status == KeyStatusDeprecated || entry.Metadata.Status == KeyStatusRevoked {
			age := now.Sub(entry.Metadata.CreatedAt)
			if age > olderThan {
				// Remove from memory
				delete(km.keys, version)

				// Remove from disk
				keyPath := km.getKeyPath(version)
				os.Remove(keyPath)
			}
		}
	}

	return nil
}

// Internal helper methods

func (km *KeyManager) getNextVersion() uint32 {
	maxVersion := uint32(0)
	for version := range km.keys {
		if version > maxVersion {
			maxVersion = version
		}
	}
	return maxVersion + 1
}

func (km *KeyManager) getKeyPath(version uint32) string {
	return filepath.Join(km.keyDir, fmt.Sprintf("key_v%d.json", version))
}

func (km *KeyManager) saveKey(entry *KeyEntry) error {
	keyPath := km.getKeyPath(entry.Metadata.Version)

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key entry: %w", err)
	}

	// Write with restrictive permissions
	if err := os.WriteFile(keyPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	return nil
}

func (km *KeyManager) loadKeys() error {
	// List all key files
	entries, err := os.ReadDir(km.keyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist yet, no keys to load
		}
		return fmt.Errorf("failed to read key directory: %w", err)
	}

	maxActiveVersion := uint32(0)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process JSON files
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		keyPath := filepath.Join(km.keyDir, entry.Name())

		// Read key file
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("failed to read key file %s: %w", keyPath, err)
		}

		// Parse key entry
		var keyEntry KeyEntry
		if err := json.Unmarshal(data, &keyEntry); err != nil {
			return fmt.Errorf("failed to unmarshal key file %s: %w", keyPath, err)
		}

		// Store in memory
		km.keys[keyEntry.Metadata.Version] = &keyEntry

		// Track the highest active version
		if keyEntry.Metadata.Status == KeyStatusActive && keyEntry.Metadata.Version > maxActiveVersion {
			maxActiveVersion = keyEntry.Metadata.Version
		}
	}

	// Set active version
	km.activeVersion = maxActiveVersion

	return nil
}

// Close securely closes the key manager
func (km *KeyManager) Close() error {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Zero out all decrypted keys in memory (if any were cached)
	// Currently we don't cache decrypted keys, but this is for future use
	km.keys = nil

	return nil
}

// ExportKeyMetadata exports key metadata for auditing (without actual keys)
func (km *KeyManager) ExportKeyMetadata() ([]byte, error) {
	km.mu.RLock()
	defer km.mu.RUnlock()

	metadata := make([]KeyMetadata, 0, len(km.keys))
	for _, entry := range km.keys {
		metadata = append(metadata, entry.Metadata)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return data, nil
}

// GetStatistics returns statistics about the key manager
func (km *KeyManager) GetStatistics() KeyManagerStatistics {
	km.mu.RLock()
	defer km.mu.RUnlock()

	stats := KeyManagerStatistics{
		TotalKeys:     len(km.keys),
		ActiveVersion: km.activeVersion,
	}

	for _, entry := range km.keys {
		switch entry.Metadata.Status {
		case KeyStatusActive:
			stats.ActiveKeys++
		case KeyStatusRotated:
			stats.RotatedKeys++
		case KeyStatusDeprecated:
			stats.DeprecatedKeys++
		case KeyStatusRevoked:
			stats.RevokedKeys++
		}

		age := time.Since(entry.Metadata.CreatedAt)
		if stats.OldestKeyAge == 0 || age > stats.OldestKeyAge {
			stats.OldestKeyAge = age
		}
		if stats.NewestKeyAge == 0 || age < stats.NewestKeyAge {
			stats.NewestKeyAge = age
		}
	}

	if stats.ActiveKeys > 0 {
		if activeEntry, exists := km.keys[km.activeVersion]; exists {
			stats.ActiveKeyAge = time.Since(activeEntry.Metadata.CreatedAt)
		}
	}

	return stats
}

// KeyManagerStatistics holds statistics about the key manager
type KeyManagerStatistics struct {
	TotalKeys       int           `json:"total_keys"`
	ActiveKeys      int           `json:"active_keys"`
	RotatedKeys     int           `json:"rotated_keys"`
	DeprecatedKeys  int           `json:"deprecated_keys"`
	RevokedKeys     int           `json:"revoked_keys"`
	ActiveVersion   uint32        `json:"active_version"`
	ActiveKeyAge    time.Duration `json:"active_key_age"`
	OldestKeyAge    time.Duration `json:"oldest_key_age"`
	NewestKeyAge    time.Duration `json:"newest_key_age"`
}
