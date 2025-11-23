package encryption

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	if len(key1) != KeySize {
		t.Errorf("Key length = %d, want %d", len(key1), KeySize)
	}

	// Generate another key and ensure they're different
	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}

	if bytes.Equal(key1, key2) {
		t.Error("Generated keys are identical (should be random)")
	}
}

func TestGenerateSalt(t *testing.T) {
	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt() failed: %v", err)
	}

	if len(salt1) != SaltSize {
		t.Errorf("Salt length = %d, want %d", len(salt1), SaltSize)
	}

	// Generate another salt and ensure they're different
	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt() failed: %v", err)
	}

	if bytes.Equal(salt1, salt2) {
		t.Error("Generated salts are identical (should be random)")
	}
}

func TestNewEngine(t *testing.T) {
	key, _ := GenerateKey()

	engine, err := NewEngine(key)
	if err != nil {
		t.Fatalf("NewEngine() failed: %v", err)
	}

	if engine == nil {
		t.Fatal("NewEngine() returned nil")
	}

	// Test with invalid key size
	invalidKey := make([]byte, 16) // Too short
	_, err = NewEngine(invalidKey)
	if err != ErrInvalidKey {
		t.Errorf("NewEngine(invalid key) error = %v, want %v", err, ErrInvalidKey)
	}
}

func TestNewEngineFromPassphrase(t *testing.T) {
	salt, _ := GenerateSalt()
	passphrase := "test-passphrase-12345"

	engine, err := NewEngineFromPassphrase(passphrase, salt)
	if err != nil {
		t.Fatalf("NewEngineFromPassphrase() failed: %v", err)
	}

	if engine == nil {
		t.Fatal("NewEngineFromPassphrase() returned nil")
	}

	// Same passphrase and salt should produce same key
	engine2, err := NewEngineFromPassphrase(passphrase, salt)
	if err != nil {
		t.Fatalf("NewEngineFromPassphrase() failed: %v", err)
	}

	// Test by encrypting/decrypting with both engines
	plaintext := []byte("test data")
	ciphertext, _ := engine.Encrypt(plaintext)
	decrypted, err := engine2.Decrypt(ciphertext)
	if err != nil {
		t.Errorf("Decrypt with second engine failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Engines from same passphrase produced different keys")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)

	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"Empty", []byte{}},
		{"Small", []byte("Hello, World!")},
		{"Medium", bytes.Repeat([]byte("A"), 1024)},
		{"Large", bytes.Repeat([]byte("B"), 65536)},
		{"Binary", []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			ciphertext, err := engine.Encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt() failed: %v", err)
			}

			// Ciphertext should be larger (nonce + tag)
			expectedSize := len(tt.plaintext) + NonceSize + TagSize
			if len(ciphertext) != expectedSize {
				t.Errorf("Ciphertext size = %d, want %d", len(ciphertext), expectedSize)
			}

			// Decrypt
			decrypted, err := engine.Decrypt(ciphertext)
			if err != nil {
				t.Fatalf("Decrypt() failed: %v", err)
			}

			// Verify plaintext matches
			if !bytes.Equal(tt.plaintext, decrypted) {
				t.Errorf("Decrypted text doesn't match original")
			}
		})
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)

	plaintext := []byte("test data")

	// Encrypt same plaintext twice
	ciphertext1, _ := engine.Encrypt(plaintext)
	ciphertext2, _ := engine.Encrypt(plaintext)

	// Ciphertexts should be different (due to random nonce)
	if bytes.Equal(ciphertext1, ciphertext2) {
		t.Error("Same plaintext produced identical ciphertext (nonces not random)")
	}

	// But both should decrypt to same plaintext
	decrypted1, _ := engine.Decrypt(ciphertext1)
	decrypted2, _ := engine.Decrypt(ciphertext2)

	if !bytes.Equal(decrypted1, plaintext) || !bytes.Equal(decrypted2, plaintext) {
		t.Error("Decryption failed for non-deterministic encryption")
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	engine1, _ := NewEngine(key1)
	engine2, _ := NewEngine(key2)

	plaintext := []byte("secret message")
	ciphertext, _ := engine1.Encrypt(plaintext)

	// Try to decrypt with wrong key
	_, err := engine2.Decrypt(ciphertext)
	if err != ErrAuthenticationFailed {
		t.Errorf("Decrypt with wrong key error = %v, want %v", err, ErrAuthenticationFailed)
	}
}

func TestDecryptTamperedCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)

	plaintext := []byte("important data")
	ciphertext, _ := engine.Encrypt(plaintext)

	// Tamper with ciphertext (flip a bit)
	ciphertext[len(ciphertext)/2] ^= 0x01

	// Decryption should fail due to authentication failure
	_, err := engine.Decrypt(ciphertext)
	if err != ErrAuthenticationFailed {
		t.Errorf("Decrypt tampered data error = %v, want %v", err, ErrAuthenticationFailed)
	}
}

func TestDecryptInvalidCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)

	// Too short ciphertext
	shortCiphertext := make([]byte, NonceSize+TagSize-1)
	_, err := engine.Decrypt(shortCiphertext)
	if err != ErrInvalidCiphertext {
		t.Errorf("Decrypt short ciphertext error = %v, want %v", err, ErrInvalidCiphertext)
	}
}

func TestFileHeader(t *testing.T) {
	header := CreateFileHeader(1)

	if string(header.Magic[:]) != MagicNumber {
		t.Errorf("Magic = %s, want %s", header.Magic, MagicNumber)
	}

	if header.Version != FileVersion {
		t.Errorf("Version = %d, want %d", header.Version, FileVersion)
	}

	if header.KeyVersion != 1 {
		t.Errorf("KeyVersion = %d, want 1", header.KeyVersion)
	}

	// Test marshaling/unmarshaling
	marshaled := MarshalFileHeader(header)
	if len(marshaled) != HeaderSize {
		t.Errorf("Marshaled size = %d, want %d", len(marshaled), HeaderSize)
	}

	unmarshaled, err := UnmarshalFileHeader(marshaled)
	if err != nil {
		t.Fatalf("UnmarshalFileHeader() failed: %v", err)
	}

	if string(unmarshaled.Magic[:]) != string(header.Magic[:]) ||
		unmarshaled.Version != header.Version ||
		unmarshaled.KeyVersion != header.KeyVersion {
		t.Error("Unmarshaled header doesn't match original")
	}
}

func TestInvalidFileHeader(t *testing.T) {
	// Invalid magic number
	invalidHeader := CreateFileHeader(1)
	copy(invalidHeader.Magic[:], "INVALID!")
	marshaled := MarshalFileHeader(invalidHeader)

	_, err := UnmarshalFileHeader(marshaled)
	if err != ErrInvalidHeader {
		t.Errorf("Invalid magic error = %v, want %v", err, ErrInvalidHeader)
	}

	// Unsupported version
	futureHeader := CreateFileHeader(1)
	futureHeader.Version = 999
	marshaled = MarshalFileHeader(futureHeader)

	_, err = UnmarshalFileHeader(marshaled)
	if err != ErrUnsupportedVersion {
		t.Errorf("Future version error = %v, want %v", err, ErrUnsupportedVersion)
	}
}

func TestDEKEncryption(t *testing.T) {
	masterKey, _ := GenerateKey()
	engine, _ := NewEngine(masterKey)

	dek, _ := GenerateKey()

	// Encrypt DEK
	dekBlock, err := engine.EncryptDEK(dek)
	if err != nil {
		t.Fatalf("EncryptDEK() failed: %v", err)
	}

	// Decrypt DEK
	decryptedDEK, err := engine.DecryptDEK(dekBlock)
	if err != nil {
		t.Fatalf("DecryptDEK() failed: %v", err)
	}

	if !bytes.Equal(dek, decryptedDEK) {
		t.Error("Decrypted DEK doesn't match original")
	}
}

func TestDEKBlockMarshalUnmarshal(t *testing.T) {
	masterKey, _ := GenerateKey()
	engine, _ := NewEngine(masterKey)
	dek, _ := GenerateKey()

	dekBlock, _ := engine.EncryptDEK(dek)

	// Marshal
	marshaled := MarshalDEKBlock(dekBlock)
	if len(marshaled) != DEKBlockSize {
		t.Errorf("Marshaled DEK block size = %d, want %d", len(marshaled), DEKBlockSize)
	}

	// Unmarshal
	unmarshaled, err := UnmarshalDEKBlock(marshaled)
	if err != nil {
		t.Fatalf("UnmarshalDEKBlock() failed: %v", err)
	}

	// Decrypt unmarshaled DEK block
	decryptedDEK, err := engine.DecryptDEK(unmarshaled)
	if err != nil {
		t.Fatalf("DecryptDEK() failed: %v", err)
	}

	if !bytes.Equal(dek, decryptedDEK) {
		t.Error("DEK after marshal/unmarshal doesn't match original")
	}
}

func TestEncryptDecryptBlock(t *testing.T) {
	masterKey, _ := GenerateKey()
	engine, _ := NewEngine(masterKey)
	dek, _ := GenerateKey()

	plaintext := []byte("block data to encrypt")

	// Encrypt block
	encryptedBlock, err := engine.EncryptBlock(plaintext, dek)
	if err != nil {
		t.Fatalf("EncryptBlock() failed: %v", err)
	}

	// Decrypt block
	decrypted, err := engine.DecryptBlock(encryptedBlock, dek)
	if err != nil {
		t.Fatalf("DecryptBlock() failed: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Error("Decrypted block doesn't match original")
	}
}

func TestStreamEncryptorDecryptor(t *testing.T) {
	masterKey, _ := GenerateKey()
	engine, _ := NewEngine(masterKey)

	// Create a buffer to simulate a file
	var buf bytes.Buffer

	// Create stream encryptor
	encryptor, err := engine.NewStreamEncryptor(&buf, 1)
	if err != nil {
		t.Fatalf("NewStreamEncryptor() failed: %v", err)
	}

	// Write multiple blocks
	testBlocks := [][]byte{
		[]byte("First block of data"),
		[]byte("Second block of data"),
		bytes.Repeat([]byte("X"), 1024), // Larger block
		[]byte("Final block"),
	}

	for _, block := range testBlocks {
		if err := encryptor.WriteBlock(block); err != nil {
			t.Fatalf("WriteBlock() failed: %v", err)
		}
	}

	encryptor.Close()

	// Create stream decryptor
	reader := bytes.NewReader(buf.Bytes())
	decryptor, err := engine.NewStreamDecryptor(reader)
	if err != nil {
		t.Fatalf("NewStreamDecryptor() failed: %v", err)
	}
	defer decryptor.Close()

	// Read and verify blocks
	for i, expectedBlock := range testBlocks {
		decryptedBlock, err := decryptor.ReadBlock(BlockSize)
		if err != nil {
			t.Fatalf("ReadBlock() for block %d failed: %v", i, err)
		}

		if !bytes.Equal(expectedBlock, decryptedBlock) {
			t.Errorf("Block %d doesn't match (got %d bytes, want %d bytes)",
				i, len(decryptedBlock), len(expectedBlock))
		}
	}

	// Verify EOF
	_, err = decryptor.ReadBlock(BlockSize)
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}

	// Verify header
	header := decryptor.GetHeader()
	if header.KeyVersion != 1 {
		t.Errorf("Header key version = %d, want 1", header.KeyVersion)
	}
}

func TestStreamEncryptorWithLargeData(t *testing.T) {
	masterKey, _ := GenerateKey()
	engine, _ := NewEngine(masterKey)

	var buf bytes.Buffer

	encryptor, _ := engine.NewStreamEncryptor(&buf, 1)

	// Write many blocks of varying sizes
	for i := 0; i < 100; i++ {
		blockSize := 1024 + (i * 100) // Varying sizes
		block := make([]byte, blockSize)
		rand.Read(block)

		if err := encryptor.WriteBlock(block); err != nil {
			t.Fatalf("WriteBlock() %d failed: %v", i, err)
		}
	}

	encryptor.Close()

	// Verify we can decrypt
	reader := bytes.NewReader(buf.Bytes())
	decryptor, _ := engine.NewStreamDecryptor(reader)
	defer decryptor.Close()

	blocksRead := 0
	for {
		_, err := decryptor.ReadBlock(BlockSize)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadBlock() failed: %v", err)
		}
		blocksRead++
	}

	if blocksRead != 100 {
		t.Errorf("Read %d blocks, want 100", blocksRead)
	}
}

func TestConcurrentEncryption(t *testing.T) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)

	plaintext := []byte("concurrent test data")

	// Run multiple encryptions concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				ciphertext, err := engine.Encrypt(plaintext)
				if err != nil {
					t.Errorf("Concurrent Encrypt() failed: %v", err)
					done <- false
					return
				}

				decrypted, err := engine.Decrypt(ciphertext)
				if err != nil {
					t.Errorf("Concurrent Decrypt() failed: %v", err)
					done <- false
					return
				}

				if !bytes.Equal(plaintext, decrypted) {
					t.Error("Concurrent encryption/decryption mismatch")
					done <- false
					return
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func BenchmarkEncrypt_Small(b *testing.B) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)
	plaintext := []byte("Hello, World!")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Encrypt(plaintext)
	}
}

func BenchmarkEncrypt_1KB(b *testing.B) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)
	plaintext := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Encrypt(plaintext)
	}
}

func BenchmarkEncrypt_64KB(b *testing.B) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)
	plaintext := make([]byte, 65536)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Encrypt(plaintext)
	}
}

func BenchmarkDecrypt_Small(b *testing.B) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)
	plaintext := []byte("Hello, World!")
	ciphertext, _ := engine.Encrypt(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Decrypt(ciphertext)
	}
}

func BenchmarkDecrypt_64KB(b *testing.B) {
	key, _ := GenerateKey()
	engine, _ := NewEngine(key)
	plaintext := make([]byte, 65536)
	ciphertext, _ := engine.Encrypt(plaintext)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Decrypt(ciphertext)
	}
}

func BenchmarkPBKDF2(b *testing.B) {
	salt, _ := GenerateSalt()
	passphrase := "test-passphrase-12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewEngineFromPassphrase(passphrase, salt)
	}
}
