package auth

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserExists        = errors.New("user already exists")
	ErrEmptyPassword     = errors.New("password cannot be empty")
	ErrWeakPassword      = errors.New("password must be at least 8 characters")
	ErrInvalidUsername   = errors.New("username must be 3-50 alphanumeric characters")
	ErrPasswordHashFailed = errors.New("failed to hash password")
)

const (
	MinPasswordLength = 8
	MinUsernameLength = 3
	MaxUsernameLength = 50
	BcryptCost        = 12 // Cost factor for bcrypt
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// AuthProvider indicates how a user authenticates
type AuthProvider string

const (
	AuthProviderLocal AuthProvider = "local"
	AuthProviderOIDC  AuthProvider = "oidc"
)

// User represents a user in the system
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"` // Never serialize password hash
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`

	// OIDC-specific fields (populated for OIDC users)
	AuthProvider AuthProvider `json:"auth_provider,omitempty"`
	OIDCSubject  string       `json:"oidc_subject,omitempty"`  // OIDC 'sub' claim - unique per issuer
	OIDCIssuer   string       `json:"oidc_issuer,omitempty"`   // OIDC issuer URL
	Email        string       `json:"email,omitempty"`         // User's email (from OIDC or manual)
	EmailVerified bool        `json:"email_verified,omitempty"`
	DisplayName  string       `json:"display_name,omitempty"`  // Full name from OIDC
	Picture      string       `json:"picture,omitempty"`       // Profile picture URL
	LastLoginAt  int64        `json:"last_login_at,omitempty"` // Last successful login timestamp
}

// IsOIDCUser returns true if the user was provisioned via OIDC
func (u *User) IsOIDCUser() bool {
	return u.AuthProvider == AuthProviderOIDC
}

// UserStore manages user storage and authentication
type UserStore struct {
	users       map[string]*User   // userID -> User
	usernameMap map[string]string  // username -> userID
	oidcSubMap  map[string]string  // "issuer|subject" -> userID (for OIDC user lookup)
	mu          sync.RWMutex
}

// NewUserStore creates a new user store
func NewUserStore() *UserStore {
	return &UserStore{
		users:       make(map[string]*User),
		usernameMap: make(map[string]string),
		oidcSubMap:  make(map[string]string),
	}
}

// oidcKey creates a unique key for OIDC subject lookup (issuer + subject)
func oidcKey(issuer, subject string) string {
	return issuer + "|" + subject
}

// CreateUser creates a new user with hashed password
func (s *UserStore) CreateUser(username, password, role string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate username
	if err := validateUsername(username); err != nil {
		return nil, err
	}

	// Check for duplicate username
	if _, exists := s.usernameMap[username]; exists {
		return nil, fmt.Errorf("%w: %s", ErrUserExists, username)
	}

	// Validate password
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	// Validate role
	if !validRoles[role] {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRole, role)
	}

	// Hash password
	hashedPassword, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPasswordHashFailed, err)
	}

	// Create user
	user := &User{
		ID:           uuid.New().String(),
		Username:     username,
		PasswordHash: hashedPassword,
		Role:         role,
		AuthProvider: AuthProviderLocal,
		CreatedAt:    0, // Will be set by application
	}

	// Store user
	s.users[user.ID] = user
	s.usernameMap[username] = user.ID

	return user, nil
}

// GetUserByUsername retrieves a user by username
func (s *UserStore) GetUserByUsername(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if username == "" {
		return nil, ErrInvalidUsername
	}

	userID, exists := s.usernameMap[username]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUserNotFound, username)
	}

	user, exists := s.users[userID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUserNotFound, username)
	}

	return user, nil
}

// GetUserByID retrieves a user by ID
func (s *UserStore) GetUserByID(userID string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if userID == "" {
		return nil, ErrUserNotFound
	}

	user, exists := s.users[userID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrUserNotFound, userID)
	}

	return user, nil
}

// VerifyPassword verifies a password against a user's stored hash
func (s *UserStore) VerifyPassword(user *User, password string) bool {
	if user == nil || password == "" {
		return false
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	return err == nil
}

// ListUsers returns all users
func (s *UserStore) ListUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}

	return users
}

// UpdateUserRole updates a user's role
func (s *UserStore) UpdateUserRole(userID, newRole string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate role
	if !validRoles[newRole] {
		return fmt.Errorf("%w: %s", ErrInvalidRole, newRole)
	}

	// Get user
	user, exists := s.users[userID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrUserNotFound, userID)
	}

	// Update role
	user.Role = newRole

	return nil
}

// DeleteUser deletes a user
func (s *UserStore) DeleteUser(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get user
	user, exists := s.users[userID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrUserNotFound, userID)
	}

	// Delete from all maps
	delete(s.users, userID)
	delete(s.usernameMap, user.Username)
	if user.OIDCSubject != "" && user.OIDCIssuer != "" {
		delete(s.oidcSubMap, oidcKey(user.OIDCIssuer, user.OIDCSubject))
	}

	return nil
}

// ChangePassword changes a user's password
func (s *UserStore) ChangePassword(userID, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate password
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	// Get user
	user, exists := s.users[userID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrUserNotFound, userID)
	}

	// Hash new password
	hashedPassword, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPasswordHashFailed, err)
	}

	// Update password
	user.PasswordHash = hashedPassword

	return nil
}

// Helper functions

func validateUsername(username string) error {
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return ErrInvalidUsername
	}

	if !usernameRegex.MatchString(username) {
		return ErrInvalidUsername
	}

	return nil
}

func validatePassword(password string) error {
	if password == "" {
		return ErrEmptyPassword
	}

	if len(password) < MinPasswordLength {
		return ErrWeakPassword
	}

	return nil
}

func hashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}

	return string(hashedBytes), nil
}

// OIDC User Provisioning

// OIDCUserInfo contains OIDC claims for user provisioning
type OIDCUserInfo struct {
	Subject           string // OIDC 'sub' claim (required)
	Issuer            string // OIDC issuer URL (required)
	Email             string
	EmailVerified     bool
	Name              string // Display name
	PreferredUsername string
	Picture           string
	Role              string // Mapped GraphDB role
}

// GetUserByOIDCSubject looks up a user by their OIDC issuer and subject
func (s *UserStore) GetUserByOIDCSubject(issuer, subject string) (*User, error) {
	if issuer == "" || subject == "" {
		return nil, ErrUserNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	userID, exists := s.oidcSubMap[oidcKey(issuer, subject)]
	if !exists {
		return nil, ErrUserNotFound
	}

	user, exists := s.users[userID]
	if !exists {
		return nil, ErrUserNotFound
	}

	return user, nil
}

// CreateOrUpdateOIDCUser creates a new OIDC user or updates an existing one.
// This implements the "auto-provision on first login" pattern.
// Returns the user and whether it was newly created.
func (s *UserStore) CreateOrUpdateOIDCUser(info *OIDCUserInfo, createdAt int64) (*User, bool, error) {
	if info.Subject == "" || info.Issuer == "" {
		return nil, false, errors.New("OIDC subject and issuer are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := oidcKey(info.Issuer, info.Subject)
	isNew := false

	// Check if user already exists
	userID, exists := s.oidcSubMap[key]
	var user *User

	if exists {
		// Update existing user
		user = s.users[userID]
		user.Email = info.Email
		user.EmailVerified = info.EmailVerified
		user.DisplayName = info.Name
		user.Picture = info.Picture
		user.LastLoginAt = createdAt

		// Update role if provided (don't downgrade, only upgrade)
		if info.Role != "" && rolePriority(info.Role) > rolePriority(user.Role) {
			user.Role = info.Role
		}
	} else {
		// Create new user
		isNew = true
		username := s.generateOIDCUsername(info)
		role := info.Role
		if role == "" {
			role = RoleViewer // Default role
		}

		user = &User{
			ID:            uuid.New().String(),
			Username:      username,
			PasswordHash:  "", // OIDC users don't have passwords
			Role:          role,
			AuthProvider:  AuthProviderOIDC,
			OIDCSubject:   info.Subject,
			OIDCIssuer:    info.Issuer,
			Email:         info.Email,
			EmailVerified: info.EmailVerified,
			DisplayName:   info.Name,
			Picture:       info.Picture,
			CreatedAt:     createdAt,
			LastLoginAt:   createdAt,
		}

		// Store user in all maps
		s.users[user.ID] = user
		s.usernameMap[username] = user.ID
		s.oidcSubMap[key] = user.ID
	}

	return user, isNew, nil
}

// generateOIDCUsername creates a username for an OIDC user
// Priority: preferred_username > email (before @) > subject
func (s *UserStore) generateOIDCUsername(info *OIDCUserInfo) string {
	var base string

	if info.PreferredUsername != "" {
		base = info.PreferredUsername
	} else if info.Email != "" {
		// Use part before @ as base
		if idx := indexByte(info.Email, '@'); idx > 0 {
			base = info.Email[:idx]
		} else {
			base = info.Email
		}
	} else {
		base = "oidc_user"
	}

	// Sanitize to valid username characters
	base = sanitizeUsername(base)

	// Ensure minimum length
	if len(base) < MinUsernameLength {
		base = base + "_user"
	}

	// Ensure uniqueness
	username := base
	counter := 1
	for {
		if _, exists := s.usernameMap[username]; !exists {
			return username
		}
		username = fmt.Sprintf("%s_%d", base, counter)
		counter++
	}
}

// sanitizeUsername removes invalid characters from a username
func sanitizeUsername(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, c)
		}
	}
	if len(result) > MaxUsernameLength {
		result = result[:MaxUsernameLength]
	}
	return string(result)
}

// indexByte returns the index of the first byte c in s, or -1 if not found
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// rolePriority returns the priority of a role (higher = more privileged)
func rolePriority(role string) int {
	switch role {
	case RoleAdmin:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}
