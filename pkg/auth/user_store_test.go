package auth

import (
	"testing"
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
