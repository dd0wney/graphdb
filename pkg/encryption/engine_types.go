package encryption

import "fmt"

const (
	// Encryption constants
	KeySize          = 32     // AES-256
	NonceSize        = 12     // GCM standard nonce size
	TagSize          = 16     // GCM authentication tag size
	SaltSize         = 32     // Salt for PBKDF2
	BlockSize        = 65536  // 64KB blocks
	PBKDF2Iterations = 600000 // OWASP recommended minimum

	// File header constants
	MagicNumber  = "GDBE0001" // GraphDB Encryption v0001
	HeaderSize   = 64
	DEKBlockSize = 256
	FileVersion  = 1
)

var (
	ErrInvalidKey           = fmt.Errorf("invalid encryption key")
	ErrInvalidCiphertext    = fmt.Errorf("invalid ciphertext")
	ErrAuthenticationFailed = fmt.Errorf("authentication failed - data may be tampered")
	ErrInvalidHeader        = fmt.Errorf("invalid file header")
	ErrUnsupportedVersion   = fmt.Errorf("unsupported encryption version")
)

// FileHeader represents the encrypted file header
type FileHeader struct {
	Magic      [8]byte  // Magic number
	Version    uint32   // File format version
	Algorithm  uint32   // Encryption algorithm identifier
	KeyVersion uint32   // Key version for rotation
	Reserved   [44]byte // Reserved for future use
}

// DEKBlock represents the encrypted data encryption key block
type DEKBlock struct {
	Nonce        [NonceSize]byte // Nonce for DEK encryption
	EncryptedDEK [KeySize]byte   // Encrypted data encryption key
	Tag          [TagSize]byte   // Authentication tag
	Reserved     [196]byte       // Reserved for future use
}

// DataBlock represents an encrypted data block
type DataBlock struct {
	Nonce [NonceSize]byte // Unique nonce for this block
	Data  []byte          // Encrypted data + authentication tag
}
