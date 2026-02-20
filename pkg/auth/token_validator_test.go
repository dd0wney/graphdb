package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// mockValidator is a test validator for CompositeTokenValidator tests
type mockValidator struct {
	name     string
	validate func(ctx context.Context, token string) (*Claims, error)
}

func (m *mockValidator) ValidateToken(ctx context.Context, token string) (*Claims, error) {
	return m.validate(ctx, token)
}

func (m *mockValidator) Name() string {
	return m.name
}

// TestNewCompositeTokenValidator tests creation of composite validator
func TestNewCompositeTokenValidator(t *testing.T) {
	// Create with no validators
	cv := NewCompositeTokenValidator()
	if cv == nil {
		t.Fatal("Expected non-nil CompositeTokenValidator")
	}

	// Create with validators
	v1 := &mockValidator{name: "mock1"}
	v2 := &mockValidator{name: "mock2"}
	cv = NewCompositeTokenValidator(v1, v2)
	if cv == nil {
		t.Fatal("Expected non-nil CompositeTokenValidator")
	}
}

// TestCompositeTokenValidator_Name tests that Name returns "composite"
func TestCompositeTokenValidator_Name(t *testing.T) {
	cv := NewCompositeTokenValidator()
	if got := cv.Name(); got != "composite" {
		t.Errorf("Name() = %q, want %q", got, "composite")
	}
}

// TestCompositeTokenValidator_AddValidator tests adding validators
func TestCompositeTokenValidator_AddValidator(t *testing.T) {
	cv := NewCompositeTokenValidator()

	v1 := &mockValidator{name: "mock1"}
	v2 := &mockValidator{name: "mock2"}

	cv.AddValidator(v1)
	cv.AddValidator(v2)

	// Verify by checking that validators are called in order
	callOrder := []string{}
	v1.validate = func(ctx context.Context, token string) (*Claims, error) {
		callOrder = append(callOrder, "mock1")
		return nil, errors.New("mock1 failed")
	}
	v2.validate = func(ctx context.Context, token string) (*Claims, error) {
		callOrder = append(callOrder, "mock2")
		return &Claims{UserID: "user1"}, nil
	}

	_, _ = cv.ValidateToken(context.Background(), "test-token")

	if len(callOrder) != 2 || callOrder[0] != "mock1" || callOrder[1] != "mock2" {
		t.Errorf("Expected call order [mock1, mock2], got %v", callOrder)
	}
}

// TestCompositeTokenValidator_ValidateToken tests token validation with multiple validators
func TestCompositeTokenValidator_ValidateToken(t *testing.T) {
	tests := []struct {
		name       string
		validators []TokenValidator
		wantErr    bool
		wantUserID string
	}{
		{
			name:       "no validators",
			validators: []TokenValidator{},
			wantErr:    true,
		},
		{
			name: "first validator succeeds",
			validators: []TokenValidator{
				&mockValidator{
					name: "v1",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return &Claims{UserID: "user-from-v1"}, nil
					},
				},
				&mockValidator{
					name: "v2",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return &Claims{UserID: "user-from-v2"}, nil
					},
				},
			},
			wantErr:    false,
			wantUserID: "user-from-v1",
		},
		{
			name: "first fails, second succeeds",
			validators: []TokenValidator{
				&mockValidator{
					name: "v1",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return nil, errors.New("v1 failed")
					},
				},
				&mockValidator{
					name: "v2",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return &Claims{UserID: "user-from-v2"}, nil
					},
				},
			},
			wantErr:    false,
			wantUserID: "user-from-v2",
		},
		{
			name: "all validators fail",
			validators: []TokenValidator{
				&mockValidator{
					name: "v1",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return nil, errors.New("v1 failed")
					},
				},
				&mockValidator{
					name: "v2",
					validate: func(ctx context.Context, token string) (*Claims, error) {
						return nil, errors.New("v2 failed")
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := NewCompositeTokenValidator(tt.validators...)
			claims, err := cv.ValidateToken(context.Background(), "test-token")

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if claims != nil {
					t.Error("Expected nil claims on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if claims == nil {
					t.Fatal("Expected non-nil claims")
				}
				if claims.UserID != tt.wantUserID {
					t.Errorf("UserID = %q, want %q", claims.UserID, tt.wantUserID)
				}
			}
		})
	}
}

// TestCompositeTokenValidator_ReturnsLastError tests that last error is returned when all fail
func TestCompositeTokenValidator_ReturnsLastError(t *testing.T) {
	lastErr := errors.New("last validator specific error")

	cv := NewCompositeTokenValidator(
		&mockValidator{
			name: "v1",
			validate: func(ctx context.Context, token string) (*Claims, error) {
				return nil, errors.New("first error")
			},
		},
		&mockValidator{
			name: "v2",
			validate: func(ctx context.Context, token string) (*Claims, error) {
				return nil, lastErr
			},
		},
	)

	_, err := cv.ValidateToken(context.Background(), "test-token")
	if err != lastErr {
		t.Errorf("Expected last error %v, got %v", lastErr, err)
	}
}

// TestCompositeTokenValidator_WithRealJWT tests composite validator with real JWT manager
func TestCompositeTokenValidator_WithRealJWT(t *testing.T) {
	secret := "test-secret-key-must-be-at-least-32-characters-long"
	jwtManager, err := NewJWTManager(secret, 15*time.Minute, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// Create a failing mock validator and the real JWT validator
	failingValidator := &mockValidator{
		name: "failing",
		validate: func(ctx context.Context, token string) (*Claims, error) {
			return nil, errors.New("not my token format")
		},
	}

	cv := NewCompositeTokenValidator(failingValidator, jwtManager)

	// Generate a real JWT token
	token, err := jwtManager.GenerateToken("user123", "alice", "admin")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Validate through composite - should fall through to JWT manager
	claims, err := cv.ValidateToken(context.Background(), token)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if claims == nil {
		t.Fatal("Expected non-nil claims")
	}
	if claims.UserID != "user123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user123")
	}
}
