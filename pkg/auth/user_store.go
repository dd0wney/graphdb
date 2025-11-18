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

// User represents a user in the system
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"` // Never serialize password hash
	Role         string `json:"role"`
	CreatedAt    int64  `json:"created_at"`
}

// UserStore manages user storage and authentication
type UserStore struct {
	users       map[string]*User // userID -> User
	usernameMap map[string]string // username -> userID
	mu          sync.RWMutex
}

// NewUserStore creates a new user store
func NewUserStore() *UserStore {
	return &UserStore{
		users:       make(map[string]*User),
		usernameMap: make(map[string]string),
	}
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

	// Delete from both maps
	delete(s.users, userID)
	delete(s.usernameMap, user.Username)

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
