package encryption

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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
