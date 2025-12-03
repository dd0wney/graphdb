package encryption

import (
	"fmt"
	"sync"
	"time"
)

var (
	ErrKeyNotFound       = fmt.Errorf("key not found")
	ErrInvalidKeyVersion = fmt.Errorf("invalid key version")
	ErrKeyAlreadyExists  = fmt.Errorf("key already exists")
	ErrNoActiveKey       = fmt.Errorf("no active key version")
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
	KeyStatusActive     KeyStatus = "active"     // Currently in use for new encryption
	KeyStatusRotated    KeyStatus = "rotated"    // Rotated out, but still used for decryption
	KeyStatusDeprecated KeyStatus = "deprecated" // Scheduled for removal
	KeyStatusRevoked    KeyStatus = "revoked"    // Should not be used
)

// KeyEntry represents a stored key with its metadata
type KeyEntry struct {
	Metadata     KeyMetadata `json:"metadata"`
	EncryptedKey []byte      `json:"encrypted_key"` // KEK encrypted with MEK
}

// KeyManager manages encryption keys, including rotation and versioning
type KeyManager struct {
	masterEngine  *Engine              // Engine with master key for encrypting KEKs
	keys          map[uint32]*KeyEntry // version -> key entry
	activeVersion uint32               // Current active key version
	keyDir        string               // Directory for key storage
	mu            sync.RWMutex         // Protects keys map and activeVersion
}

// KeyManagerConfig holds configuration for the key manager
type KeyManagerConfig struct {
	KeyDir      string        // Directory to store key metadata
	MasterKey   []byte        // Master encryption key (MEK)
	AutoRotate  bool          // Enable automatic key rotation
	RotateAfter time.Duration // Rotate keys after this duration
}

// KeyManagerStatistics holds statistics about the key manager
type KeyManagerStatistics struct {
	TotalKeys      int           `json:"total_keys"`
	ActiveKeys     int           `json:"active_keys"`
	RotatedKeys    int           `json:"rotated_keys"`
	DeprecatedKeys int           `json:"deprecated_keys"`
	RevokedKeys    int           `json:"revoked_keys"`
	ActiveVersion  uint32        `json:"active_version"`
	ActiveKeyAge   time.Duration `json:"active_key_age"`
	OldestKeyAge   time.Duration `json:"oldest_key_age"`
	NewestKeyAge   time.Duration `json:"newest_key_age"`
}
