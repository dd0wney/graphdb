package encryption

import "time"

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
