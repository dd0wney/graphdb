package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// CreateAPIKeyRequest represents a request to create an API key
type CreateAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
	ExpiresIn   int64    `json:"expires_in,omitempty"` // seconds, 0 = never expires
	Environment string   `json:"environment,omitempty"` // "live" or "test" - defaults to server's GRAPHDB_ENV
}

// CreateAPIKeyResponse represents the response when creating an API key
type CreateAPIKeyResponse struct {
	Key     string      `json:"key"` // Only returned once!
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Prefix  string      `json:"prefix"`
	Expires *time.Time  `json:"expires,omitempty"`
	Created time.Time   `json:"created"`
}

// APIKeyResponse represents an API key in list responses (without the actual key)
type APIKeyResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions"`
	Created     time.Time  `json:"created"`
	Expires     *time.Time `json:"expires,omitempty"`
	LastUsed    *time.Time `json:"last_used,omitempty"`
	Revoked     bool       `json:"revoked"`
}

// handleAPIKeys handles GET (list) and POST (create) for /api/v1/apikeys
func (s *Server) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	// Get claims from context (set by requireAdmin middleware)
	claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
	if !ok {
		s.respondError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleListAPIKeys(w, r, claims.UserID)
	case http.MethodPost:
		s.handleCreateAPIKey(w, r, claims.UserID)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIKey handles DELETE for /api/v1/apikeys/{id}
func (s *Server) handleAPIKey(w http.ResponseWriter, r *http.Request) {
	// Extract key ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apikeys/")
	keyID := strings.TrimSuffix(path, "/")

	if keyID == "" {
		s.respondError(w, http.StatusBadRequest, "API key ID required")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		s.handleRevokeAPIKey(w, r, keyID)
	default:
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleListAPIKeys lists all API keys
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request, userID string) {
	// Admin can see all keys, get all users' keys
	allKeys := make([]*APIKeyResponse, 0)

	// Get all users and their keys
	users := s.userStore.ListUsers()
	for _, user := range users {
		keys := s.apiKeyStore.ListKeys(user.ID)
		for _, key := range keys {
			resp := &APIKeyResponse{
				ID:          key.ID,
				Name:        key.Name,
				Prefix:      key.Prefix,
				Permissions: key.Permissions,
				Created:     key.CreatedAt,
				Revoked:     key.Revoked,
			}
			if !key.ExpiresAt.IsZero() {
				resp.Expires = &key.ExpiresAt
			}
			if !key.LastUsed.IsZero() {
				resp.LastUsed = &key.LastUsed
			}
			allKeys = append(allKeys, resp)
		}
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"keys":  allKeys,
		"count": len(allKeys),
	})
}

// handleCreateAPIKey creates a new API key
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request, userID string) {
	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.respondError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// Default permissions if not specified
	permissions := req.Permissions
	if len(permissions) == 0 {
		permissions = []string{"read", "write"}
	}

	// Convert expiry
	var expiresIn time.Duration
	if req.ExpiresIn > 0 {
		expiresIn = time.Duration(req.ExpiresIn) * time.Second
	}

	// Validate environment if specified
	env := req.Environment
	if env != "" && env != "live" && env != "test" {
		s.respondError(w, http.StatusBadRequest, "Environment must be 'live' or 'test'")
		return
	}

	// Create the API key with specified environment
	apiKey, keyString, err := s.apiKeyStore.CreateKeyWithEnv(userID, req.Name, permissions, expiresIn, env)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Persist to disk
	if err := s.SaveAuthData(); err != nil {
		// Log but don't fail - key was created successfully
		// It will be persisted on next successful save or shutdown
	}

	// Build response
	resp := CreateAPIKeyResponse{
		Key:     keyString,
		ID:      apiKey.ID,
		Name:    apiKey.Name,
		Prefix:  apiKey.Prefix,
		Created: apiKey.CreatedAt,
	}
	if !apiKey.ExpiresAt.IsZero() {
		resp.Expires = &apiKey.ExpiresAt
	}

	s.respondJSON(w, http.StatusCreated, resp)
}

// handleRevokeAPIKey revokes an API key
func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request, keyID string) {
	// Check if key exists
	_, err := s.apiKeyStore.GetKey(keyID)
	if err != nil {
		s.respondError(w, http.StatusNotFound, "API key not found")
		return
	}

	// Revoke the key
	if err := s.apiKeyStore.RevokeKey(keyID); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Persist to disk
	if err := s.SaveAuthData(); err != nil {
		// Log but don't fail - key was revoked successfully
	}

	s.respondJSON(w, http.StatusOK, map[string]any{
		"message": "API key revoked successfully",
		"id":      keyID,
	})
}
