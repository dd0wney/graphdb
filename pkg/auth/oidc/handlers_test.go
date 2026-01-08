package oidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// setupTestOIDCProvider creates a mock OIDC provider for testing
func setupTestOIDCProvider(t *testing.T) (*httptest.Server, *testKeyPair) {
	t.Helper()

	keyPair, err := generateTestKeyPair("test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate test key pair: %v", err)
	}

	var serverURL string
	mux := http.NewServeMux()

	// Discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discovery := OIDCDiscovery{
			Issuer:                serverURL,
			AuthorizationEndpoint: serverURL + "/authorize",
			TokenEndpoint:         serverURL + "/token",
			JWKSUri:               serverURL + "/.well-known/jwks.json",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discovery)
	})

	// JWKS endpoint
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		jwks := JWKS{Keys: []JWK{keyPair.toJWK()}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	// Token endpoint (for code exchange)
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}

		code := r.FormValue("code")
		if code == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_request",
				"error_description": "missing code",
			})
			return
		}

		if code == "invalid_code" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "invalid_grant",
				"error_description": "Invalid authorization code",
			})
			return
		}

		// Generate valid ID token
		now := time.Now().Unix()
		claims := map[string]any{
			"iss":                serverURL,
			"sub":                "oidc-user-123",
			"aud":                "test-client-id",
			"exp":                now + 3600,
			"iat":                now,
			"email":              "test@example.com",
			"email_verified":     true,
			"preferred_username": "testoidcuser",
			"name":               "Test OIDC User",
		}

		idToken := createSignedToken(keyPair, claims)

		tokenResp := TokenResponse{
			AccessToken:  "access_token_123",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			IDToken:      idToken,
			RefreshToken: "refresh_token_123",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tokenResp)
	})

	server := httptest.NewServer(mux)
	serverURL = server.URL

	return server, keyPair
}

func setupTestHandler(t *testing.T, idpServer *httptest.Server, keyPair *testKeyPair) *OIDCHandler {
	t.Helper()

	config := &Config{
		Enabled:      true,
		Issuer:       idpServer.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scopes:       []string{"openid", "profile", "email"},
		DefaultRole:  "viewer",
		JWKSCacheTTL: 5 * time.Minute,
	}

	userStore := auth.NewUserStore()
	jwtManager, err := auth.NewJWTManager(
		"test-jwt-secret-must-be-32-chars-long",
		15*time.Minute,
		7*24*time.Hour,
	)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	discoveryClient := NewDiscoveryClientWithHTTP(idpServer.Client(), 5*time.Minute)
	jwksClient := NewJWKSClientWithHTTP(idpServer.Client(), 5*time.Minute)
	tokenValidator := NewOIDCTokenValidatorWithClients(config, discoveryClient, jwksClient)

	return NewOIDCHandlerWithClients(
		config,
		discoveryClient,
		tokenValidator,
		userStore,
		jwtManager,
		NewStateStore(),
		idpServer.Client(),
	)
}

func TestOIDCHandler_Login(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
	req.Host = "graphdb.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	// Should redirect to OIDC provider
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 302, got %d: %s", resp.StatusCode, string(body))
	}

	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatal("Expected Location header")
	}

	// Parse the redirect URL
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("Failed to parse redirect URL: %v", err)
	}

	// Verify it's pointing to the IdP authorization endpoint
	if !strings.HasPrefix(redirectURL.String(), idpServer.URL+"/authorize") {
		t.Errorf("Expected redirect to IdP, got: %s", location)
	}

	// Verify required OAuth2 parameters
	params := redirectURL.Query()
	if params.Get("client_id") != "test-client-id" {
		t.Errorf("Expected client_id=test-client-id, got: %s", params.Get("client_id"))
	}
	if params.Get("response_type") != "code" {
		t.Errorf("Expected response_type=code, got: %s", params.Get("response_type"))
	}
	if params.Get("state") == "" {
		t.Error("Expected state parameter")
	}
	if params.Get("scope") == "" {
		t.Error("Expected scope parameter")
	}
}

func TestOIDCHandler_LoginMethodNotAllowed(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// POST is not allowed for /login
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/login", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Callback_Success(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// First, generate a valid state by simulating login
	state, err := handler.stateStore.GenerateState()
	if err != nil {
		t.Fatalf("Failed to generate state: %v", err)
	}

	// Simulate callback from IdP
	callbackURL := "/auth/oidc/callback?code=valid_code&state=" + state
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	req.Host = "graphdb.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response contains tokens
	if loginResp.AccessToken == "" {
		t.Error("Expected access_token in response")
	}
	if loginResp.RefreshToken == "" {
		t.Error("Expected refresh_token in response")
	}
	if loginResp.TokenType != "Bearer" {
		t.Errorf("Expected token_type=Bearer, got: %s", loginResp.TokenType)
	}
	if loginResp.ExpiresIn <= 0 {
		t.Errorf("Expected expires_in > 0, got: %d", loginResp.ExpiresIn)
	}

	// Verify user info
	if loginResp.User.Username == "" {
		t.Error("Expected username in response")
	}
	if loginResp.User.Role != "viewer" {
		t.Errorf("Expected role=viewer, got: %s", loginResp.User.Role)
	}
}

func TestOIDCHandler_Callback_InvalidState(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// Use an invalid state
	callbackURL := "/auth/oidc/callback?code=valid_code&state=invalid_state"
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
		if !strings.Contains(errResp.Message, "state") {
			t.Errorf("Expected error about state, got: %s", errResp.Message)
		}
	}
}

func TestOIDCHandler_Callback_MissingCode(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	state, _ := handler.stateStore.GenerateState()

	// Missing code parameter
	callbackURL := "/auth/oidc/callback?state=" + state
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Callback_IdPError(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// IdP returns an error
	callbackURL := "/auth/oidc/callback?error=access_denied&error_description=User+denied+access"
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Callback_InvalidCode(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	state, _ := handler.stateStore.GenerateState()

	// Use invalid_code which our mock server rejects
	callbackURL := "/auth/oidc/callback?code=invalid_code&state=" + state
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Token_Success(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// Create a valid ID token
	now := time.Now().Unix()
	claims := map[string]any{
		"iss":                idpServer.URL,
		"sub":                "spa-user-456",
		"aud":                "test-client-id",
		"exp":                now + 3600,
		"iat":                now,
		"email":              "spa@example.com",
		"preferred_username": "spauser",
	}
	idToken := createSignedToken(keyPair, claims)

	// POST the token
	body := `{"id_token":"` + idToken + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(respBody))
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if loginResp.AccessToken == "" {
		t.Error("Expected access_token in response")
	}
	if loginResp.User.ID == "" {
		t.Error("Expected user.id in response")
	}
}

func TestOIDCHandler_Token_InvalidToken(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// Create an expired token
	claims := map[string]any{
		"iss": idpServer.URL,
		"sub": "user",
		"aud": "test-client-id",
		"exp": time.Now().Unix() - 3600, // Expired
		"iat": time.Now().Unix() - 7200,
	}
	expiredToken := createSignedToken(keyPair, claims)

	body := `{"id_token":"` + expiredToken + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Token_MissingToken(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_Token_MethodNotAllowed(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// GET is not allowed for /token
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_NotFound(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/unknown", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestOIDCHandler_UserProvisioning(t *testing.T) {
	idpServer, keyPair := setupTestOIDCProvider(t)
	defer idpServer.Close()

	handler := setupTestHandler(t, idpServer, keyPair)

	// Generate state
	state, _ := handler.stateStore.GenerateState()

	// First login - should create user
	callbackURL := "/auth/oidc/callback?code=valid_code&state=" + state
	req := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	req.Host = "graphdb.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("First login failed: %d - %s", resp.StatusCode, string(body))
	}

	var firstResp LoginResponse
	json.NewDecoder(resp.Body).Decode(&firstResp)
	firstUserID := firstResp.User.ID

	// Second login - should update existing user
	state2, _ := handler.stateStore.GenerateState()
	callbackURL2 := "/auth/oidc/callback?code=valid_code&state=" + state2
	req2 := httptest.NewRequest(http.MethodGet, callbackURL2, nil)
	req2.Host = "graphdb.example.com"
	w2 := httptest.NewRecorder()

	handler.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("Second login failed: %d - %s", resp2.StatusCode, string(body))
	}

	var secondResp LoginResponse
	json.NewDecoder(resp2.Body).Decode(&secondResp)

	// Should be the same user
	if secondResp.User.ID != firstUserID {
		t.Errorf("Expected same user ID across logins, got %s and %s", firstUserID, secondResp.User.ID)
	}
}

func TestStateStore_OneTimeUse(t *testing.T) {
	store := NewStateStore()

	state, err := store.GenerateState()
	if err != nil {
		t.Fatalf("Failed to generate state: %v", err)
	}

	// First validation should succeed
	_, valid := store.ValidateAndConsume(state)
	if !valid {
		t.Error("Expected first validation to succeed")
	}

	// Second validation should fail (one-time use)
	_, valid = store.ValidateAndConsume(state)
	if valid {
		t.Error("Expected second validation to fail (state already consumed)")
	}
}

func TestStateStore_DoSProtection(t *testing.T) {
	store := &StateStore{
		states:  make(map[string]*StateEntry),
		ttl:     10 * time.Minute,
		maxSize: 100, // Small limit for testing
	}

	// Generate max+1 states to trigger eviction
	for i := 0; i < 110; i++ {
		_, err := store.GenerateState()
		if err != nil {
			t.Fatalf("Failed to generate state %d: %v", i, err)
		}
	}

	// Should not exceed maxSize (roughly, due to 10% eviction)
	if store.Len() > store.maxSize {
		t.Errorf("Store size %d exceeds maxSize %d", store.Len(), store.maxSize)
	}
}

// Helper to create a signed token (copy from token_validator_test.go)
func createSignedTokenFromKeyPair(kp *testKeyPair, claims map[string]any) string {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": kp.kid,
	}

	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64

	hash := sha256.Sum256([]byte(signingInput))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, kp.privateKey, crypto.SHA256, hash[:])
	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + signatureB64
}
