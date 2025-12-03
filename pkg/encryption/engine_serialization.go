package encryption

import (
	"encoding/binary"
	"fmt"
)

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
