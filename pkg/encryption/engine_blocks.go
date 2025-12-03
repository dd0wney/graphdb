package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

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
