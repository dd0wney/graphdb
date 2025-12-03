package encryption

// Encrypter is the interface for encryption operations.
// This interface can be used by packages that need encryption without
// depending on the concrete Engine implementation.
type Encrypter interface {
	// Encrypt encrypts plaintext and returns ciphertext.
	// The returned ciphertext includes any necessary metadata (nonce, tag, etc.)
	Encrypt(plaintext []byte) ([]byte, error)
}

// Decrypter is the interface for decryption operations.
type Decrypter interface {
	// Decrypt decrypts ciphertext and returns plaintext.
	// Returns ErrAuthenticationFailed if the data has been tampered with.
	Decrypt(ciphertext []byte) ([]byte, error)
}

// EncryptDecrypter combines encryption and decryption capabilities.
// Use this interface when both operations are needed.
type EncryptDecrypter interface {
	Encrypter
	Decrypter
}

// KeyProvider is the interface for key management operations.
// This is a simplified interface for retrieving keys.
type KeyProvider interface {
	// GetActiveKEK retrieves the currently active Key Encryption Key.
	// Returns the key, its version number, and any error.
	GetActiveKEK() ([]byte, uint32, error)

	// GetKEK retrieves a Key Encryption Key by version number.
	// Used for decrypting data encrypted with older key versions.
	GetKEK(version uint32) ([]byte, error)

	// GetActiveVersion returns the current active key version number.
	GetActiveVersion() uint32
}

// Verify that Engine implements the interfaces
var _ Encrypter = (*Engine)(nil)
var _ Decrypter = (*Engine)(nil)
var _ EncryptDecrypter = (*Engine)(nil)

// Verify that KeyManager implements KeyProvider
var _ KeyProvider = (*KeyManager)(nil)
