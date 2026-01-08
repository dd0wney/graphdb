package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken      = errors.New("invalid token")
	ErrExpiredToken      = errors.New("token has expired")
	ErrInvalidClaims     = errors.New("invalid token claims")
	ErrEmptyUserID       = errors.New("userID cannot be empty")
	ErrEmptyUsername     = errors.New("username cannot be empty")
	ErrEmptyRole         = errors.New("role cannot be empty")
	ErrInvalidRole       = errors.New("invalid role")
	ErrShortSecret       = errors.New("secret must be at least 32 characters")
)

// Valid roles
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

var validRoles = map[string]bool{
	RoleAdmin:  true,
	RoleEditor: true,
	RoleViewer: true,
}

// Claims represents JWT claims
type Claims struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	IssuedAt  time.Time `json:"issued_at"`
}

// RefreshClaims represents refresh token claims
type RefreshClaims struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	IssuedAt  time.Time `json:"issued_at"`
}

// JWTManager manages JWT token generation and validation
type JWTManager struct {
	secretKey             []byte
	tokenDuration         time.Duration
	refreshTokenDuration  time.Duration
}

// NewJWTManager creates a new JWT manager.
// Returns an error if the secret is shorter than 32 characters (security requirement).
func NewJWTManager(secret string, tokenDuration, refreshTokenDuration time.Duration) (*JWTManager, error) {
	if len(secret) < 32 {
		return nil, ErrShortSecret
	}

	return &JWTManager{
		secretKey:            []byte(secret),
		tokenDuration:        tokenDuration,
		refreshTokenDuration: refreshTokenDuration,
	}, nil
}

// GenerateToken generates a new JWT token
func (m *JWTManager) GenerateToken(userID, username, role string) (string, error) {
	// Validate inputs
	if userID == "" {
		return "", ErrEmptyUserID
	}
	if username == "" {
		return "", ErrEmptyUsername
	}
	if role == "" {
		return "", ErrEmptyRole
	}
	if !validRoles[role] {
		return "", fmt.Errorf("%w: %s", ErrInvalidRole, role)
	}

	now := time.Now()
	expiresAt := now.Add(m.tokenDuration)

	// Create custom claims
	claims := jwt.MapClaims{
		"user_id":    userID,
		"username":   username,
		"role":       role,
		"expires_at": expiresAt.Unix(),
		"issued_at":  now.Unix(),
		"exp":        expiresAt.Unix(), // Standard JWT expiration claim
		"iat":        now.Unix(),        // Standard JWT issued at claim
	}

	// Create token with claims
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Sign token with secret key
	tokenString, err := token.SignedString(m.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, nil
}

// ValidateToken validates a JWT token and returns claims.
// Implements TokenValidator interface.
func (m *JWTManager) ValidateToken(_ context.Context, tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	// Parse token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// Extract claims
	claimsMap, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidClaims
	}

	// Extract and validate individual claims
	userID, ok := claimsMap["user_id"].(string)
	if !ok || userID == "" {
		return nil, fmt.Errorf("%w: missing or invalid user_id", ErrInvalidClaims)
	}

	username, ok := claimsMap["username"].(string)
	if !ok || username == "" {
		return nil, fmt.Errorf("%w: missing or invalid username", ErrInvalidClaims)
	}

	role, ok := claimsMap["role"].(string)
	if !ok || role == "" {
		return nil, fmt.Errorf("%w: missing or invalid role", ErrInvalidClaims)
	}

	// Extract timestamps
	expiresAtFloat, ok := claimsMap["expires_at"].(float64)
	if !ok {
		return nil, fmt.Errorf("%w: missing or invalid expires_at", ErrInvalidClaims)
	}
	expiresAt := time.Unix(int64(expiresAtFloat), 0)

	issuedAtFloat, ok := claimsMap["issued_at"].(float64)
	if !ok {
		return nil, fmt.Errorf("%w: missing or invalid issued_at", ErrInvalidClaims)
	}
	issuedAt := time.Unix(int64(issuedAtFloat), 0)

	// Check expiration
	if time.Now().After(expiresAt) {
		return nil, ErrExpiredToken
	}

	return &Claims{
		UserID:    userID,
		Username:  username,
		Role:      role,
		ExpiresAt: expiresAt,
		IssuedAt:  issuedAt,
	}, nil
}

// GenerateRefreshToken generates a refresh token
func (m *JWTManager) GenerateRefreshToken(userID string) (string, error) {
	if userID == "" {
		return "", ErrEmptyUserID
	}

	now := time.Now()
	expiresAt := now.Add(m.refreshTokenDuration)

	claims := jwt.MapClaims{
		"user_id":    userID,
		"expires_at": expiresAt.Unix(),
		"issued_at":  now.Unix(),
		"exp":        expiresAt.Unix(),
		"iat":        now.Unix(),
		"type":       "refresh", // Mark as refresh token
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(m.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return tokenString, nil
}

// ValidateRefreshToken validates a refresh token and returns the userID
func (m *JWTManager) ValidateRefreshToken(tokenString string) (string, error) {
	if tokenString == "" {
		return "", ErrInvalidToken
	}

	// Parse token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})

	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return "", ErrInvalidToken
	}

	// Extract claims
	claimsMap, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", ErrInvalidClaims
	}

	// Verify it's a refresh token
	tokenType, ok := claimsMap["type"].(string)
	if !ok || tokenType != "refresh" {
		return "", fmt.Errorf("%w: not a refresh token", ErrInvalidToken)
	}

	// Extract userID
	userID, ok := claimsMap["user_id"].(string)
	if !ok || userID == "" {
		return "", fmt.Errorf("%w: missing or invalid user_id", ErrInvalidClaims)
	}

	// Check expiration
	expiresAtFloat, ok := claimsMap["expires_at"].(float64)
	if !ok {
		return "", fmt.Errorf("%w: missing or invalid expires_at", ErrInvalidClaims)
	}
	expiresAt := time.Unix(int64(expiresAtFloat), 0)

	if time.Now().After(expiresAt) {
		return "", ErrExpiredToken
	}

	return userID, nil
}

// Name returns the validator name for logging/debugging.
// Implements TokenValidator interface.
func (m *JWTManager) Name() string {
	return "jwt-hs256"
}

// GetTokenDuration returns the configured token duration
func (m *JWTManager) GetTokenDuration() time.Duration {
	return m.tokenDuration
}
