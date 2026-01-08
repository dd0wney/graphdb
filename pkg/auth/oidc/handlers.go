package oidc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// OIDCHandler handles OIDC authentication endpoints
type OIDCHandler struct {
	config          *Config
	discoveryClient *DiscoveryClient
	tokenValidator  *OIDCTokenValidator
	userStore       *auth.UserStore
	jwtManager      *auth.JWTManager
	stateStore      *StateStore
	httpClient      *http.Client
}

// NewOIDCHandler creates a new OIDC authentication handler
func NewOIDCHandler(
	config *Config,
	userStore *auth.UserStore,
	jwtManager *auth.JWTManager,
) *OIDCHandler {
	return &OIDCHandler{
		config:          config,
		discoveryClient: NewDiscoveryClient(),
		tokenValidator:  NewOIDCTokenValidator(config),
		userStore:       userStore,
		jwtManager:      jwtManager,
		stateStore:      NewStateStore(),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewOIDCHandlerWithClients creates an OIDC handler with custom clients (for testing)
func NewOIDCHandlerWithClients(
	config *Config,
	discoveryClient *DiscoveryClient,
	tokenValidator *OIDCTokenValidator,
	userStore *auth.UserStore,
	jwtManager *auth.JWTManager,
	stateStore *StateStore,
	httpClient *http.Client,
) *OIDCHandler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OIDCHandler{
		config:          config,
		discoveryClient: discoveryClient,
		tokenValidator:  tokenValidator,
		userStore:       userStore,
		jwtManager:      jwtManager,
		stateStore:      stateStore,
		httpClient:      httpClient,
	}
}

// ServeHTTP implements http.Handler interface for routing
func (h *OIDCHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip prefix to get the action
	path := strings.TrimPrefix(r.URL.Path, "/auth/oidc")

	switch path {
	case "/login", "/login/":
		if r.Method == http.MethodGet {
			h.handleLogin(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	case "/callback", "/callback/":
		if r.Method == http.MethodGet {
			h.handleCallback(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	case "/token", "/token/":
		if r.Method == http.MethodPost {
			h.handleToken(w, r)
		} else {
			h.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
	default:
		h.respondError(w, http.StatusNotFound, "Not found")
	}
}

// handleLogin initiates the OIDC authorization code flow
// GET /auth/oidc/login
func (h *OIDCHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Fetch OIDC discovery document
	discovery, err := h.discoveryClient.GetDiscovery(ctx, h.config.Issuer)
	if err != nil {
		log.Printf("OIDC: Failed to fetch discovery document: %v", err)
		h.respondError(w, http.StatusServiceUnavailable, "OIDC provider unavailable")
		return
	}

	// Generate CSRF state
	state, err := h.stateStore.GenerateState()
	if err != nil {
		log.Printf("OIDC: Failed to generate state: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to initiate login")
		return
	}

	// Build authorization URL
	authURL, err := h.buildAuthorizationURL(r, discovery, state)
	if err != nil {
		log.Printf("OIDC: Failed to build authorization URL: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to initiate login")
		return
	}

	// Redirect to OIDC provider
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleCallback handles the OIDC callback after user authentication
// GET /auth/oidc/callback?code=...&state=...
func (h *OIDCHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check for error response from IdP
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		errDesc := r.URL.Query().Get("error_description")
		log.Printf("OIDC: Provider returned error: %s - %s", errCode, errDesc)
		h.respondError(w, http.StatusUnauthorized, fmt.Sprintf("Authentication failed: %s", errDesc))
		return
	}

	// Validate state parameter (CSRF protection)
	state := r.URL.Query().Get("state")
	stateEntry, valid := h.stateStore.ValidateAndConsume(state)
	if !valid {
		log.Printf("OIDC: Invalid or expired state parameter")
		h.respondError(w, http.StatusBadRequest, "Invalid or expired state parameter")
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		h.respondError(w, http.StatusBadRequest, "Missing authorization code")
		return
	}

	// Exchange code for tokens
	tokenResponse, err := h.exchangeCode(ctx, r, code)
	if err != nil {
		log.Printf("OIDC: Failed to exchange code: %v", err)
		h.respondError(w, http.StatusUnauthorized, "Failed to exchange authorization code")
		return
	}

	// Validate ID token
	claims, err := h.tokenValidator.ValidateToken(ctx, tokenResponse.IDToken)
	if err != nil {
		log.Printf("OIDC: Invalid ID token: %v", err)
		h.respondError(w, http.StatusUnauthorized, "Invalid ID token")
		return
	}

	// Get full OIDC claims for user provisioning
	oidcClaims, err := h.tokenValidator.GetOIDCClaims(ctx, tokenResponse.IDToken)
	if err != nil {
		log.Printf("OIDC: Failed to parse OIDC claims: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to process user information")
		return
	}

	// Validate nonce if present in state entry
	if stateEntry.Nonce != "" && oidcClaims.Nonce != "" {
		if stateEntry.Nonce != oidcClaims.Nonce {
			log.Printf("OIDC: Nonce mismatch")
			h.respondError(w, http.StatusUnauthorized, "Security validation failed")
			return
		}
	}

	// Provision or update user
	user, _, err := h.provisionUser(oidcClaims, claims.Role)
	if err != nil {
		log.Printf("OIDC: Failed to provision user: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to provision user")
		return
	}

	// Issue local JWT
	accessToken, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("OIDC: Failed to generate access token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	refreshToken, err := h.jwtManager.GenerateRefreshToken(user.ID)
	if err != nil {
		log.Printf("OIDC: Failed to generate refresh token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	// Return tokens
	h.respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.jwtManager.GetTokenDuration().Seconds()),
		User: UserInfo{
			ID:          user.ID,
			Username:    user.Username,
			Role:        user.Role,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}

// handleToken validates an OIDC ID token directly (SPA flow)
// POST /auth/oidc/token
// Body: {"id_token": "..."}
func (h *OIDCHandler) handleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req TokenValidationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.IDToken == "" {
		h.respondError(w, http.StatusBadRequest, "id_token is required")
		return
	}

	// Validate the OIDC ID token
	claims, err := h.tokenValidator.ValidateToken(ctx, req.IDToken)
	if err != nil {
		log.Printf("OIDC: Invalid ID token: %v", err)
		h.respondError(w, http.StatusUnauthorized, fmt.Sprintf("Invalid ID token: %v", err))
		return
	}

	// Get full OIDC claims for user provisioning
	oidcClaims, err := h.tokenValidator.GetOIDCClaims(ctx, req.IDToken)
	if err != nil {
		log.Printf("OIDC: Failed to parse OIDC claims: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to process user information")
		return
	}

	// Provision or update user
	user, _, err := h.provisionUser(oidcClaims, claims.Role)
	if err != nil {
		log.Printf("OIDC: Failed to provision user: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to provision user")
		return
	}

	// Issue local JWT
	accessToken, err := h.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		log.Printf("OIDC: Failed to generate access token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	refreshToken, err := h.jwtManager.GenerateRefreshToken(user.ID)
	if err != nil {
		log.Printf("OIDC: Failed to generate refresh token: %v", err)
		h.respondError(w, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	// Return tokens
	h.respondJSON(w, http.StatusOK, LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(h.jwtManager.GetTokenDuration().Seconds()),
		User: UserInfo{
			ID:          user.ID,
			Username:    user.Username,
			Role:        user.Role,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		},
	})
}

// buildAuthorizationURL constructs the OIDC authorization URL
func (h *OIDCHandler) buildAuthorizationURL(r *http.Request, discovery *OIDCDiscovery, state string) (string, error) {
	authURL, err := url.Parse(discovery.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization endpoint: %w", err)
	}

	// Determine redirect URI
	redirectURI := h.config.RedirectURI
	if redirectURI == "" {
		// Auto-detect from request
		scheme := "https"
		if r.TLS == nil && !isForwardedHTTPS(r) {
			scheme = "http"
		}
		redirectURI = fmt.Sprintf("%s://%s/auth/oidc/callback", scheme, r.Host)
	}

	// Build query parameters
	params := url.Values{
		"client_id":     {h.config.ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(h.config.Scopes, " ")},
		"state":         {state},
	}

	authURL.RawQuery = params.Encode()
	return authURL.String(), nil
}

// exchangeCode exchanges an authorization code for tokens
func (h *OIDCHandler) exchangeCode(ctx context.Context, r *http.Request, code string) (*TokenResponse, error) {
	// Get discovery document for token endpoint
	discovery, err := h.discoveryClient.GetDiscovery(ctx, h.config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to get discovery: %w", err)
	}

	// Determine redirect URI (must match the one used in authorization request)
	redirectURI := h.config.RedirectURI
	if redirectURI == "" {
		scheme := "https"
		if r.TLS == nil && !isForwardedHTTPS(r) {
			scheme = "http"
		}
		redirectURI = fmt.Sprintf("%s://%s/auth/oidc/callback", scheme, r.Host)
	}

	// Build token request
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {h.config.ClientID},
	}

	// Add client secret if configured (confidential client)
	if h.config.ClientSecret != "" {
		data.Set("client_secret", h.config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.TokenEndpoint, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024)) // 1MB max
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("token endpoint error: %s - %s", errResp.Error, errResp.Description)
		}
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.IDToken == "" {
		return nil, fmt.Errorf("token response missing id_token")
	}

	return &tokenResp, nil
}

// provisionUser creates or updates a user from OIDC claims
func (h *OIDCHandler) provisionUser(claims *IDTokenClaims, role string) (*auth.User, bool, error) {
	info := &auth.OIDCUserInfo{
		Subject:           claims.Subject,
		Issuer:            claims.Issuer,
		Email:             claims.Email,
		EmailVerified:     claims.EmailVerified,
		Name:              claims.Name,
		PreferredUsername: claims.PreferredUsername,
		Picture:           claims.Picture,
		Role:              role,
	}

	return h.userStore.CreateOrUpdateOIDCUser(info, time.Now().Unix())
}

// isForwardedHTTPS checks if the request was forwarded over HTTPS
func isForwardedHTTPS(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-Proto") == "https" ||
		r.Header.Get("X-Forwarded-Ssl") == "on"
}

// Response types

// LoginResponse is returned after successful OIDC authentication
type LoginResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	TokenType    string   `json:"token_type"`
	ExpiresIn    int      `json:"expires_in"`
	User         UserInfo `json:"user"`
}

// UserInfo contains user details in the login response
type UserInfo struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// TokenValidationRequest is the request body for POST /auth/oidc/token
type TokenValidationRequest struct {
	IDToken string `json:"id_token"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Helper methods

func (h *OIDCHandler) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func (h *OIDCHandler) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
	}
	h.respondJSON(w, status, response)
}
