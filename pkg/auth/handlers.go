package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultTokenDuration        = 15 * time.Minute
	DefaultRefreshTokenDuration = 7 * 24 * time.Hour
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userStore  *UserStore
	jwtManager *JWTManager
}

// NewAuthHandler creates a new authentication handler
func NewAuthHandler(userStore *UserStore, jwtManager *JWTManager) *AuthHandler {
	return &AuthHandler{
		userStore:  userStore,
		jwtManager: jwtManager,
	}
}

// ServeHTTP implements http.Handler interface for routing
func (h *AuthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Route requests based on path
	switch r.URL.Path {
	case "/auth/login":
		if r.Method == http.MethodPost {
			h.handleLogin(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	case "/auth/refresh":
		if r.Method == http.MethodPost {
			h.handleRefresh(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	case "/auth/register":
		if r.Method == http.MethodPost {
			h.handleRegister(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	case "/auth/me":
		if r.Method == http.MethodGet {
			h.handleMe(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	default:
		h.respondError(w, http.StatusNotFound, "Not found")
	}
}

// Request/Response types

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         UserResponse `json:"user"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type RefreshResponse struct {
	AccessToken string `json:"access_token"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type RegisterResponse struct {
	User UserResponse `json:"user"`
}

type MeResponse struct {
	User UserResponse `json:"user"`
}

type UserResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Handlers

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.Username == "" {
		h.respondError(w, http.StatusBadRequest, "Username is required")
		return
	}
	if req.Password == "" {
		h.respondError(w, http.StatusBadRequest, "Password is required")
		return
	}

	// Get user
	user, err := h.userStore.GetUserByUsername(req.Username)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Verify password
	if !h.userStore.VerifyPassword(user, req.Password) {
		h.respondError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Generate access token
	accessToken, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("Failed to generate access token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	// Generate refresh token
	refreshToken, err := h.jwtManager.GenerateRefreshToken(user.ID)
	if err != nil {
		log.Printf("Failed to generate refresh token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	response := LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: UserResponse{
			ID:       user.ID,
			Username: user.Username,
			Role:     user.Role,
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate request
	if req.RefreshToken == "" {
		h.respondError(w, http.StatusBadRequest, "Refresh token is required")
		return
	}

	// Validate refresh token
	userID, err := h.jwtManager.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	// Get user
	user, err := h.userStore.GetUserByID(userID)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "User not found")
		return
	}

	// Generate new access token
	accessToken, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("Failed to generate access token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	response := RefreshResponse{
		AccessToken: accessToken,
	}

	h.respondJSON(w, http.StatusOK, response)
}

func (h *AuthHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Extract and validate JWT from Authorization header
	claims, err := h.extractAndValidateToken(r)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Only admins can register new users
	if claims.Role != RoleAdmin {
		h.respondError(w, http.StatusForbidden, "Only admins can register users")
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create user
	user, err := h.userStore.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	response := RegisterResponse{
		User: UserResponse{
			ID:       user.ID,
			Username: user.Username,
			Role:     user.Role,
		},
	}

	h.respondJSON(w, http.StatusCreated, response)
}

func (h *AuthHandler) handleMe(w http.ResponseWriter, r *http.Request) {
	// Extract and validate JWT from Authorization header
	claims, err := h.extractAndValidateToken(r)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Get user from store to ensure it still exists
	user, err := h.userStore.GetUserByID(claims.UserID)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "User not found")
		return
	}

	response := MeResponse{
		User: UserResponse{
			ID:       user.ID,
			Username: user.Username,
			Role:     user.Role,
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

// Helper methods

func (h *AuthHandler) extractAndValidateToken(r *http.Request) (*Claims, error) {
	// Get Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	// Extract token (format: "Bearer <token>")
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	token := parts[1]

	// Validate token
	claims, err := h.jwtManager.ValidateToken(token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return claims, nil
}

func (h *AuthHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func (h *AuthHandler) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	}
	h.respondJSON(w, status, response)
}
