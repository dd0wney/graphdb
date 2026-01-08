package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// UserManagementHandler handles user management endpoints
type UserManagementHandler struct {
	userStore  *UserStore
	jwtManager *JWTManager
}

// NewUserManagementHandler creates a new user management handler
func NewUserManagementHandler(userStore *UserStore, jwtManager *JWTManager) *UserManagementHandler {
	return &UserManagementHandler{
		userStore:  userStore,
		jwtManager: jwtManager,
	}
}

// ServeHTTP implements http.Handler interface for routing
func (h *UserManagementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// All user management endpoints require authentication and admin role
	claims, err := h.extractAndValidateToken(r)
	if err != nil {
		h.respondError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if claims.Role != RoleAdmin {
		h.respondError(w, http.StatusForbidden, "Admin access required")
		return
	}

	// Route requests based on path
	path := r.URL.Path

	switch {
	case path == "/api/users" && r.Method == http.MethodGet:
		h.handleListUsers(w, r)
	case path == "/api/users" && r.Method == http.MethodPost:
		h.handleCreateUser(w, r)
	case strings.HasPrefix(path, "/api/users/") && r.Method == http.MethodGet:
		h.handleGetUser(w, r)
	case strings.HasPrefix(path, "/api/users/") && r.Method == http.MethodPut:
		h.handleUpdateUser(w, r)
	case strings.HasPrefix(path, "/api/users/") && r.Method == http.MethodDelete:
		h.handleDeleteUser(w, r)
	case strings.HasSuffix(path, "/password") && r.Method == http.MethodPut:
		h.handleChangePassword(w, r)
	default:
		h.respondError(w, http.StatusNotFound, "Not found")
	}
}

// extractUserID extracts user ID from URL path
func extractUserID(path string) string {
	// Path format: /api/users/{id} or /api/users/{id}/password
	parts := strings.Split(strings.TrimPrefix(path, "/api/users/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// handleListUsers returns all users
func (h *UserManagementHandler) handleListUsers(w http.ResponseWriter, _ *http.Request) {
	users := h.userStore.ListUsers()

	response := make([]UserListItem, 0, len(users))
	for _, user := range users {
		response = append(response, UserListItem{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		})
	}

	h.respondJSON(w, http.StatusOK, ListUsersResponse{Users: response})
}

// handleCreateUser creates a new user
func (h *UserManagementHandler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
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
	if req.Role == "" {
		req.Role = RoleViewer // Default role
	}

	// Create user
	user, err := h.userStore.CreateUser(req.Username, req.Password, req.Role)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create user: %v", err))
		return
	}

	response := CreateUserResponse{
		User: UserListItem{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	}

	h.respondJSON(w, http.StatusCreated, response)
}

// handleGetUser returns a specific user
func (h *UserManagementHandler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r.URL.Path)
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	user, err := h.userStore.GetUserByID(userID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "User not found")
		return
	}

	response := GetUserResponse{
		User: UserListItem{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

// handleUpdateUser updates a user's role
func (h *UserManagementHandler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r.URL.Path)
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Update role if provided
	if req.Role != "" {
		if err := h.userStore.UpdateUserRole(userID, req.Role); err != nil {
			h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to update user: %v", err))
			return
		}
	}

	// Get updated user
	user, err := h.userStore.GetUserByID(userID)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "User not found")
		return
	}

	response := UpdateUserResponse{
		User: UserListItem{
			ID:        user.ID,
			Username:  user.Username,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	}

	h.respondJSON(w, http.StatusOK, response)
}

// handleDeleteUser deletes a user
func (h *UserManagementHandler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r.URL.Path)
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	if err := h.userStore.DeleteUser(userID); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to delete user: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, DeleteUserResponse{Success: true})
}

// handleChangePassword changes a user's password
func (h *UserManagementHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r.URL.Path)
	if userID == "" {
		h.respondError(w, http.StatusBadRequest, "User ID is required")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.NewPassword == "" {
		h.respondError(w, http.StatusBadRequest, "New password is required")
		return
	}

	if err := h.userStore.ChangePassword(userID, req.NewPassword); err != nil {
		h.respondError(w, http.StatusBadRequest, fmt.Sprintf("Failed to change password: %v", err))
		return
	}

	h.respondJSON(w, http.StatusOK, ChangePasswordResponse{Success: true})
}

// Helper methods

func (h *UserManagementHandler) extractAndValidateToken(r *http.Request) (*Claims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, fmt.Errorf("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, fmt.Errorf("invalid authorization header format")
	}

	token := parts[1]
	claims, err := h.jwtManager.ValidateToken(r.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return claims, nil
}

func (h *UserManagementHandler) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *UserManagementHandler) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	}
	h.respondJSON(w, status, response)
}

// Request/Response types

// UserListItem represents a user in list responses
type UserListItem struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"created_at"`
}

// ListUsersResponse is the response for listing users
type ListUsersResponse struct {
	Users []UserListItem `json:"users"`
}

// CreateUserRequest is the request for creating a user
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUserResponse is the response for creating a user
type CreateUserResponse struct {
	User UserListItem `json:"user"`
}

// GetUserResponse is the response for getting a user
type GetUserResponse struct {
	User UserListItem `json:"user"`
}

// UpdateUserRequest is the request for updating a user
type UpdateUserRequest struct {
	Role string `json:"role"`
}

// UpdateUserResponse is the response for updating a user
type UpdateUserResponse struct {
	User UserListItem `json:"user"`
}

// DeleteUserResponse is the response for deleting a user
type DeleteUserResponse struct {
	Success bool `json:"success"`
}

// ChangePasswordRequest is the request for changing password
type ChangePasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// ChangePasswordResponse is the response for changing password
type ChangePasswordResponse struct {
	Success bool `json:"success"`
}
