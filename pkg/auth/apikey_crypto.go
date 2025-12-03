package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"os"
)

const (
	KeyPrefixProduction = "gdb_live_"
	KeyPrefixTest       = "gdb_test_"
	KeyRandomLength     = 32 // bytes of random data
)

// generateAPIKey generates a new API key string and returns the key and its prefix.
// Uses gdb_test_ prefix when GRAPHDB_ENV != "production", otherwise gdb_live_.
func generateAPIKey() (string, string, error) {
	return generateAPIKeyWithEnv("")
}

// generateAPIKeyWithEnv generates a new API key with explicit environment.
// env can be "live", "test", or "" (auto-detect from GRAPHDB_ENV).
func generateAPIKeyWithEnv(env string) (string, string, error) {
	// Generate random bytes
	randomBytes := make([]byte, KeyRandomLength)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", "", err
	}

	// Encode to base64 (URL-safe)
	randomPart := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Determine prefix
	var prefix string
	switch env {
	case "live":
		prefix = KeyPrefixProduction
	case "test":
		prefix = KeyPrefixTest
	default:
		// Auto-detect from environment
		prefix = KeyPrefixTest
		if os.Getenv("GRAPHDB_ENV") == "production" {
			prefix = KeyPrefixProduction
		}
	}
	keyString := prefix + randomPart

	return keyString, prefix, nil
}

// hashAPIKey creates an HMAC-SHA256 hash of the key using the store's secret.
// HMAC provides better security than plain SHA-256:
// - Requires knowledge of the server secret to compute valid hashes
// - Prevents offline rainbow table attacks even if database is compromised
func (s *APIKeyStore) hashAPIKey(keyString string) string {
	mac := hmac.New(sha256.New, s.hmacSecret)
	mac.Write([]byte(keyString))
	return hex.EncodeToString(mac.Sum(nil))
}

// compareKeyHash compares a key against a stored hash in constant time
// to prevent timing attacks during validation
func (s *APIKeyStore) compareKeyHash(keyString, storedHash string) bool {
	computedHash := s.hashAPIKey(keyString)
	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) == 1
}

// generateID generates a unique ID for key metadata
func generateID() string {
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	return base64.RawURLEncoding.EncodeToString(randomBytes)
}
