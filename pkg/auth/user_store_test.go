package auth

import (
	"testing"
	"time"
)

// TestUserStore_CreateUser tests user creation
func TestUserStore_CreateUser(t *testing.T) {
	store := NewUserStore()

	tests := []struct {
		name      string
		username  string
		password  string
		role      string
		wantError bool
	}{
		{
			name:      "Valid admin user",
			username:  "admin",
			password:  "SecurePass123!",
			role:      RoleAdmin,
			wantError: false,
		},
		{
			name:      "Valid editor user",
			username:  "editor",
			password:  "EditorPass456!",
			role:      RoleEditor,
			wantError: false,
		},
		{
			name:      "Valid viewer user",
			username:  "viewer",
			password:  "ViewerPass789!",
			role:      RoleViewer,
			wantError: false,
		},
		{
			name:      "Empty username should fail",
			username:  "",
			password:  "password",
			role:      RoleViewer,
			wantError: true,
		},
		{
			name:      "Empty password should fail",
			username:  "user",
			password:  "",
			role:      RoleViewer,
			wantError: true,
		},
		{
			name:      "Invalid role should fail",
			username:  "user",
			password:  "password",
			role:      "superadmin",
			wantError: true,
		},
		{
			name:      "Short password should fail",
			username:  "user",
			password:  "short",
			role:      RoleViewer,
			wantError: true,
		},
		{
			name:      "Username with spaces should fail",
			username:  "user name",
			password:  "SecurePass123!",
			role:      RoleViewer,
			wantError: true,
		},
		{
			name:      "Username with special chars should fail",
			username:  "user@name",
			password:  "SecurePass123!",
			role:      RoleViewer,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := store.CreateUser(tt.username, tt.password, tt.role)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if user != nil {
					t.Errorf("Expected nil user on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if user == nil {
					t.Error("Expected non-nil user")
					return
				}
				if user.Username != tt.username {
					t.Errorf("Expected username %s, got %s", tt.username, user.Username)
				}
				if user.Role != tt.role {
					t.Errorf("Expected role %s, got %s", tt.role, user.Role)
				}
				if user.ID == "" {
					t.Error("Expected non-empty user ID")
				}
				if user.PasswordHash == "" {
					t.Error("Expected non-empty password hash")
				}
				if user.PasswordHash == tt.password {
					t.Error("Password should be hashed, not stored in plaintext")
				}
			}
		})
	}
}

// TestUserStore_GetUserByUsername tests retrieving users
func TestUserStore_GetUserByUsername(t *testing.T) {
	store := NewUserStore()

	// Create test user
	_, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name      string
		username  string
		wantError bool
	}{
		{
			name:      "Existing user",
			username:  "alice",
			wantError: false,
		},
		{
			name:      "Non-existent user",
			username:  "bob",
			wantError: true,
		},
		{
			name:      "Empty username",
			username:  "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := store.GetUserByUsername(tt.username)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if user != nil {
					t.Errorf("Expected nil user on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if user == nil {
					t.Error("Expected non-nil user")
					return
				}
				if user.Username != tt.username {
					t.Errorf("Expected username %s, got %s", tt.username, user.Username)
				}
			}
		})
	}
}

// TestUserStore_GetUserByID tests retrieving users by ID
func TestUserStore_GetUserByID(t *testing.T) {
	store := NewUserStore()

	// Create test user
	createdUser, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name      string
		userID    string
		wantError bool
	}{
		{
			name:      "Existing user",
			userID:    createdUser.ID,
			wantError: false,
		},
		{
			name:      "Non-existent user",
			userID:    "nonexistent",
			wantError: true,
		},
		{
			name:      "Empty ID",
			userID:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := store.GetUserByID(tt.userID)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if user != nil {
					t.Errorf("Expected nil user on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if user == nil {
					t.Error("Expected non-nil user")
					return
				}
				if user.ID != tt.userID {
					t.Errorf("Expected ID %s, got %s", tt.userID, user.ID)
				}
			}
		})
	}
}

// TestUserStore_VerifyPassword tests password verification
func TestUserStore_VerifyPassword(t *testing.T) {
	store := NewUserStore()

	// Create test user
	password := "SecurePass123!"
	user, err := store.CreateUser("alice", password, RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name      string
		password  string
		wantMatch bool
	}{
		{
			name:      "Correct password",
			password:  password,
			wantMatch: true,
		},
		{
			name:      "Wrong password",
			password:  "WrongPass123!",
			wantMatch: false,
		},
		{
			name:      "Empty password",
			password:  "",
			wantMatch: false,
		},
		{
			name:      "Similar but wrong password",
			password:  "SecurePass123",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := store.VerifyPassword(user, tt.password)

			if match != tt.wantMatch {
				t.Errorf("Expected match=%v, got %v", tt.wantMatch, match)
			}
		})
	}
}

// TestUserStore_DuplicateUsername tests that duplicate usernames are rejected
func TestUserStore_DuplicateUsername(t *testing.T) {
	store := NewUserStore()

	// Create first user
	_, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create first user: %v", err)
	}

	// Try to create duplicate user
	_, err = store.CreateUser("alice", "DifferentPass456!", RoleEditor)
	if err == nil {
		t.Error("Expected error for duplicate username, got none")
	}
}

// TestUserStore_ListUsers tests listing all users
func TestUserStore_ListUsers(t *testing.T) {
	store := NewUserStore()

	// Initially should be empty
	users := store.ListUsers()
	if len(users) != 0 {
		t.Errorf("Expected 0 users initially, got %d", len(users))
	}

	// Create some users
	usernames := []string{"alice", "bob", "charlie"}
	for _, username := range usernames {
		_, err := store.CreateUser(username, "Password123!", RoleViewer)
		if err != nil {
			t.Fatalf("Failed to create user %s: %v", username, err)
		}
	}

	// List should now have 3 users
	users = store.ListUsers()
	if len(users) != 3 {
		t.Errorf("Expected 3 users, got %d", len(users))
	}

	// Verify all usernames are present
	usernameMap := make(map[string]bool)
	for _, user := range users {
		usernameMap[user.Username] = true
	}
	for _, username := range usernames {
		if !usernameMap[username] {
			t.Errorf("Expected username %s in list, but not found", username)
		}
	}
}

// TestUserStore_UpdateUser tests updating user details
func TestUserStore_UpdateUser(t *testing.T) {
	store := NewUserStore()

	// Create test user
	user, err := store.CreateUser("alice", "AlicePass123!", RoleViewer)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Update role to admin
	err = store.UpdateUserRole(user.ID, RoleAdmin)
	if err != nil {
		t.Errorf("Failed to update user role: %v", err)
	}

	// Verify update
	updatedUser, err := store.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("Failed to get updated user: %v", err)
	}
	if updatedUser.Role != RoleAdmin {
		t.Errorf("Expected role %s, got %s", RoleAdmin, updatedUser.Role)
	}

	// Try invalid role
	err = store.UpdateUserRole(user.ID, "superadmin")
	if err == nil {
		t.Error("Expected error for invalid role, got none")
	}

	// Try non-existent user
	err = store.UpdateUserRole("nonexistent", RoleAdmin)
	if err == nil {
		t.Error("Expected error for non-existent user, got none")
	}
}

// TestUserStore_DeleteUser tests user deletion
func TestUserStore_DeleteUser(t *testing.T) {
	store := NewUserStore()

	// Create test user
	user, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Verify user exists
	_, err = store.GetUserByID(user.ID)
	if err != nil {
		t.Errorf("User should exist before deletion: %v", err)
	}

	// Delete user
	err = store.DeleteUser(user.ID)
	if err != nil {
		t.Errorf("Failed to delete user: %v", err)
	}

	// Verify user no longer exists
	_, err = store.GetUserByID(user.ID)
	if err == nil {
		t.Error("User should not exist after deletion")
	}

	// Try deleting non-existent user
	err = store.DeleteUser("nonexistent")
	if err == nil {
		t.Error("Expected error when deleting non-existent user")
	}
}

// TestUserStore_ChangePassword tests password changes
func TestUserStore_ChangePassword(t *testing.T) {
	store := NewUserStore()

	oldPassword := "OldPass123!"
	newPassword := "NewPass456!"

	// Create test user
	user, err := store.CreateUser("alice", oldPassword, RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Verify old password works
	if !store.VerifyPassword(user, oldPassword) {
		t.Error("Old password should be valid before change")
	}

	// Change password
	err = store.ChangePassword(user.ID, newPassword)
	if err != nil {
		t.Errorf("Failed to change password: %v", err)
	}

	// Get updated user
	updatedUser, err := store.GetUserByID(user.ID)
	if err != nil {
		t.Fatalf("Failed to get updated user: %v", err)
	}

	// Verify new password works
	if !store.VerifyPassword(updatedUser, newPassword) {
		t.Error("New password should be valid after change")
	}

	// Verify old password no longer works
	if store.VerifyPassword(updatedUser, oldPassword) {
		t.Error("Old password should not be valid after change")
	}

	// Try with invalid password (too short)
	err = store.ChangePassword(user.ID, "short")
	if err == nil {
		t.Error("Expected error for invalid new password")
	}
}

// OIDC User Provisioning Tests

// TestUserStore_CreateOrUpdateOIDCUser tests OIDC user provisioning
func TestUserStore_CreateOrUpdateOIDCUser(t *testing.T) {
	store := NewUserStore()
	now := time.Now().Unix()

	tests := []struct {
		name              string
		info              *OIDCUserInfo
		expectNew         bool
		expectErr         bool
		expectedUsername  string
		expectedRole      string
	}{
		{
			name: "Create new OIDC user with preferred_username",
			info: &OIDCUserInfo{
				Subject:           "user123",
				Issuer:            "https://idp.example.com",
				Email:             "user@example.com",
				PreferredUsername: "testuser",
				Role:              "viewer",
			},
			expectNew:        true,
			expectErr:        false,
			expectedUsername: "testuser",
			expectedRole:     "viewer",
		},
		{
			name: "Create new OIDC user with email only",
			info: &OIDCUserInfo{
				Subject: "user456",
				Issuer:  "https://idp.example.com",
				Email:   "alice@example.com",
				Role:    "editor",
			},
			expectNew:        true,
			expectErr:        false,
			expectedUsername: "alice",
			expectedRole:     "editor",
		},
		{
			name: "Update existing OIDC user",
			info: &OIDCUserInfo{
				Subject:           "user123",
				Issuer:            "https://idp.example.com",
				Email:             "newemail@example.com",
				PreferredUsername: "testuser",
				Role:              "viewer",
			},
			expectNew:        false,
			expectErr:        false,
			expectedUsername: "testuser",
			expectedRole:     "viewer",
		},
		{
			name: "Update existing user with higher role",
			info: &OIDCUserInfo{
				Subject: "user123",
				Issuer:  "https://idp.example.com",
				Role:    "admin",
			},
			expectNew:    false,
			expectErr:    false,
			expectedRole: "admin",
		},
		{
			name: "Missing subject should fail",
			info: &OIDCUserInfo{
				Issuer: "https://idp.example.com",
			},
			expectErr: true,
		},
		{
			name: "Missing issuer should fail",
			info: &OIDCUserInfo{
				Subject: "user789",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, isNew, err := store.CreateOrUpdateOIDCUser(tt.info, now)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if isNew != tt.expectNew {
				t.Errorf("Expected isNew=%v, got %v", tt.expectNew, isNew)
			}

			if tt.expectedUsername != "" && user.Username != tt.expectedUsername {
				t.Errorf("Expected username %q, got %q", tt.expectedUsername, user.Username)
			}

			if user.Role != tt.expectedRole {
				t.Errorf("Expected role %q, got %q", tt.expectedRole, user.Role)
			}

			if user.AuthProvider != AuthProviderOIDC {
				t.Errorf("Expected AuthProvider %q, got %q", AuthProviderOIDC, user.AuthProvider)
			}
		})
	}
}

// TestUserStore_GetUserByOIDCSubject tests OIDC user lookup
func TestUserStore_GetUserByOIDCSubject(t *testing.T) {
	store := NewUserStore()
	now := time.Now().Unix()

	// Create OIDC user
	info := &OIDCUserInfo{
		Subject: "oidc-user-123",
		Issuer:  "https://idp.example.com",
		Email:   "oidc@example.com",
		Role:    "viewer",
	}
	_, _, err := store.CreateOrUpdateOIDCUser(info, now)
	if err != nil {
		t.Fatalf("Failed to create OIDC user: %v", err)
	}

	tests := []struct {
		name      string
		issuer    string
		subject   string
		expectErr bool
	}{
		{
			name:      "Find existing OIDC user",
			issuer:    "https://idp.example.com",
			subject:   "oidc-user-123",
			expectErr: false,
		},
		{
			name:      "User not found - wrong subject",
			issuer:    "https://idp.example.com",
			subject:   "wrong-subject",
			expectErr: true,
		},
		{
			name:      "User not found - wrong issuer",
			issuer:    "https://different.idp.com",
			subject:   "oidc-user-123",
			expectErr: true,
		},
		{
			name:      "Empty issuer",
			issuer:    "",
			subject:   "oidc-user-123",
			expectErr: true,
		},
		{
			name:      "Empty subject",
			issuer:    "https://idp.example.com",
			subject:   "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := store.GetUserByOIDCSubject(tt.issuer, tt.subject)

			if tt.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if user.OIDCSubject != tt.subject {
				t.Errorf("Expected subject %q, got %q", tt.subject, user.OIDCSubject)
			}

			if user.OIDCIssuer != tt.issuer {
				t.Errorf("Expected issuer %q, got %q", tt.issuer, user.OIDCIssuer)
			}
		})
	}
}

// TestUserStore_DeleteOIDCUser tests deletion of OIDC users
func TestUserStore_DeleteOIDCUser(t *testing.T) {
	store := NewUserStore()
	now := time.Now().Unix()

	// Create OIDC user
	info := &OIDCUserInfo{
		Subject: "delete-test-user",
		Issuer:  "https://idp.example.com",
		Email:   "delete@example.com",
		Role:    "viewer",
	}
	user, _, err := store.CreateOrUpdateOIDCUser(info, now)
	if err != nil {
		t.Fatalf("Failed to create OIDC user: %v", err)
	}

	// Verify user exists
	_, err = store.GetUserByOIDCSubject(info.Issuer, info.Subject)
	if err != nil {
		t.Error("OIDC user should exist before deletion")
	}

	// Delete user
	err = store.DeleteUser(user.ID)
	if err != nil {
		t.Errorf("Failed to delete OIDC user: %v", err)
	}

	// Verify user no longer exists in any index
	_, err = store.GetUserByID(user.ID)
	if err == nil {
		t.Error("User should not exist by ID after deletion")
	}

	_, err = store.GetUserByUsername(user.Username)
	if err == nil {
		t.Error("User should not exist by username after deletion")
	}

	_, err = store.GetUserByOIDCSubject(info.Issuer, info.Subject)
	if err == nil {
		t.Error("User should not exist by OIDC subject after deletion")
	}
}

// TestUserStore_OIDCUsernameGeneration tests username generation for OIDC users
func TestUserStore_OIDCUsernameGeneration(t *testing.T) {
	store := NewUserStore()
	now := time.Now().Unix()

	// Create user with preferred_username
	user1, _, _ := store.CreateOrUpdateOIDCUser(&OIDCUserInfo{
		Subject:           "user1",
		Issuer:            "https://idp.example.com",
		PreferredUsername: "alice",
	}, now)
	if user1.Username != "alice" {
		t.Errorf("Expected username 'alice', got %q", user1.Username)
	}

	// Create another user with same preferred_username (should get unique suffix)
	user2, _, _ := store.CreateOrUpdateOIDCUser(&OIDCUserInfo{
		Subject:           "user2",
		Issuer:            "https://idp.example.com",
		PreferredUsername: "alice",
	}, now)
	if user2.Username == "alice" {
		t.Error("Second user should have unique username")
	}
	if user2.Username != "alice_1" {
		t.Errorf("Expected username 'alice_1', got %q", user2.Username)
	}

	// Create user with email only
	user3, _, _ := store.CreateOrUpdateOIDCUser(&OIDCUserInfo{
		Subject: "user3",
		Issuer:  "https://idp.example.com",
		Email:   "bob@example.com",
	}, now)
	if user3.Username != "bob" {
		t.Errorf("Expected username 'bob', got %q", user3.Username)
	}

	// Create user with special characters in email (should be sanitized)
	user4, _, _ := store.CreateOrUpdateOIDCUser(&OIDCUserInfo{
		Subject: "user4",
		Issuer:  "https://idp.example.com",
		Email:   "charlie+test@example.com",
	}, now)
	if user4.Username != "charlietest" {
		t.Errorf("Expected username 'charlietest' (sanitized), got %q", user4.Username)
	}
}

// TestUserStore_IsOIDCUser tests the IsOIDCUser helper
func TestUserStore_IsOIDCUser(t *testing.T) {
	store := NewUserStore()
	now := time.Now().Unix()

	// Create local user
	localUser, _ := store.CreateUser("localuser", "Password123!", RoleViewer)
	if localUser.IsOIDCUser() {
		t.Error("Local user should not be OIDC user")
	}

	// Create OIDC user
	oidcUser, _, _ := store.CreateOrUpdateOIDCUser(&OIDCUserInfo{
		Subject: "oidc-sub",
		Issuer:  "https://idp.example.com",
	}, now)
	if !oidcUser.IsOIDCUser() {
		t.Error("OIDC user should be OIDC user")
	}
}
