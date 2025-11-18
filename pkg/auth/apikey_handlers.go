package auth

import (
	"encoding/json"
	"net/http"
	"time"
)

// APIKeyHandler handles API key management endpoints
type APIKeyHandler struct {
	store     *APIKeyStore
	userStore *UserStore
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(store *APIKeyStore, userStore *UserStore) *APIKeyHandler {
	return &APIKeyHandler{
		store:     store,
		userStore: userStore,
	}
}

// CreateAPIKeyRequest represents a request to create an API key
type CreateAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	ExpiresIn   int64    `json:"expires_in"` // seconds, 0 = never expires
}

// CreateAPIKeyResponse represents the response when creating an API key
type CreateAPIKeyResponse struct {
	Key    string  `json:"key"` // Only returned once!
	APIKey *APIKey `json:"api_key"`
}

// HandleCreateAPIKey creates a new API key (admin only)
func (h *APIKeyHandler) HandleCreateAPIKey(w http.ResponseWriter, r *http.Request, userID, role string) {
	// Only admins can create API keys
	if role != RoleAdmin {
		respondError(w, http.StatusForbidden, "Only admins can create API keys")
		return
	}

	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Create API key
	var expiresIn time.Duration
	if req.ExpiresIn > 0 {
		expiresIn = time.Duration(req.ExpiresIn) * time.Second
	}

	apiKey, keyString, err := h.store.CreateKey(userID, req.Name, req.Permissions, expiresIn)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	response := CreateAPIKeyResponse{
		Key:    keyString,
		APIKey: apiKey,
	}

	respondJSON(w, http.StatusCreated, response)
}

// HandleListAPIKeys lists API keys for the authenticated user
func (h *APIKeyHandler) HandleListAPIKeys(w http.ResponseWriter, r *http.Request, userID, role string) {
	keys := h.store.ListKeys(userID)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"keys": keys,
	})
}

// HandleRevokeAPIKey revokes an API key
func (h *APIKeyHandler) HandleRevokeAPIKey(w http.ResponseWriter, r *http.Request, keyID, userID, role string) {
	// Get the key
	key, err := h.store.GetKey(keyID)
	if err != nil {
		respondError(w, http.StatusNotFound, "API key not found")
		return
	}

	// Only admins or the key owner can revoke
	if role != RoleAdmin && key.UserID != userID {
		respondError(w, http.StatusForbidden, "You can only revoke your own keys")
		return
	}

	// Revoke key
	err = h.store.RevokeKey(keyID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "API key revoked successfully",
	})
}

// Helper functions

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]interface{}{
		"error":   http.StatusText(status),
		"message": message,
	})
}
