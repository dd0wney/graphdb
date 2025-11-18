package auth

import (
	"strings"
	"testing"
	"time"
)

// TestAPIKeyStore_CreateKey tests API key creation
func TestAPIKeyStore_CreateKey(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	// Create test user
	user, err := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name        string
		userID      string
		keyName     string
		permissions []string
		expiresIn   time.Duration
		wantError   bool
	}{
		{
			name:        "Valid API key",
			userID:      user.ID,
			keyName:     "Production API Key",
			permissions: []string{"read", "write"},
			expiresIn:   365 * 24 * time.Hour,
			wantError:   false,
		},
		{
			name:        "Read-only key",
			userID:      user.ID,
			keyName:     "Read-only Key",
			permissions: []string{"read"},
			expiresIn:   30 * 24 * time.Hour,
			wantError:   false,
		},
		{
			name:        "Empty userID should fail",
			userID:      "",
			keyName:     "Test Key",
			permissions: []string{"read"},
			expiresIn:   24 * time.Hour,
			wantError:   true,
		},
		{
			name:        "Empty key name should fail",
			userID:      user.ID,
			keyName:     "",
			permissions: []string{"read"},
			expiresIn:   24 * time.Hour,
			wantError:   true,
		},
		{
			name:        "Empty permissions should fail",
			userID:      user.ID,
			keyName:     "Test Key",
			permissions: []string{},
			expiresIn:   24 * time.Hour,
			wantError:   true,
		},
		{
			name:        "Invalid permission should fail",
			userID:      user.ID,
			keyName:     "Test Key",
			permissions: []string{"superadmin"},
			expiresIn:   24 * time.Hour,
			wantError:   true,
		},
		{
			name:        "Zero expiration time",
			userID:      user.ID,
			keyName:     "No expiration key",
			permissions: []string{"read"},
			expiresIn:   0,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, key, err := store.CreateKey(tt.userID, tt.keyName, tt.permissions, tt.expiresIn)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if apiKey != nil || key != "" {
					t.Errorf("Expected nil key on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if apiKey == nil {
					t.Error("Expected non-nil API key")
					return
				}
				if key == "" {
					t.Error("Expected non-empty key string")
				}

				// Verify key has proper format (prefix_env_random)
				if !strings.HasPrefix(key, "gdb_") {
					t.Errorf("Key should have 'gdb_' prefix, got: %s", key)
				}

				// Verify key length (should be at least 32 characters)
				if len(key) < 32 {
					t.Errorf("Key too short: %d characters", len(key))
				}

				// Verify API key properties
				if apiKey.UserID != tt.userID {
					t.Errorf("Expected UserID %s, got %s", tt.userID, apiKey.UserID)
				}
				if apiKey.Name != tt.keyName {
					t.Errorf("Expected Name %s, got %s", tt.keyName, apiKey.Name)
				}
				if len(apiKey.Permissions) != len(tt.permissions) {
					t.Errorf("Expected %d permissions, got %d", len(tt.permissions), len(apiKey.Permissions))
				}
				if apiKey.KeyHash == "" {
					t.Error("Expected non-empty key hash")
				}
				if apiKey.Prefix == "" {
					t.Error("Expected non-empty prefix")
				}
			}
		})
	}
}

// TestAPIKeyStore_ValidateKey tests API key validation
func TestAPIKeyStore_ValidateKey(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	// Create test user
	user, err := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create valid key
	_, validKey, err := store.CreateKey(user.ID, "Valid Key", []string{"read", "write"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	// Create expired key
	_, expiredKey, err := store.CreateKey(user.ID, "Expired Key", []string{"read"}, -1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create expired key: %v", err)
	}

	tests := []struct {
		name      string
		key       string
		wantError bool
	}{
		{
			name:      "Valid key",
			key:       validKey,
			wantError: false,
		},
		{
			name:      "Expired key",
			key:       expiredKey,
			wantError: true,
		},
		{
			name:      "Invalid key format",
			key:       "invalid_key",
			wantError: true,
		},
		{
			name:      "Empty key",
			key:       "",
			wantError: true,
		},
		{
			name:      "Non-existent key",
			key:       "gdb_test_nonexistent1234567890",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiKey, err := store.ValidateKey(tt.key)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if apiKey != nil {
					t.Errorf("Expected nil API key on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if apiKey == nil {
					t.Error("Expected non-nil API key")
				}
			}
		})
	}
}

// TestAPIKeyStore_ListKeys tests listing API keys for a user
func TestAPIKeyStore_ListKeys(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	// Create test users
	user1, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	user2, _ := userStore.CreateUser("bob", "Password123!", RoleEditor)

	// Initially no keys
	keys := store.ListKeys(user1.ID)
	if len(keys) != 0 {
		t.Errorf("Expected 0 keys initially, got %d", len(keys))
	}

	// Create keys for user1
	_, _, err := store.CreateKey(user1.ID, "Key 1", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create key 1: %v", err)
	}
	_, _, err = store.CreateKey(user1.ID, "Key 2", []string{"write"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create key 2: %v", err)
	}

	// Create key for user2
	_, _, err = store.CreateKey(user2.ID, "Bob's Key", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create Bob's key: %v", err)
	}

	// List keys for user1
	keys = store.ListKeys(user1.ID)
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys for user1, got %d", len(keys))
	}

	// List keys for user2
	keys = store.ListKeys(user2.ID)
	if len(keys) != 1 {
		t.Errorf("Expected 1 key for user2, got %d", len(keys))
	}
}

// TestAPIKeyStore_RevokeKey tests key revocation
func TestAPIKeyStore_RevokeKey(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	user, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	apiKey, key, err := store.CreateKey(user.ID, "Test Key", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	// Verify key works before revocation
	_, err = store.ValidateKey(key)
	if err != nil {
		t.Errorf("Key should be valid before revocation: %v", err)
	}

	// Revoke key
	err = store.RevokeKey(apiKey.ID)
	if err != nil {
		t.Errorf("Failed to revoke key: %v", err)
	}

	// Verify key no longer works
	_, err = store.ValidateKey(key)
	if err == nil {
		t.Error("Revoked key should not validate")
	}

	// Try revoking non-existent key
	err = store.RevokeKey("nonexistent")
	if err == nil {
		t.Error("Expected error when revoking non-existent key")
	}
}

// TestAPIKeyStore_GetKey tests retrieving a specific key
func TestAPIKeyStore_GetKey(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	user, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	created, _, err := store.CreateKey(user.ID, "Test Key", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	// Get existing key
	retrieved, err := store.GetKey(created.ID)
	if err != nil {
		t.Errorf("Failed to get key: %v", err)
	}
	if retrieved.ID != created.ID {
		t.Errorf("Expected key ID %s, got %s", created.ID, retrieved.ID)
	}

	// Get non-existent key
	_, err = store.GetKey("nonexistent")
	if err == nil {
		t.Error("Expected error when getting non-existent key")
	}
}

// TestAPIKeyStore_UpdateKeyName tests updating key metadata
func TestAPIKeyStore_UpdateKeyName(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	user, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	apiKey, _, err := store.CreateKey(user.ID, "Old Name", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	// Update name
	newName := "New Name"
	err = store.UpdateKeyName(apiKey.ID, newName)
	if err != nil {
		t.Errorf("Failed to update key name: %v", err)
	}

	// Verify update
	updated, err := store.GetKey(apiKey.ID)
	if err != nil {
		t.Fatalf("Failed to get updated key: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Expected name %s, got %s", newName, updated.Name)
	}

	// Try updating non-existent key
	err = store.UpdateKeyName("nonexistent", "name")
	if err == nil {
		t.Error("Expected error when updating non-existent key")
	}
}

// TestAPIKeyStore_KeyPermissions tests permission checking
func TestAPIKeyStore_KeyPermissions(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	user, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	apiKey, _, err := store.CreateKey(user.ID, "Test Key", []string{"read", "write"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	tests := []struct {
		permission string
		expected   bool
	}{
		{"read", true},
		{"write", true},
		{"delete", false},
		{"admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			hasPermission := store.HasPermission(apiKey, tt.permission)
			if hasPermission != tt.expected {
				t.Errorf("Expected HasPermission(%s) = %v, got %v", tt.permission, tt.expected, hasPermission)
			}
		})
	}
}

// TestAPIKeyStore_LastUsed tests updating last used timestamp
func TestAPIKeyStore_LastUsed(t *testing.T) {
	store := NewAPIKeyStore()
	userStore := NewUserStore()

	user, _ := userStore.CreateUser("alice", "Password123!", RoleAdmin)
	apiKey, _, err := store.CreateKey(user.ID, "Test Key", []string{"read"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create test key: %v", err)
	}

	// Initially last used should be zero
	if !apiKey.LastUsed.IsZero() {
		t.Error("Expected LastUsed to be zero initially")
	}

	// Update last used
	time.Sleep(10 * time.Millisecond) // Ensure time difference
	err = store.UpdateLastUsed(apiKey.ID)
	if err != nil {
		t.Errorf("Failed to update last used: %v", err)
	}

	// Verify update
	updated, err := store.GetKey(apiKey.ID)
	if err != nil {
		t.Fatalf("Failed to get updated key: %v", err)
	}
	if updated.LastUsed.IsZero() {
		t.Error("Expected LastUsed to be non-zero after update")
	}
}
