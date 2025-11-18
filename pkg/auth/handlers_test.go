package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthHandler_Login tests the login endpoint
func TestAuthHandler_Login(t *testing.T) {
	store := NewUserStore()
	jwtManager := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	// Create test user
	_, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "Valid login",
			requestBody: map[string]interface{}{
				"username": "alice",
				"password": "AlicePass123!",
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder) {
				var response map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response["access_token"] == nil || response["access_token"].(string) == "" {
					t.Error("Expected non-empty access_token")
				}
				if response["refresh_token"] == nil || response["refresh_token"].(string) == "" {
					t.Error("Expected non-empty refresh_token")
				}
				if response["user"] == nil {
					t.Error("Expected user object in response")
				}
			},
		},
		{
			name: "Wrong password",
			requestBody: map[string]interface{}{
				"username": "alice",
				"password": "WrongPass123!",
			},
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
		{
			name: "Non-existent user",
			requestBody: map[string]interface{}{
				"username": "nonexistent",
				"password": "password",
			},
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
		{
			name: "Empty username",
			requestBody: map[string]interface{}{
				"username": "",
				"password": "password",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
		{
			name: "Empty password",
			requestBody: map[string]interface{}{
				"username": "alice",
				"password": "",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
		{
			name:           "Invalid JSON",
			requestBody:    nil,
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if tt.requestBody != nil {
				body, _ = json.Marshal(tt.requestBody)
			} else {
				body = []byte("invalid json")
			}

			req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

// TestAuthHandler_Refresh tests the refresh token endpoint
func TestAuthHandler_Refresh(t *testing.T) {
	store := NewUserStore()
	jwtManager := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	// Create test user
	user, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate refresh token
	validRefreshToken, err := jwtManager.GenerateRefreshToken(user.ID)
	if err != nil {
		t.Fatalf("Failed to generate refresh token: %v", err)
	}

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "Valid refresh token",
			requestBody: map[string]interface{}{
				"refresh_token": validRefreshToken,
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder) {
				var response map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response["access_token"] == nil || response["access_token"].(string) == "" {
					t.Error("Expected non-empty access_token")
				}
			},
		},
		{
			name: "Invalid refresh token",
			requestBody: map[string]interface{}{
				"refresh_token": "invalid.token.here",
			},
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
		{
			name: "Empty refresh token",
			requestBody: map[string]interface{}{
				"refresh_token": "",
			},
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

// TestAuthHandler_Register tests user registration
func TestAuthHandler_Register(t *testing.T) {
	store := NewUserStore()
	jwtManager := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	// Create admin user for authorization
	adminUser, err := store.CreateUser("admin", "AdminPass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create admin user: %v", err)
	}

	// Generate admin token
	adminToken, err := jwtManager.GenerateToken(adminUser.ID, adminUser.Username, adminUser.Role)
	if err != nil {
		t.Fatalf("Failed to generate admin token: %v", err)
	}

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		authToken      string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "Valid registration by admin",
			requestBody: map[string]interface{}{
				"username": "newuser",
				"password": "NewUserPass123!",
				"role":     RoleViewer,
			},
			authToken:      adminToken,
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder) {
				var response map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				user, ok := response["user"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected user object in response")
				}

				if user["username"] != "newuser" {
					t.Errorf("Expected username 'newuser', got %v", user["username"])
				}
			},
		},
		{
			name: "Missing authorization",
			requestBody: map[string]interface{}{
				"username": "unauthorized",
				"password": "Pass123!",
				"role":     RoleViewer,
			},
			authToken:      "",
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
		{
			name: "Weak password",
			requestBody: map[string]interface{}{
				"username": "weakpass",
				"password": "weak",
				"role":     RoleViewer,
			},
			authToken:      adminToken,
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
		{
			name: "Invalid role",
			requestBody: map[string]interface{}{
				"username": "invalidrole",
				"password": "ValidPass123!",
				"role":     "superadmin",
			},
			authToken:      adminToken,
			expectedStatus: http.StatusBadRequest,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			if tt.authToken != "" {
				req.Header.Set("Authorization", "Bearer "+tt.authToken)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

// TestAuthHandler_Me tests the current user endpoint
func TestAuthHandler_Me(t *testing.T) {
	store := NewUserStore()
	jwtManager := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	// Create test user
	user, err := store.CreateUser("alice", "AlicePass123!", RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate valid token
	validToken, err := jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	tests := []struct {
		name           string
		authToken      string
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:           "Valid token",
			authToken:      validToken,
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder) {
				var response map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				userObj, ok := response["user"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected user object in response")
				}

				if userObj["username"] != "alice" {
					t.Errorf("Expected username 'alice', got %v", userObj["username"])
				}
				if userObj["role"] != RoleAdmin {
					t.Errorf("Expected role %s, got %v", RoleAdmin, userObj["role"])
				}
			},
		},
		{
			name:           "Missing token",
			authToken:      "",
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
		{
			name:           "Invalid token",
			authToken:      "invalid.token.here",
			expectedStatus: http.StatusUnauthorized,
			checkResponse:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)

			if tt.authToken != "" {
				req.Header.Set("Authorization", "Bearer "+tt.authToken)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, rr.Code, rr.Body.String())
			}

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr)
			}
		})
	}
}

// TestAuthHandler_Routes tests that routes are properly configured
func TestAuthHandler_Routes(t *testing.T) {
	store := NewUserStore()
	jwtManager := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	tests := []struct {
		method         string
		path           string
		expectedStatus int
	}{
		{http.MethodPost, "/auth/login", http.StatusBadRequest}, // No body
		{http.MethodPost, "/auth/refresh", http.StatusBadRequest}, // No body
		{http.MethodPost, "/auth/register", http.StatusUnauthorized}, // No auth
		{http.MethodGet, "/auth/me", http.StatusUnauthorized}, // No auth
		{http.MethodGet, "/auth/unknown", http.StatusNotFound}, // Unknown route
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}
