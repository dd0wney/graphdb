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

// extractAndValidateToken extracts and validates JWT from Authorization header
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
	claims, err := h.jwtManager.ValidateToken(r.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	return claims, nil
}

func (h *AuthHandler) respondJSON(w http.ResponseWriter, status int, data any) {
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
