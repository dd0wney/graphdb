package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// Engine provides AES-256-GCM encryption and decryption
type Engine struct {
	masterKey []byte // Master encryption key (MEK)
}

// NewEngine creates a new encryption engine with the given master key
func NewEngine(masterKey []byte) (*Engine, error) {
	if len(masterKey) != KeySize {
		return nil, ErrInvalidKey
	}

	// Make a copy to avoid external mutations
	key := make([]byte, KeySize)
	copy(key, masterKey)

	return &Engine{
		masterKey: key,
	}, nil
}

// NewEngineFromPassphrase creates an engine with a key derived from a passphrase
func NewEngineFromPassphrase(passphrase string, salt []byte) (*Engine, error) {
	if len(salt) != SaltSize {
		return nil, fmt.Errorf("salt must be %d bytes", SaltSize)
	}

	// Derive key using PBKDF2
	key := pbkdf2.Key([]byte(passphrase), salt, PBKDF2Iterations, KeySize, sha256.New)

	return NewEngine(key)
}

// GenerateSalt generates a cryptographically secure random salt
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}
	return salt, nil
}

// GenerateKey generates a cryptographically secure random encryption key
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with the engine's master key
// Returns: nonce + ciphertext + tag concatenated
func (e *Engine) Encrypt(plaintext []byte) ([]byte, error) {
	return e.EncryptWithKey(plaintext, e.masterKey)
}

// EncryptWithKey encrypts plaintext with a specific key
// Returns: nonce + ciphertext + tag concatenated
func (e *Engine) EncryptWithKey(plaintext []byte, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Return nonce + ciphertext+tag
	result := make([]byte, NonceSize+len(ciphertext))
	copy(result[:NonceSize], nonce)
	copy(result[NonceSize:], ciphertext)

	return result, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM with the engine's master key
// Input format: nonce + ciphertext + tag concatenated
func (e *Engine) Decrypt(ciphertext []byte) ([]byte, error) {
	return e.DecryptWithKey(ciphertext, e.masterKey)
}

// DecryptWithKey decrypts ciphertext with a specific key
// Input format: nonce + ciphertext + tag concatenated
func (e *Engine) DecryptWithKey(ciphertext []byte, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}

	if len(ciphertext) < NonceSize+TagSize {
		return nil, ErrInvalidCiphertext
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce and ciphertext+tag
	nonce := ciphertext[:NonceSize]
	ct := ciphertext[NonceSize:]

	// Decrypt and verify
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}

	return plaintext, nil
}
