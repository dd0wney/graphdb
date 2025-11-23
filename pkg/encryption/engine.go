package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// Encryption constants
	KeySize       = 32 // AES-256
	NonceSize     = 12 // GCM standard nonce size
	TagSize       = 16 // GCM authentication tag size
	SaltSize      = 32 // Salt for PBKDF2
	BlockSize     = 65536 // 64KB blocks
	PBKDF2Iterations = 600000 // OWASP recommended minimum

	// File header constants
	MagicNumber   = "GDBE0001" // GraphDB Encryption v0001
	HeaderSize    = 64
	DEKBlockSize  = 256
	FileVersion   = 1
)

var (
	ErrInvalidKey        = fmt.Errorf("invalid encryption key")
	ErrInvalidCiphertext = fmt.Errorf("invalid ciphertext")
	ErrAuthenticationFailed = fmt.Errorf("authentication failed - data may be tampered")
	ErrInvalidHeader     = fmt.Errorf("invalid file header")
	ErrUnsupportedVersion = fmt.Errorf("unsupported encryption version")
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
	Nonce        [NonceSize]byte  // Nonce for DEK encryption
	EncryptedDEK [KeySize]byte    // Encrypted data encryption key
	Tag          [TagSize]byte    // Authentication tag
	Reserved     [196]byte        // Reserved for future use
}

// DataBlock represents an encrypted data block
type DataBlock struct {
	Nonce [NonceSize]byte // Unique nonce for this block
	Data  []byte          // Encrypted data + authentication tag
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

// EncryptBlock encrypts a data block with a given DEK
func (e *Engine) EncryptBlock(plaintext []byte, dek []byte) (*DataBlock, error) {
	if len(dek) != KeySize {
		return nil, ErrInvalidKey
	}

	// Create AES cipher
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	var nonce [NonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate
	ciphertext := gcm.Seal(nil, nonce[:], plaintext, nil)

	return &DataBlock{
		Nonce: nonce,
		Data:  ciphertext,
	}, nil
}

// DecryptBlock decrypts a data block with a given DEK
func (e *Engine) DecryptBlock(block *DataBlock, dek []byte) ([]byte, error) {
	if len(dek) != KeySize {
		return nil, ErrInvalidKey
	}

	// Create AES cipher
	aesCipher, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode
	gcm, err := cipher.NewGCM(aesCipher)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt and verify
	plaintext, err := gcm.Open(nil, block.Nonce[:], block.Data, nil)
	if err != nil {
		return nil, ErrAuthenticationFailed
	}

	return plaintext, nil
}

// CreateFileHeader creates a new encrypted file header
func CreateFileHeader(keyVersion uint32) *FileHeader {
	header := &FileHeader{
		Version:    FileVersion,
		Algorithm:  1, // AES-256-GCM
		KeyVersion: keyVersion,
	}
	copy(header.Magic[:], MagicNumber)
	return header
}

// ValidateFileHeader validates an encrypted file header
func ValidateFileHeader(header *FileHeader) error {
	if string(header.Magic[:]) != MagicNumber {
		return ErrInvalidHeader
	}
	if header.Version != FileVersion {
		return ErrUnsupportedVersion
	}
	return nil
}

// MarshalFileHeader serializes a file header to bytes
func MarshalFileHeader(header *FileHeader) []byte {
	buf := make([]byte, HeaderSize)
	copy(buf[0:8], header.Magic[:])
	binary.LittleEndian.PutUint32(buf[8:12], header.Version)
	binary.LittleEndian.PutUint32(buf[12:16], header.Algorithm)
	binary.LittleEndian.PutUint32(buf[16:20], header.KeyVersion)
	copy(buf[20:64], header.Reserved[:])
	return buf
}

// UnmarshalFileHeader deserializes a file header from bytes
func UnmarshalFileHeader(buf []byte) (*FileHeader, error) {
	if len(buf) < HeaderSize {
		return nil, ErrInvalidHeader
	}

	header := &FileHeader{
		Version:    binary.LittleEndian.Uint32(buf[8:12]),
		Algorithm:  binary.LittleEndian.Uint32(buf[12:16]),
		KeyVersion: binary.LittleEndian.Uint32(buf[16:20]),
	}
	copy(header.Magic[:], buf[0:8])
	copy(header.Reserved[:], buf[20:64])

	return header, ValidateFileHeader(header)
}

// EncryptDEK encrypts a data encryption key with the master key
func (e *Engine) EncryptDEK(dek []byte) (*DEKBlock, error) {
	if len(dek) != KeySize {
		return nil, ErrInvalidKey
	}

	encrypted, err := e.Encrypt(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt DEK: %w", err)
	}

	dekBlock := &DEKBlock{}
	copy(dekBlock.Nonce[:], encrypted[:NonceSize])
	copy(dekBlock.EncryptedDEK[:], encrypted[NonceSize:NonceSize+KeySize])
	copy(dekBlock.Tag[:], encrypted[NonceSize+KeySize:])

	return dekBlock, nil
}

// DecryptDEK decrypts a data encryption key with the master key
func (e *Engine) DecryptDEK(dekBlock *DEKBlock) ([]byte, error) {
	// Reconstruct encrypted DEK format
	encrypted := make([]byte, NonceSize+KeySize+TagSize)
	copy(encrypted[:NonceSize], dekBlock.Nonce[:])
	copy(encrypted[NonceSize:NonceSize+KeySize], dekBlock.EncryptedDEK[:])
	copy(encrypted[NonceSize+KeySize:], dekBlock.Tag[:])

	dek, err := e.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
	}

	return dek, nil
}

// MarshalDEKBlock serializes a DEK block to bytes
func MarshalDEKBlock(dekBlock *DEKBlock) []byte {
	buf := make([]byte, DEKBlockSize)
	copy(buf[0:NonceSize], dekBlock.Nonce[:])
	copy(buf[NonceSize:NonceSize+KeySize], dekBlock.EncryptedDEK[:])
	copy(buf[NonceSize+KeySize:NonceSize+KeySize+TagSize], dekBlock.Tag[:])
	copy(buf[NonceSize+KeySize+TagSize:], dekBlock.Reserved[:])
	return buf
}

// UnmarshalDEKBlock deserializes a DEK block from bytes
func UnmarshalDEKBlock(buf []byte) (*DEKBlock, error) {
	if len(buf) < DEKBlockSize {
		return nil, fmt.Errorf("invalid DEK block size")
	}

	dekBlock := &DEKBlock{}
	copy(dekBlock.Nonce[:], buf[0:NonceSize])
	copy(dekBlock.EncryptedDEK[:], buf[NonceSize:NonceSize+KeySize])
	copy(dekBlock.Tag[:], buf[NonceSize+KeySize:NonceSize+KeySize+TagSize])
	copy(dekBlock.Reserved[:], buf[NonceSize+KeySize+TagSize:DEKBlockSize])

	return dekBlock, nil
}

// StreamEncryptor provides streaming encryption for large files
type StreamEncryptor struct {
	engine *Engine
	dek    []byte
	writer io.Writer
}

// NewStreamEncryptor creates a new streaming encryptor
func (e *Engine) NewStreamEncryptor(w io.Writer, keyVersion uint32) (*StreamEncryptor, error) {
	// Generate random DEK
	dek, err := GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate DEK: %w", err)
	}

	// Write file header
	header := CreateFileHeader(keyVersion)
	headerBytes := MarshalFileHeader(header)
	if _, err := w.Write(headerBytes); err != nil {
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	// Encrypt and write DEK block
	dekBlock, err := e.EncryptDEK(dek)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt DEK: %w", err)
	}
	dekBytes := MarshalDEKBlock(dekBlock)
	if _, err := w.Write(dekBytes); err != nil {
		return nil, fmt.Errorf("failed to write DEK block: %w", err)
	}

	return &StreamEncryptor{
		engine: e,
		dek:    dek,
		writer: w,
	}, nil
}

// WriteBlock encrypts and writes a data block
func (se *StreamEncryptor) WriteBlock(plaintext []byte) error {
	block, err := se.engine.EncryptBlock(plaintext, se.dek)
	if err != nil {
		return fmt.Errorf("failed to encrypt block: %w", err)
	}

	// Write block size (4 bytes)
	sizeBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(sizeBytes, uint32(len(block.Data)))
	if _, err := se.writer.Write(sizeBytes); err != nil {
		return fmt.Errorf("failed to write block size: %w", err)
	}

	// Write nonce
	if _, err := se.writer.Write(block.Nonce[:]); err != nil {
		return fmt.Errorf("failed to write nonce: %w", err)
	}

	// Write encrypted data (ciphertext + tag)
	if _, err := se.writer.Write(block.Data); err != nil {
		return fmt.Errorf("failed to write encrypted data: %w", err)
	}

	return nil
}

// Close closes the stream encryptor (currently a no-op, for future use)
func (se *StreamEncryptor) Close() error {
	// Securely zero out the DEK
	for i := range se.dek {
		se.dek[i] = 0
	}
	return nil
}

// StreamDecryptor provides streaming decryption for large files
type StreamDecryptor struct {
	engine *Engine
	dek    []byte
	reader io.Reader
	header *FileHeader
}

// NewStreamDecryptor creates a new streaming decryptor
func (e *Engine) NewStreamDecryptor(r io.Reader) (*StreamDecryptor, error) {
	// Read and parse file header
	headerBytes := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, headerBytes); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	header, err := UnmarshalFileHeader(headerBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid header: %w", err)
	}

	// Read and decrypt DEK block
	dekBytes := make([]byte, DEKBlockSize)
	if _, err := io.ReadFull(r, dekBytes); err != nil {
		return nil, fmt.Errorf("failed to read DEK block: %w", err)
	}

	dekBlock, err := UnmarshalDEKBlock(dekBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid DEK block: %w", err)
	}

	dek, err := e.DecryptDEK(dekBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt DEK: %w", err)
	}

	return &StreamDecryptor{
		engine: e,
		dek:    dek,
		reader: r,
		header: header,
	}, nil
}

// ReadBlock reads and decrypts the next data block
func (sd *StreamDecryptor) ReadBlock(maxSize int) ([]byte, error) {
	// Read block size (4 bytes)
	sizeBytes := make([]byte, 4)
	if _, err := io.ReadFull(sd.reader, sizeBytes); err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read block size: %w", err)
	}
	blockSize := binary.LittleEndian.Uint32(sizeBytes)

	// Read nonce
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(sd.reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("failed to read nonce: %w", err)
	}

	// Read encrypted data (exact size from header)
	encryptedData := make([]byte, blockSize)
	if _, err := io.ReadFull(sd.reader, encryptedData); err != nil {
		return nil, fmt.Errorf("failed to read encrypted data: %w", err)
	}

	// Decrypt block
	block := &DataBlock{
		Nonce: nonce,
		Data:  encryptedData,
	}

	plaintext, err := sd.engine.DecryptBlock(block, sd.dek)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt block: %w", err)
	}

	return plaintext, nil
}

// GetHeader returns the file header
func (sd *StreamDecryptor) GetHeader() *FileHeader {
	return sd.header
}

// Close closes the stream decryptor
func (sd *StreamDecryptor) Close() error {
	// Securely zero out the DEK
	for i := range sd.dek {
		sd.dek[i] = 0
	}
	return nil
}
