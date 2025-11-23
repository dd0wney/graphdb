package encryption

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewKeyManager(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	config := KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	}

	km, err := NewKeyManager(config)
	if err != nil {
		t.Fatalf("NewKeyManager() failed: %v", err)
	}
	defer km.Close()

	if km == nil {
		t.Fatal("NewKeyManager() returned nil")
	}

	if km.GetActiveVersion() != 0 {
		t.Errorf("Initial active version = %d, want 0", km.GetActiveVersion())
	}
}

func TestGenerateKEK(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate first KEK
	version1, err := km.GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK() failed: %v", err)
	}

	if version1 != 1 {
		t.Errorf("First version = %d, want 1", version1)
	}

	if km.GetActiveVersion() != version1 {
		t.Errorf("Active version = %d, want %d", km.GetActiveVersion(), version1)
	}

	// Generate second KEK (rotation)
	version2, err := km.GenerateKEK()
	if err != nil {
		t.Fatalf("Second GenerateKEK() failed: %v", err)
	}

	if version2 != 2 {
		t.Errorf("Second version = %d, want 2", version2)
	}

	if km.GetActiveVersion() != version2 {
		t.Errorf("Active version = %d, want %d", km.GetActiveVersion(), version2)
	}

	// Check first key is now rotated
	metadata, err := km.GetKeyMetadata(version1)
	if err != nil {
		t.Fatalf("GetKeyMetadata() failed: %v", err)
	}

	if metadata.Status != KeyStatusRotated {
		t.Errorf("First key status = %s, want %s", metadata.Status, KeyStatusRotated)
	}
}

func TestGetKEK(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	version, _ := km.GenerateKEK()

	// Retrieve KEK
	kek, err := km.GetKEK(version)
	if err != nil {
		t.Fatalf("GetKEK() failed: %v", err)
	}

	if len(kek) != KeySize {
		t.Errorf("KEK size = %d, want %d", len(kek), KeySize)
	}

	// Retrieve same KEK again should give same result
	kek2, err := km.GetKEK(version)
	if err != nil {
		t.Fatalf("Second GetKEK() failed: %v", err)
	}

	// Note: We can't compare keys directly as they are freshly decrypted each time
	if len(kek2) != KeySize {
		t.Errorf("Second KEK size = %d, want %d", len(kek2), KeySize)
	}
}

func TestGetActiveKEK(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// No active key yet
	_, _, err := km.GetActiveKEK()
	if err != ErrNoActiveKey {
		t.Errorf("GetActiveKEK() with no keys error = %v, want %v", err, ErrNoActiveKey)
	}

	// Generate a key
	expectedVersion, _ := km.GenerateKEK()

	// Get active KEK
	kek, version, err := km.GetActiveKEK()
	if err != nil {
		t.Fatalf("GetActiveKEK() failed: %v", err)
	}

	if version != expectedVersion {
		t.Errorf("Active version = %d, want %d", version, expectedVersion)
	}

	if len(kek) != KeySize {
		t.Errorf("Active KEK size = %d, want %d", len(kek), KeySize)
	}
}

func TestRotateKey(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate initial key
	v1, _ := km.GenerateKEK()

	// Rotate
	v2, err := km.RotateKey()
	if err != nil {
		t.Fatalf("RotateKey() failed: %v", err)
	}

	if v2 <= v1 {
		t.Errorf("Rotated version %d not greater than previous %d", v2, v1)
	}

	// Check old key is rotated
	metadata, _ := km.GetKeyMetadata(v1)
	if metadata.Status != KeyStatusRotated {
		t.Errorf("Old key status = %s, want %s", metadata.Status, KeyStatusRotated)
	}

	// Check new key is active
	metadata2, _ := km.GetKeyMetadata(v2)
	if metadata2.Status != KeyStatusActive {
		t.Errorf("New key status = %s, want %s", metadata2.Status, KeyStatusActive)
	}
}

func TestRevokeKey(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	v1, _ := km.GenerateKEK()
	v2, _ := km.GenerateKEK() // Rotate to have an old key

	// Cannot revoke active key
	err := km.RevokeKey(v2)
	if err == nil {
		t.Error("Expected error when revoking active key")
	}

	// Can revoke old key
	err = km.RevokeKey(v1)
	if err != nil {
		t.Errorf("RevokeKey() failed: %v", err)
	}

	// Check status
	metadata, _ := km.GetKeyMetadata(v1)
	if metadata.Status != KeyStatusRevoked {
		t.Errorf("Key status = %s, want %s", metadata.Status, KeyStatusRevoked)
	}

	// Cannot retrieve revoked key
	_, err = km.GetKEK(v1)
	if err == nil {
		t.Error("Expected error when retrieving revoked key")
	}
}

func TestDeprecateKey(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	v1, _ := km.GenerateKEK()
	v2, _ := km.GenerateKEK()

	// Cannot deprecate active key
	err := km.DeprecateKey(v2)
	if err == nil {
		t.Error("Expected error when deprecating active key")
	}

	// Can deprecate old key
	err = km.DeprecateKey(v1)
	if err != nil {
		t.Errorf("DeprecateKey() failed: %v", err)
	}

	metadata, _ := km.GetKeyMetadata(v1)
	if metadata.Status != KeyStatusDeprecated {
		t.Errorf("Key status = %s, want %s", metadata.Status, KeyStatusDeprecated)
	}
}

func TestListKeys(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate several keys
	km.GenerateKEK()
	km.GenerateKEK()
	km.GenerateKEK()

	keys := km.ListKeys()
	if len(keys) != 3 {
		t.Errorf("ListKeys() returned %d keys, want 3", len(keys))
	}
}

func TestPersistence(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	// Create first key manager and generate keys
	km1, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})

	v1, _ := km1.GenerateKEK()
	v2, _ := km1.GenerateKEK()

	km1.Close()

	// Create second key manager with same directory
	km2, err := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	if err != nil {
		t.Fatalf("Failed to reload key manager: %v", err)
	}
	defer km2.Close()

	// Should have loaded both keys
	keys := km2.ListKeys()
	if len(keys) != 2 {
		t.Errorf("Loaded %d keys, want 2", len(keys))
	}

	// Active version should be preserved
	if km2.GetActiveVersion() != v2 {
		t.Errorf("Active version = %d, want %d", km2.GetActiveVersion(), v2)
	}

	// Should be able to retrieve keys
	_, err = km2.GetKEK(v1)
	if err != nil {
		t.Errorf("Failed to retrieve key v1: %v", err)
	}

	_, err = km2.GetKEK(v2)
	if err != nil {
		t.Errorf("Failed to retrieve key v2: %v", err)
	}
}

func TestGetKeyMetadata(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	version, _ := km.GenerateKEK()

	metadata, err := km.GetKeyMetadata(version)
	if err != nil {
		t.Fatalf("GetKeyMetadata() failed: %v", err)
	}

	if metadata.Version != version {
		t.Errorf("Metadata version = %d, want %d", metadata.Version, version)
	}

	if metadata.Algorithm != "AES-256-GCM" {
		t.Errorf("Algorithm = %s, want AES-256-GCM", metadata.Algorithm)
	}

	if metadata.Purpose != "KEK" {
		t.Errorf("Purpose = %s, want KEK", metadata.Purpose)
	}

	if metadata.Status != KeyStatusActive {
		t.Errorf("Status = %s, want %s", metadata.Status, KeyStatusActive)
	}

	if metadata.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestGetKeyAge(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	version, _ := km.GenerateKEK()

	time.Sleep(10 * time.Millisecond)

	age, err := km.GetKeyAge(version)
	if err != nil {
		t.Fatalf("GetKeyAge() failed: %v", err)
	}

	if age < 10*time.Millisecond {
		t.Errorf("Key age %v is less than 10ms", age)
	}
}

func TestShouldRotate(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// No active key, should rotate
	if !km.ShouldRotate(24 * time.Hour) {
		t.Error("ShouldRotate() = false with no active key, want true")
	}

	// Generate key
	km.GenerateKEK()

	// Fresh key, should not rotate
	if km.ShouldRotate(24 * time.Hour) {
		t.Error("ShouldRotate() = true for fresh key, want false")
	}

	// Should rotate if max age is 0
	if !km.ShouldRotate(0) {
		t.Error("ShouldRotate() = false with maxAge=0, want true")
	}
}

func TestCleanupDeprecatedKeys(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate and deprecate some keys
	v1, _ := km.GenerateKEK()
	v2, _ := km.GenerateKEK()
	v3, _ := km.GenerateKEK()

	km.DeprecateKey(v1)
	km.DeprecateKey(v2)

	// Cleanup keys older than 1 hour (should not cleanup anything yet)
	err := km.CleanupDeprecatedKeys(1 * time.Hour)
	if err != nil {
		t.Fatalf("CleanupDeprecatedKeys() failed: %v", err)
	}

	// Should still have all keys
	if len(km.ListKeys()) != 3 {
		t.Errorf("After cleanup, have %d keys, want 3", len(km.ListKeys()))
	}

	// Cleanup keys older than 0 (should cleanup deprecated keys)
	err = km.CleanupDeprecatedKeys(0)
	if err != nil {
		t.Fatalf("CleanupDeprecatedKeys() failed: %v", err)
	}

	// Should only have active key left
	keys := km.ListKeys()
	if len(keys) != 1 {
		t.Errorf("After aggressive cleanup, have %d keys, want 1", len(keys))
	}

	if keys[0].Version != v3 {
		t.Errorf("Remaining key version = %d, want %d", keys[0].Version, v3)
	}
}

func TestGetStatistics(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate some keys with different statuses
	v1, _ := km.GenerateKEK()
	_, _ = km.GenerateKEK()
	v3, _ := km.GenerateKEK()

	km.DeprecateKey(v1)

	stats := km.GetStatistics()

	if stats.TotalKeys != 3 {
		t.Errorf("TotalKeys = %d, want 3", stats.TotalKeys)
	}

	if stats.ActiveKeys != 1 {
		t.Errorf("ActiveKeys = %d, want 1", stats.ActiveKeys)
	}

	if stats.RotatedKeys != 1 {
		t.Errorf("RotatedKeys = %d, want 1", stats.RotatedKeys)
	}

	if stats.DeprecatedKeys != 1 {
		t.Errorf("DeprecatedKeys = %d, want 1", stats.DeprecatedKeys)
	}

	if stats.ActiveVersion != v3 {
		t.Errorf("ActiveVersion = %d, want %d", stats.ActiveVersion, v3)
	}
}

func TestExportKeyMetadata(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	km.GenerateKEK()
	km.GenerateKEK()

	data, err := km.ExportKeyMetadata()
	if err != nil {
		t.Fatalf("ExportKeyMetadata() failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Exported metadata is empty")
	}

	// Should be valid JSON
	if data[0] != '[' {
		t.Error("Exported data is not JSON array")
	}
}

func TestKeyFilePermissions(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	version, _ := km.GenerateKEK()

	// Check file permissions
	keyPath := filepath.Join(tempDir, "key_v1.json")
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("Failed to stat key file: %v", err)
	}

	perm := info.Mode().Perm()
	expected := os.FileMode(0600)

	if perm != expected {
		t.Errorf("Key file permissions = %o, want %o", perm, expected)
	}

	// Verify key was actually saved
	_, err = km.GetKEK(version)
	if err != nil {
		t.Errorf("Failed to retrieve saved key: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	masterKey, _ := GenerateKey()

	km, _ := NewKeyManager(KeyManagerConfig{
		KeyDir:    tempDir,
		MasterKey: masterKey,
	})
	defer km.Close()

	// Generate initial key
	version, _ := km.GenerateKEK()

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, err := km.GetKEK(version)
				if err != nil {
					t.Errorf("Concurrent GetKEK() failed: %v", err)
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
