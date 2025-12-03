package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

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
