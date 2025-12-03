package encryption

import (
	"fmt"
	"os"
	"time"
)

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

// Close securely closes the key manager
func (km *KeyManager) Close() error {
	km.mu.Lock()
	defer km.mu.Unlock()

	// Zero out all decrypted keys in memory (if any were cached)
	// Currently we don't cache decrypted keys, but this is for future use
	km.keys = nil

	return nil
}
