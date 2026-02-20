package auth

import (
	"os"
	"strings"
	"testing"
)

// TestGenerateAPIKey tests the basic generateAPIKey function
func TestGenerateAPIKey(t *testing.T) {
	// Ensure we're in test mode (not production)
	originalEnv := os.Getenv("GRAPHDB_ENV")
	os.Setenv("GRAPHDB_ENV", "test")
	defer os.Setenv("GRAPHDB_ENV", originalEnv)

	key, prefix, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generateAPIKey() error: %v", err)
	}

	if key == "" {
		t.Error("Expected non-empty key")
	}

	if prefix == "" {
		t.Error("Expected non-empty prefix")
	}

	// Key should start with the prefix
	if !strings.HasPrefix(key, prefix) {
		t.Errorf("Key %q should start with prefix %q", key, prefix)
	}

	// In test mode, should use test prefix
	if prefix != KeyPrefixTest {
		t.Errorf("Expected prefix %q in test mode, got %q", KeyPrefixTest, prefix)
	}
}

// TestGenerateAPIKeyWithEnv tests explicit environment key generation
func TestGenerateAPIKeyWithEnv(t *testing.T) {
	tests := []struct {
		name           string
		env            string
		expectedPrefix string
	}{
		{
			name:           "explicit live environment",
			env:            "live",
			expectedPrefix: KeyPrefixProduction,
		},
		{
			name:           "explicit test environment",
			env:            "test",
			expectedPrefix: KeyPrefixTest,
		},
		{
			name:           "auto-detect test (default)",
			env:            "",
			expectedPrefix: KeyPrefixTest, // Default when GRAPHDB_ENV != "production"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment for auto-detect case
			if tt.env == "" {
				originalEnv := os.Getenv("GRAPHDB_ENV")
				os.Setenv("GRAPHDB_ENV", "development")
				defer os.Setenv("GRAPHDB_ENV", originalEnv)
			}

			key, prefix, err := generateAPIKeyWithEnv(tt.env)
			if err != nil {
				t.Fatalf("generateAPIKeyWithEnv(%q) error: %v", tt.env, err)
			}

			if prefix != tt.expectedPrefix {
				t.Errorf("Expected prefix %q, got %q", tt.expectedPrefix, prefix)
			}

			if !strings.HasPrefix(key, tt.expectedPrefix) {
				t.Errorf("Key %q should start with prefix %q", key, tt.expectedPrefix)
			}
		})
	}
}

// TestGenerateAPIKeyWithEnv_Production tests production environment detection
func TestGenerateAPIKeyWithEnv_Production(t *testing.T) {
	// Set environment to production
	originalEnv := os.Getenv("GRAPHDB_ENV")
	os.Setenv("GRAPHDB_ENV", "production")
	defer os.Setenv("GRAPHDB_ENV", originalEnv)

	// With empty env arg, should auto-detect production
	key, prefix, err := generateAPIKeyWithEnv("")
	if err != nil {
		t.Fatalf("generateAPIKeyWithEnv() error: %v", err)
	}

	if prefix != KeyPrefixProduction {
		t.Errorf("Expected prefix %q in production, got %q", KeyPrefixProduction, prefix)
	}

	if !strings.HasPrefix(key, KeyPrefixProduction) {
		t.Errorf("Key should start with production prefix")
	}
}

// TestGenerateAPIKey_Uniqueness tests that generated keys are unique
func TestGenerateAPIKey_Uniqueness(t *testing.T) {
	keys := make(map[string]bool)

	// Generate multiple keys
	for i := 0; i < 100; i++ {
		key, _, err := generateAPIKey()
		if err != nil {
			t.Fatalf("generateAPIKey() error on iteration %d: %v", i, err)
		}

		if keys[key] {
			t.Errorf("Duplicate key generated: %s", key)
		}
		keys[key] = true
	}
}

// TestGenerateAPIKey_Length tests that generated keys have sufficient length
func TestGenerateAPIKey_Length(t *testing.T) {
	key, _, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generateAPIKey() error: %v", err)
	}

	// Key should be: prefix (9 chars for gdb_test_) + base64 encoded 32 random bytes (43 chars)
	// Total: at least 50 characters
	minLength := 50
	if len(key) < minLength {
		t.Errorf("Key length %d is less than minimum %d", len(key), minLength)
	}
}

// TestAPIKeyStore_HashAndCompare tests hash generation and comparison
func TestAPIKeyStore_HashAndCompare(t *testing.T) {
	store := NewAPIKeyStore()

	testKey := "gdb_test_abcdefghijklmnopqrstuvwxyz123456"

	// Hash should be deterministic
	hash1 := store.hashAPIKey(testKey)
	hash2 := store.hashAPIKey(testKey)

	if hash1 != hash2 {
		t.Error("hashAPIKey should produce same hash for same input")
	}

	// Hash should be hex encoded
	if len(hash1) != 64 { // SHA-256 produces 32 bytes = 64 hex chars
		t.Errorf("Expected hash length 64, got %d", len(hash1))
	}

	// compareKeyHash should return true for matching key
	if !store.compareKeyHash(testKey, hash1) {
		t.Error("compareKeyHash should return true for matching key")
	}

	// compareKeyHash should return false for wrong key
	wrongKey := "gdb_test_wrongkey1234567890"
	if store.compareKeyHash(wrongKey, hash1) {
		t.Error("compareKeyHash should return false for wrong key")
	}

	// compareKeyHash should return false for wrong hash
	if store.compareKeyHash(testKey, "wronghash") {
		t.Error("compareKeyHash should return false for wrong hash")
	}
}

// TestAPIKeyStore_DifferentKeysProduceDifferentHashes tests hash uniqueness
func TestAPIKeyStore_DifferentKeysProduceDifferentHashes(t *testing.T) {
	store := NewAPIKeyStore()

	key1 := "gdb_test_key1_abcdefghijklmnopqrstuvwxyz"
	key2 := "gdb_test_key2_abcdefghijklmnopqrstuvwxyz"

	hash1 := store.hashAPIKey(key1)
	hash2 := store.hashAPIKey(key2)

	if hash1 == hash2 {
		t.Error("Different keys should produce different hashes")
	}
}

// TestGenerateID tests unique ID generation
func TestGenerateID(t *testing.T) {
	ids := make(map[string]bool)

	// Generate multiple IDs and check uniqueness
	for i := 0; i < 100; i++ {
		id := generateID()

		if id == "" {
			t.Errorf("generateID() returned empty string on iteration %d", i)
		}

		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

// TestGenerateID_Length tests that generated IDs have expected length
func TestGenerateID_Length(t *testing.T) {
	id := generateID()

	// ID is base64 URL encoded 16 random bytes = 22 characters (no padding)
	expectedLength := 22
	if len(id) != expectedLength {
		t.Errorf("Expected ID length %d, got %d", expectedLength, len(id))
	}
}

// TestAPIKeyStore_HMACSecret tests that different stores have different secrets
func TestAPIKeyStore_HMACSecret(t *testing.T) {
	store1 := NewAPIKeyStore()
	store2 := NewAPIKeyStore()

	testKey := "gdb_test_abcdefghijklmnopqrstuvwxyz123456"

	hash1 := store1.hashAPIKey(testKey)
	hash2 := store2.hashAPIKey(testKey)

	// Different stores should have different HMAC secrets, producing different hashes
	if hash1 == hash2 {
		t.Error("Different stores should produce different hashes (different HMAC secrets)")
	}
}
