package encryption

import (
	"encoding/binary"
	"fmt"
	"io"
)

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
