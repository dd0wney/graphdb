package auth

import (
	"testing"
	"time"
)

// TestJWTManager_GenerateToken tests JWT token generation
func TestJWTManager_GenerateToken(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	tests := []struct {
		name      string
		userID    string
		username  string
		role      string
		wantError bool
	}{
		{
			name:      "Valid token generation",
			userID:    "user123",
			username:  "alice",
			role:      "admin",
			wantError: false,
		},
		{
			name:      "Valid token with viewer role",
			userID:    "user456",
			username:  "bob",
			role:      "viewer",
			wantError: false,
		},
		{
			name:      "Empty userID should fail",
			userID:    "",
			username:  "charlie",
			role:      "editor",
			wantError: true,
		},
		{
			name:      "Empty username should fail",
			userID:    "user789",
			username:  "",
			role:      "editor",
			wantError: true,
		},
		{
			name:      "Empty role should fail",
			userID:    "user101",
			username:  "dave",
			role:      "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := jwtManager.GenerateToken(tt.userID, tt.username, tt.role)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if token != "" {
					t.Errorf("Expected empty token on error, got %s", token)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if token == "" {
					t.Error("Expected non-empty token")
				}
				// Token should be a non-empty string with JWT format (header.payload.signature)
				if len(token) < 20 {
					t.Errorf("Token too short: %s", token)
				}
			}
		})
	}
}

// TestJWTManager_ValidateToken tests JWT token validation
func TestJWTManager_ValidateToken(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// Generate a valid token
	validToken, err := jwtManager.GenerateToken("user123", "alice", "admin")
	if err != nil {
		t.Fatalf("Failed to generate test token: %v", err)
	}

	tests := []struct {
		name      string
		token     string
		wantError bool
	}{
		{
			name:      "Valid token",
			token:     validToken,
			wantError: false,
		},
		{
			name:      "Empty token",
			token:     "",
			wantError: true,
		},
		{
			name:      "Malformed token",
			token:     "not.a.valid.jwt",
			wantError: true,
		},
		{
			name:      "Invalid signature",
			token:     "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := jwtManager.ValidateToken(tt.token)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if claims != nil {
					t.Errorf("Expected nil claims on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if claims == nil {
					t.Error("Expected non-nil claims")
				}
			}
		})
	}
}

// TestJWTManager_ExtractClaims tests claims extraction from valid tokens
func TestJWTManager_ExtractClaims(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	tests := []struct {
		name         string
		userID       string
		username     string
		role         string
	}{
		{
			name:     "Admin user claims",
			userID:   "admin001",
			username: "admin",
			role:     "admin",
		},
		{
			name:     "Editor user claims",
			userID:   "editor001",
			username: "editor",
			role:     "editor",
		},
		{
			name:     "Viewer user claims",
			userID:   "viewer001",
			username: "viewer",
			role:     "viewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := jwtManager.GenerateToken(tt.userID, tt.username, tt.role)
			if err != nil {
				t.Fatalf("Failed to generate token: %v", err)
			}

			claims, err := jwtManager.ValidateToken(token)
			if err != nil {
				t.Fatalf("Failed to validate token: %v", err)
			}

			if claims.UserID != tt.userID {
				t.Errorf("Expected UserID %s, got %s", tt.userID, claims.UserID)
			}
			if claims.Username != tt.username {
				t.Errorf("Expected Username %s, got %s", tt.username, claims.Username)
			}
			if claims.Role != tt.role {
				t.Errorf("Expected Role %s, got %s", tt.role, claims.Role)
			}
			if claims.ExpiresAt.IsZero() {
				t.Error("Expected non-zero ExpiresAt")
			}
			if claims.IssuedAt.IsZero() {
				t.Error("Expected non-zero IssuedAt")
			}
		})
	}
}

// TestJWTManager_TokenExpiration tests that expired tokens are rejected
func TestJWTManager_TokenExpiration(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	// Create manager with very short expiration
	jwtManager, err := NewJWTManager(secret, 1*time.Millisecond, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	token, err := jwtManager.GenerateToken("user123", "alice", "admin")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Wait for token to expire
	time.Sleep(100 * time.Millisecond)

	_, err = jwtManager.ValidateToken(token)
	if err == nil {
		t.Error("Expected error for expired token, got none")
	}
}

// TestJWTManager_GenerateRefreshToken tests refresh token generation
func TestJWTManager_GenerateRefreshToken(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	tests := []struct {
		name      string
		userID    string
		wantError bool
	}{
		{
			name:      "Valid refresh token",
			userID:    "user123",
			wantError: false,
		},
		{
			name:      "Empty userID should fail",
			userID:    "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refreshToken, err := jwtManager.GenerateRefreshToken(tt.userID)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if refreshToken != "" {
					t.Errorf("Expected empty refresh token on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if refreshToken == "" {
					t.Error("Expected non-empty refresh token")
				}
				if len(refreshToken) < 20 {
					t.Errorf("Refresh token too short: %s", refreshToken)
				}
			}
		})
	}
}

// TestJWTManager_ValidateRefreshToken tests refresh token validation
func TestJWTManager_ValidateRefreshToken(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// Generate a valid refresh token
	validRefreshToken, err := jwtManager.GenerateRefreshToken("user123")
	if err != nil {
		t.Fatalf("Failed to generate refresh token: %v", err)
	}

	tests := []struct {
		name         string
		refreshToken string
		wantError    bool
		expectedUser string
	}{
		{
			name:         "Valid refresh token",
			refreshToken: validRefreshToken,
			wantError:    false,
			expectedUser: "user123",
		},
		{
			name:         "Empty refresh token",
			refreshToken: "",
			wantError:    true,
		},
		{
			name:         "Invalid refresh token",
			refreshToken: "invalid.token.here",
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID, err := jwtManager.ValidateRefreshToken(tt.refreshToken)

			if tt.wantError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				if userID != "" {
					t.Errorf("Expected empty userID on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if userID != tt.expectedUser {
					t.Errorf("Expected userID %s, got %s", tt.expectedUser, userID)
				}
			}
		})
	}
}

// TestJWTManager_RefreshTokenExpiration tests refresh token expiration
func TestJWTManager_RefreshTokenExpiration(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	// Create manager with very short refresh token expiration
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	refreshToken, err := jwtManager.GenerateRefreshToken("user123")
	if err != nil {
		t.Fatalf("Failed to generate refresh token: %v", err)
	}

	// Wait for refresh token to expire
	time.Sleep(100 * time.Millisecond)

	_, err = jwtManager.ValidateRefreshToken(refreshToken)
	if err == nil {
		t.Error("Expected error for expired refresh token, got none")
	}
}

// TestJWTManager_DifferentSecrets tests that tokens from different secrets are invalid
func TestJWTManager_DifferentSecrets(t *testing.T) {
	secret1 := "test-secret-key-must-be-at-least-32-characters-long-1"
	secret2 := "test-secret-key-must-be-at-least-32-characters-long-2"

	jwtManager1, err := NewJWTManager(secret1, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager 1: %v", err)
	}
	jwtManager2, err := NewJWTManager(secret2, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager 2: %v", err)
	}

	// Generate token with first manager
	token, err := jwtManager1.GenerateToken("user123", "alice", "admin")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Try to validate with second manager (different secret)
	_, err = jwtManager2.ValidateToken(token)
	if err == nil {
		t.Error("Expected error when validating token with different secret, got none")
	}
}

// TestJWTManager_ShortSecret tests that short secrets are rejected
func TestJWTManager_ShortSecret(t *testing.T) {
	// This should return an error, not panic
	_, err := NewJWTManager("short", 15*time.Minute, 7*24*time.Hour)
	if err == nil {
		t.Error("Expected error for short secret, got none")
	}
	if err != ErrShortSecret {
		t.Errorf("Expected ErrShortSecret, got: %v", err)
	}
}

// TestJWTManager_RoleValidation tests that only valid roles are accepted
func TestJWTManager_RoleValidation(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	validRoles := []string{"admin", "editor", "viewer"}
	for _, role := range validRoles {
		t.Run("Valid role: "+role, func(t *testing.T) {
			token, err := jwtManager.GenerateToken("user123", "alice", role)
			if err != nil {
				t.Errorf("Expected valid role %s to succeed, got error: %v", role, err)
			}
			if token == "" {
				t.Errorf("Expected non-empty token for valid role %s", role)
			}
		})
	}

	invalidRoles := []string{"superadmin", "root", "guest", "anonymous", ""}
	for _, role := range invalidRoles {
		t.Run("Invalid role: "+role, func(t *testing.T) {
			_, err := jwtManager.GenerateToken("user123", "alice", role)
			if err == nil {
				t.Errorf("Expected invalid role %s to fail, got no error", role)
			}
		})
	}
}
