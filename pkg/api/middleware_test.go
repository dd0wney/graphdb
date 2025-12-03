package api

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/api/middleware"
	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

// TestBodySizeLimitMiddleware tests the request body size limiting middleware
func TestBodySizeLimitMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		bodySize       int
		maxSize        int64
		expectStatus   int
		expectError    bool
		contentType    string
	}{
		{
			name:         "Small request within limit",
			bodySize:     100,
			maxSize:      1024,
			expectStatus: http.StatusOK,
			expectError:  false,
			contentType:  "application/json",
		},
		{
			name:         "Request at exact limit",
			bodySize:     1024,
			maxSize:      1024,
			expectStatus: http.StatusOK,
			expectError:  false,
			contentType:  "application/json",
		},
		{
			name:         "Request exceeds limit",
			bodySize:     2048,
			maxSize:      1024,
			expectStatus: http.StatusRequestEntityTooLarge,
			expectError:  true,
			contentType:  "application/json",
		},
		{
			name:         "Large request exceeds 10MB default",
			bodySize:     11 * 1024 * 1024, // 11MB
			maxSize:      10 * 1024 * 1024, // 10MB
			expectStatus: http.StatusRequestEntityTooLarge,
			expectError:  true,
			contentType:  "application/json",
		},
		{
			name:         "Empty request",
			bodySize:     0,
			maxSize:      1024,
			expectStatus: http.StatusOK,
			expectError:  false,
			contentType:  "application/json",
		},
		{
			name:         "Text content within limit",
			bodySize:     500,
			maxSize:      1024,
			expectStatus: http.StatusOK,
			expectError:  false,
			contentType:  "text/plain",
		},
		{
			name:         "Binary content exceeds limit",
			bodySize:     2048,
			maxSize:      1024,
			expectStatus: http.StatusRequestEntityTooLarge,
			expectError:  true,
			contentType:  "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			s := &Server{}

			// Create a dummy handler that just responds OK
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Try to read the body to trigger the size limit
				buf := new(bytes.Buffer)
				_, _ = buf.ReadFrom(r.Body)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})

			// Wrap with the body size limit middleware
			wrapped := s.bodySizeLimitMiddleware(handler, tt.maxSize)

			// Create request with specified body size
			body := bytes.NewReader(make([]byte, tt.bodySize))
			req := httptest.NewRequest("POST", "/test", body)
			req.Header.Set("Content-Type", tt.contentType)

			// Record response
			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			// Check status code
			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}
		})
	}
}

// TestBodySizeLimitMiddleware_DefaultLimit tests the default 10MB limit
func TestBodySizeLimitMiddleware_DefaultLimit(t *testing.T) {
	s := &Server{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	// Use the convenience method that applies default limit
	wrapped := s.bodySizeLimitMiddleware(handler, 10*1024*1024)

	// Test with 9MB (should pass)
	body := bytes.NewReader(make([]byte, 9*1024*1024))
	req := httptest.NewRequest("POST", "/test", body)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 9MB request to pass, got status %d", rr.Code)
	}

	// Test with 11MB (should fail)
	body = bytes.NewReader(make([]byte, 11*1024*1024))
	req = httptest.NewRequest("POST", "/test", body)
	rr = httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected 11MB request to fail, got status %d", rr.Code)
	}
}

// TestBodySizeLimitMiddleware_MultipleRequests tests that the limit applies per request
func TestBodySizeLimitMiddleware_MultipleRequests(t *testing.T) {
	s := &Server{}

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := s.bodySizeLimitMiddleware(handler, 1024)

	// Send 3 requests under the limit
	for i := 0; i < 3; i++ {
		body := bytes.NewReader(make([]byte, 500))
		req := httptest.NewRequest("POST", "/test", body)
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Request %d failed with status %d", i+1, rr.Code)
		}
	}

	if callCount != 3 {
		t.Errorf("Expected handler to be called 3 times, got %d", callCount)
	}
}

// TestBodySizeLimitMiddleware_GET_Requests tests that GET requests aren't affected
func TestBodySizeLimitMiddleware_GET_Requests(t *testing.T) {
	s := &Server{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := s.bodySizeLimitMiddleware(handler, 1024)

	// GET request should pass through even with no body
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET request failed with status %d", rr.Code)
	}
}

// TestBodySizeLimitMiddleware_ChainedWithOtherMiddleware tests middleware chain integration
func TestBodySizeLimitMiddleware_ChainedWithOtherMiddleware(t *testing.T) {
	s := &Server{
		// Configure CORS for the test
		corsConfig: &CORSConfig{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Content-Type", "Authorization"},
			AllowCredentials: false,
			MaxAge:           86400,
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Chain: logging -> body limit -> cors -> handler
	wrapped := s.loggingMiddleware(
		s.bodySizeLimitMiddleware(
			s.corsMiddleware(handler),
			1024,
		),
	)

	// Test with request under limit (include Origin header for CORS)
	body := bytes.NewReader([]byte(strings.Repeat("a", 500)))
	req := httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK in middleware chain, got %d", rr.Code)
	}

	// Verify CORS headers are still set
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS headers to be set")
	}

	// Test with request over limit
	body = bytes.NewReader([]byte(strings.Repeat("a", 2000)))
	req = httptest.NewRequest("POST", "/test", body)
	req.Header.Set("Origin", "http://example.com")
	rr = httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d", rr.Code)
	}
}

// TestLoggingMiddleware tests request logging middleware
func TestLoggingMiddleware(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	handler := server.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	t.Logf("✓ Logging middleware passed")
}

// TestCORSMiddleware tests CORS headers and OPTIONS method
func TestCORSMiddleware(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Configure CORS to allow all origins for this test
	server.corsConfig = &CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           86400,
	}

	tests := []struct {
		name         string
		method       string
		origin       string
		expectStatus int
		expectCalled bool
		expectCORS   bool
	}{
		{
			name:         "GET request passes through with CORS",
			method:       http.MethodGet,
			origin:       "http://example.com",
			expectStatus: http.StatusOK,
			expectCalled: true,
			expectCORS:   true,
		},
		{
			name:         "POST request passes through with CORS",
			method:       http.MethodPost,
			origin:       "http://example.com",
			expectStatus: http.StatusOK,
			expectCalled: true,
			expectCORS:   true,
		},
		{
			name:         "OPTIONS preflight handled by middleware",
			method:       http.MethodOptions,
			origin:       "http://example.com",
			expectStatus: http.StatusOK,
			expectCalled: false,
			expectCORS:   true,
		},
		{
			name:         "Request without Origin header - no CORS headers",
			method:       http.MethodGet,
			origin:       "",
			expectStatus: http.StatusOK,
			expectCalled: true,
			expectCORS:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			handler := server.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}

			corsHeader := rr.Header().Get("Access-Control-Allow-Origin")
			if tt.expectCORS && corsHeader == "" {
				t.Errorf("Expected CORS header, got none")
			}
			if !tt.expectCORS && corsHeader != "" {
				t.Errorf("Did not expect CORS header, got %q", corsHeader)
			}

			if handlerCalled != tt.expectCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectCalled, handlerCalled)
			}
		})
	}

	t.Logf("✓ CORS middleware tests passed")
}

// TestRequireAuth_JWT tests JWT authentication
func TestRequireAuth_JWT(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test user directly
	username := "testauth"
	password := "testpass123"

	user, err := server.userStore.CreateUser(username, password, auth.RoleViewer)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate a valid token
	validToken, err := server.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	tests := []struct {
		name         string
		authHeader   string
		expectStatus int
		expectCalled bool
	}{
		{
			name:         "Valid JWT token",
			authHeader:   "Bearer " + validToken,
			expectStatus: http.StatusOK,
			expectCalled: true,
		},
		{
			name:         "Invalid JWT token",
			authHeader:   "Bearer invalid.token.here",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
		{
			name:         "Malformed Bearer header",
			authHeader:   "Bearer",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
		{
			name:         "Missing Authorization header",
			authHeader:   "",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
		{
			name:         "Wrong auth scheme",
			authHeader:   "Basic dXNlcjpwYXNz",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			protectedHandler := server.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true

				claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
				if !ok {
					t.Error("Expected claims in context")
				} else if claims.Username != username {
					t.Errorf("Expected username %q in claims, got %q", username, claims.Username)
				}

				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			protectedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}

			if handlerCalled != tt.expectCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectCalled, handlerCalled)
			}
		})
	}

	t.Logf("✓ JWT authentication middleware tests passed")
}

// TestRequireAuth_APIKey tests API key authentication
func TestRequireAuth_APIKey(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	username := "apiuser"
	password := "apipass123"

	// Create a test user directly
	user, err := server.userStore.CreateUser(username, password, auth.RoleViewer)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create an API key for the user
	_, validAPIKey, err := server.apiKeyStore.CreateKey(user.ID, "Test Key", []string{"read", "write"}, 0)
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	tests := []struct {
		name         string
		apiKey       string
		expectStatus int
		expectCalled bool
	}{
		{
			name:         "Valid API key",
			apiKey:       validAPIKey,
			expectStatus: http.StatusOK,
			expectCalled: true,
		},
		{
			name:         "Invalid API key",
			apiKey:       "invalid-key-12345",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
		{
			name:         "Empty API key",
			apiKey:       "",
			expectStatus: http.StatusUnauthorized,
			expectCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled := false
			protectedHandler := server.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tt.apiKey != "" {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			rr := httptest.NewRecorder()
			protectedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}

			if handlerCalled != tt.expectCalled {
				t.Errorf("Expected handler called=%v, got %v", tt.expectCalled, handlerCalled)
			}
		})
	}

	t.Logf("✓ API key authentication middleware tests passed")
}

// TestAuditMiddleware tests audit logging middleware
func TestAuditMiddleware(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		method     string
		path       string
		statusCode int
	}{
		{
			name:       "Create node success",
			method:     http.MethodPost,
			path:       "/nodes",
			statusCode: http.StatusCreated,
		},
		{
			name:       "Read node success",
			method:     http.MethodGet,
			path:       "/nodes/123",
			statusCode: http.StatusOK,
		},
		{
			name:       "Update edge success",
			method:     http.MethodPut,
			path:       "/edges/456",
			statusCode: http.StatusOK,
		},
		{
			name:       "Delete edge failure",
			method:     http.MethodDelete,
			path:       "/edges/789",
			statusCode: http.StatusNotFound,
		},
		{
			name:       "Authentication",
			method:     http.MethodPost,
			path:       "/auth/login",
			statusCode: http.StatusOK,
		},
		{
			name:       "Query execution",
			method:     http.MethodPost,
			path:       "/query",
			statusCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := server.auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("User-Agent", "TestAgent/1.0")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.statusCode {
				t.Errorf("Expected status %d, got %d", tt.statusCode, rr.Code)
			}
		})
	}

	t.Logf("✓ Audit middleware tests passed")
}

// TestAuditMiddleware_WithAuth tests audit middleware with authenticated user
func TestAuditMiddleware_WithAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	claims := &auth.Claims{
		UserID:   "user123",
		Username: "testuser",
		Role:     "admin",
	}

	handler := server.auditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/nodes", nil)
	ctx := context.WithValue(req.Context(), claimsContextKey, claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	t.Logf("✓ Audit middleware with auth context passed")
}

// TestDetermineResourceAndAction tests resource/action classification
func TestDetermineResourceAndAction(t *testing.T) {
	tests := []struct {
		method           string
		path             string
		expectedResource audit.ResourceType
		expectedAction   audit.Action
	}{
		{http.MethodPost, "/nodes", audit.ResourceNode, audit.ActionCreate},
		{http.MethodGet, "/nodes/123", audit.ResourceNode, audit.ActionRead},
		{http.MethodPut, "/nodes/123", audit.ResourceNode, audit.ActionUpdate},
		{http.MethodDelete, "/nodes/123", audit.ResourceNode, audit.ActionDelete},
		{http.MethodPost, "/edges", audit.ResourceEdge, audit.ActionCreate},
		{http.MethodGet, "/edges/456", audit.ResourceEdge, audit.ActionRead},
		{http.MethodPut, "/edges/456", audit.ResourceEdge, audit.ActionUpdate},
		{http.MethodDelete, "/edges/456", audit.ResourceEdge, audit.ActionDelete},
		{http.MethodPost, "/auth/login", audit.ResourceAuth, audit.ActionAuth},
		{http.MethodPost, "/auth/register", audit.ResourceAuth, audit.ActionAuth},
		{http.MethodPost, "/query", audit.ResourceQuery, audit.ActionCreate},
		{http.MethodPost, "/graphql", audit.ResourceQuery, audit.ActionCreate},
		{http.MethodGet, "/graphql", audit.ResourceQuery, audit.ActionRead},
		{http.MethodGet, "/health", audit.ResourceQuery, audit.ActionRead},
		{http.MethodPatch, "/nodes/789", audit.ResourceNode, audit.ActionUpdate},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			resource, action := determineResourceAndAction(tt.method, tt.path)

			if resource != tt.expectedResource {
				t.Errorf("Expected resource %v, got %v", tt.expectedResource, resource)
			}

			if action != tt.expectedAction {
				t.Errorf("Expected action %v, got %v", tt.expectedAction, action)
			}
		})
	}

	t.Logf("✓ determineResourceAndAction tests passed (%d cases)", len(tests))
}

// TestGetIPAddress tests IP address extraction
// SECURITY: Headers are only trusted when request comes from a trusted proxy
func TestGetIPAddress(t *testing.T) {
	// Without trusted proxies configured, headers should NOT be trusted
	tests := []struct {
		name       string
		remoteAddr string
		xRealIP    string
		xForwarded string
		expectedIP string
	}{
		{
			name:       "Direct connection - no port",
			remoteAddr: "192.168.1.100",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "Direct connection - with port",
			remoteAddr: "192.168.1.100:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "X-Real-IP header ignored (untrusted source)",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			expectedIP: "10.0.0.1", // Uses RemoteAddr, not header
		},
		{
			name:       "X-Forwarded-For header ignored (untrusted source)",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "203.0.113.51",
			expectedIP: "10.0.0.1", // Uses RemoteAddr, not header
		},
		{
			name:       "Both headers ignored (untrusted source)",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			xForwarded: "203.0.113.51",
			expectedIP: "10.0.0.1", // Uses RemoteAddr, not header
		},
		{
			name:       "IPv6 address",
			remoteAddr: "[::1]:8080",
			expectedIP: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			if tt.xForwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwarded)
			}

			ip := getIPAddress(req)

			if ip != tt.expectedIP {
				t.Errorf("Expected IP %q, got %q", tt.expectedIP, ip)
			}
		})
	}

	t.Logf("✓ getIPAddress tests passed (%d cases)", len(tests))
}

// TestGetIPAddress_WithTrustedProxy tests IP extraction when request is from a trusted proxy
func TestGetIPAddress_WithTrustedProxy(t *testing.T) {
	// Configure 10.0.0.0/8 as trusted proxy network
	_, network, _ := net.ParseCIDR("10.0.0.0/8")
	trustedProxies := []*net.IPNet{network}

	tests := []struct {
		name       string
		remoteAddr string
		xRealIP    string
		xForwarded string
		expectedIP string
	}{
		{
			name:       "X-Real-IP header trusted from trusted proxy",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			expectedIP: "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For header trusted from trusted proxy",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "203.0.113.51",
			expectedIP: "203.0.113.51",
		},
		{
			name:       "X-Real-IP takes precedence over X-Forwarded-For",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "203.0.113.50",
			xForwarded: "203.0.113.51",
			expectedIP: "203.0.113.50",
		},
		{
			name:       "X-Forwarded-For with multiple IPs uses first",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "203.0.113.51, 10.0.0.2, 10.0.0.3",
			expectedIP: "203.0.113.51",
		},
		{
			name:       "Headers ignored from untrusted source",
			remoteAddr: "192.168.1.1:8080", // Not in 10.0.0.0/8
			xRealIP:    "203.0.113.50",
			expectedIP: "192.168.1.1", // Falls back to RemoteAddr
		},
		{
			name:       "No headers from trusted proxy uses RemoteAddr",
			remoteAddr: "10.0.0.1:8080",
			expectedIP: "10.0.0.1",
		},
		{
			name:       "Invalid X-Real-IP ignored",
			remoteAddr: "10.0.0.1:8080",
			xRealIP:    "not-an-ip",
			expectedIP: "10.0.0.1", // Falls back to RemoteAddr
		},
		{
			name:       "Invalid X-Forwarded-For ignored",
			remoteAddr: "10.0.0.1:8080",
			xForwarded: "not-an-ip, garbage",
			expectedIP: "10.0.0.1", // Falls back to RemoteAddr
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr

			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			if tt.xForwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwarded)
			}

			// Use middleware package's GetClientIPWithProxies for testing with custom proxy list
			ip := middleware.GetClientIPWithProxies(req, trustedProxies)

			if ip != tt.expectedIP {
				t.Errorf("Expected IP %q, got %q", tt.expectedIP, ip)
			}
		})
	}

	t.Logf("✓ getIPAddress with trusted proxy tests passed (%d cases)", len(tests))
}

// TestIsTrustedProxy tests the trusted proxy checking function
func TestIsTrustedProxy(t *testing.T) {
	// Configure multiple trusted networks
	_, network1, _ := net.ParseCIDR("10.0.0.0/8")
	_, network2, _ := net.ParseCIDR("172.16.0.0/12")
	_, network3, _ := net.ParseCIDR("::1/128")
	trustedProxies := []*net.IPNet{network1, network2, network3}

	tests := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{"10.x.x.x is trusted", "10.0.0.1:8080", true},
		{"10.255.255.255 is trusted", "10.255.255.255:443", true},
		{"172.16.x.x is trusted", "172.16.0.1:8080", true},
		{"172.31.255.255 is trusted", "172.31.255.255:80", true},
		{"192.168.x.x is NOT trusted", "192.168.1.1:8080", false},
		{"Public IP is NOT trusted", "203.0.113.50:8080", false},
		{"IPv6 localhost is trusted", "[::1]:8080", true},
		{"Invalid address", "not-an-ip:8080", false},
		{"Empty address", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use middleware package's IsTrustedProxyIn for testing with custom proxy list
			result := middleware.IsTrustedProxyIn(tt.remoteAddr, trustedProxies)
			if result != tt.expected {
				t.Errorf("IsTrustedProxyIn(%q) = %v, want %v", tt.remoteAddr, result, tt.expected)
			}
		})
	}

	// Test with no trusted proxies configured
	if middleware.IsTrustedProxyIn("10.0.0.1:8080", nil) {
		t.Error("With no trusted proxies, IsTrustedProxyIn should return false")
	}

	t.Logf("✓ IsTrustedProxy tests passed")
}

// TestStatusResponseWriter tests the wrapper that captures status codes
func TestStatusResponseWriter(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		expectedStatus int
	}{
		{"Success", http.StatusOK, http.StatusOK},
		{"Created", http.StatusCreated, http.StatusCreated},
		{"Bad Request", http.StatusBadRequest, http.StatusBadRequest},
		{"Not Found", http.StatusNotFound, http.StatusNotFound},
		{"Internal Error", http.StatusInternalServerError, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			wrapper := &statusResponseWriter{
				ResponseWriter: rr,
				statusCode:     http.StatusOK,
			}

			wrapper.WriteHeader(tt.statusCode)

			if wrapper.statusCode != tt.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatus, wrapper.statusCode)
			}

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected underlying recorder status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}

	t.Logf("✓ statusResponseWriter tests passed")
}

// TestMiddleware_Integration tests multiple middlewares working together
func TestMiddleware_Integration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Configure CORS to allow the test origin
	server.corsConfig = &CORSConfig{
		AllowedOrigins:   []string{"http://example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           86400,
	}

	username := "integration"
	password := "intpass123"

	// Create a test user directly
	user, err := server.userStore.CreateUser(username, password, auth.RoleViewer)
	if err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Generate a valid token
	token, err := server.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Stack middlewares: logging -> CORS -> audit -> auth -> handler
	handler := server.loggingMiddleware(
		server.corsMiddleware(
			server.auditMiddleware(
				server.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					claims, ok := r.Context().Value(claimsContextKey).(*auth.Claims)
					if !ok {
						t.Error("Expected claims in context from auth middleware")
					} else if claims.Username != username {
						t.Errorf("Expected username %q, got %q", username, claims.Username)
					}

					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"success": true}`))
				})),
			),
		),
	)

	req := httptest.NewRequest(http.MethodGet, "/protected/resource", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected CORS headers from middleware")
	}

	if !strings.Contains(rr.Body.String(), "success") {
		t.Errorf("Expected success response, got %s", rr.Body.String())
	}

	t.Logf("✓ Middleware integration test passed (logging + CORS + audit + auth)")
}
