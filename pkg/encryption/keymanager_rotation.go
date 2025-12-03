package encryption

import (
	"fmt"
	"os"
	"time"
)

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
