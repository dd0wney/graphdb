package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- BodySizeLimit Tests ---

func TestBodySizeLimit_AllowsSmallRequest(t *testing.T) {
	handler := BodySizeLimit(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader("small body"))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestBodySizeLimit_RejectsLargeContentLength(t *testing.T) {
	handler := BodySizeLimit(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for oversized request")
	}))

	req := httptest.NewRequest("POST", "/", strings.NewReader(""))
	req.ContentLength = 1000 // Larger than limit

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
}

func TestBodySizeLimit_LimitsActualBody(t *testing.T) {
	handler := BodySizeLimit(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MaxBytesReader will cause an error when trying to read too much
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Create request with body larger than limit but no Content-Length header
	largeBody := strings.Repeat("x", 100)
	req := httptest.NewRequest("POST", "/", strings.NewReader(largeBody))
	req.ContentLength = -1 // Unknown content length

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should fail when trying to read
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
}

// --- PanicRecovery Tests ---

func TestPanicRecovery_HandlesNormalRequest(t *testing.T) {
	handler := PanicRecovery()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", rr.Body.String())
	}
}

func TestPanicRecovery_RecoversPanic(t *testing.T) {
	handler := PanicRecovery()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	// Should not expose internal details
	if strings.Contains(rr.Body.String(), "test panic") {
		t.Error("Response should not contain panic message")
	}
}

// --- Logging Tests ---

func TestLogging_WithRequestID(t *testing.T) {
	getRequestID := func(r *http.Request) string {
		return "test-id-123"
	}

	handler := Logging(getRequestID)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestLogging_WithoutRequestID(t *testing.T) {
	handler := Logging(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

// --- RequestID Tests ---

func TestRequestID_GeneratesNew(t *testing.T) {
	var capturedID string
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if capturedID == "" {
		t.Error("Expected request ID to be generated")
	}

	// Should be in response header
	if rr.Header().Get(RequestIDHeader) != capturedID {
		t.Errorf("Response header should contain request ID")
	}
}

func TestRequestID_UsesClientProvided(t *testing.T) {
	var capturedID string
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(RequestIDHeader, "client-provided-id")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if capturedID != "client-provided-id" {
		t.Errorf("Expected 'client-provided-id', got '%s'", capturedID)
	}
}

func TestRequestID_SanitizesInput(t *testing.T) {
	var capturedID string
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(RequestIDHeader, "id<script>alert('xss')</script>")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not contain dangerous characters
	if strings.ContainsAny(capturedID, "<>'\"") {
		t.Errorf("Request ID should be sanitized, got '%s'", capturedID)
	}
}

func TestRequestID_TruncatesLong(t *testing.T) {
	var capturedID string
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = GetRequestID(r)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	longID := strings.Repeat("a", 200)
	req.Header.Set(RequestIDHeader, longID)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if len(capturedID) > 64 {
		t.Errorf("Request ID should be truncated to 64 chars, got %d", len(capturedID))
	}
}

func TestGetRequestID_NoContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	id := GetRequestID(req)
	if id != "" {
		t.Errorf("Expected empty string for request without ID, got '%s'", id)
	}
}

func TestSanitizeRequestID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc123", "abc123"},
		{"abc-123_456.xyz", "abc-123_456.xyz"},
		{"<script>", "script"},
		{"foo bar", "foobar"},
		{"test@email.com", "testemail.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeRequestID(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeRequestID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// --- CORS Tests ---

func TestDefaultCORSConfig(t *testing.T) {
	config := DefaultCORSConfig()

	if config == nil {
		t.Fatal("DefaultCORSConfig returned nil")
	}
	if len(config.AllowedOrigins) != 0 {
		t.Errorf("Expected empty AllowedOrigins, got %v", config.AllowedOrigins)
	}
	if config.MaxAge != 86400 {
		t.Errorf("Expected MaxAge 86400, got %d", config.MaxAge)
	}
}

func TestCORS_AllowedOrigin(t *testing.T) {
	config := &CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("Expected Access-Control-Allow-Origin header")
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials header")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	config := &CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Should not set CORS header for disallowed origin")
	}
}

func TestCORS_WildcardOrigin(t *testing.T) {
	config := &CORSConfig{
		AllowedOrigins: []string{"*"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://any-origin.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://any-origin.com" {
		t.Errorf("Wildcard should allow any origin")
	}
}

func TestCORS_PreflightAllowed(t *testing.T) {
	config := &CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for preflight")
	}))

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d for allowed preflight, got %d", http.StatusOK, rr.Code)
	}
}

func TestCORS_PreflightDisallowed(t *testing.T) {
	config := &CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for preflight")
	}))

	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for disallowed preflight, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestCORS_NilConfig(t *testing.T) {
	handler := CORS(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should not set CORS headers with nil config
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("Should not set CORS header with nil config")
	}
}

// --- SecurityHeaders Tests ---

func TestSecurityHeaders_AllHeaders(t *testing.T) {
	config := &SecurityHeadersConfig{TLSEnabled: true}

	handler := SecurityHeaders(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	expectedHeaders := map[string]string{
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"X-XSS-Protection":        "1; mode=block",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	}

	for header, expected := range expectedHeaders {
		if rr.Header().Get(header) != expected {
			t.Errorf("Header %s: expected '%s', got '%s'", header, expected, rr.Header().Get(header))
		}
	}
}

func TestSecurityHeaders_NoTLS(t *testing.T) {
	config := &SecurityHeadersConfig{TLSEnabled: false}

	handler := SecurityHeaders(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// HSTS should not be set when TLS is disabled
	if rr.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS should not be set when TLS is disabled")
	}
}

func TestSecurityHeaders_NilConfig(t *testing.T) {
	handler := SecurityHeaders(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should still set non-TLS headers
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("Should set X-Frame-Options even with nil config")
	}
}

// --- Metrics Tests ---

type mockMetricsRecorder struct {
	requests      []string
	responseSizes []float64
	inFlight      int
}

func (m *mockMetricsRecorder) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	m.requests = append(m.requests, method+" "+path+" "+status)
}

func (m *mockMetricsRecorder) RecordResponseSize(method, path string, size float64) {
	m.responseSizes = append(m.responseSizes, size)
}

func (m *mockMetricsRecorder) IncHTTPRequestsInFlight() {
	m.inFlight++
}

func (m *mockMetricsRecorder) DecHTTPRequestsInFlight() {
	m.inFlight--
}

func TestMetrics_RecordsRequest(t *testing.T) {
	recorder := &mockMetricsRecorder{}

	handler := Metrics(recorder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if len(recorder.requests) != 1 {
		t.Errorf("Expected 1 recorded request, got %d", len(recorder.requests))
	}
	if recorder.requests[0] != "GET /test 200" {
		t.Errorf("Unexpected recorded request: %s", recorder.requests[0])
	}
}

func TestMetrics_RecordsResponseSize(t *testing.T) {
	recorder := &mockMetricsRecorder{}

	handler := Metrics(recorder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if len(recorder.responseSizes) != 1 || recorder.responseSizes[0] != 5 {
		t.Errorf("Expected response size 5, got %v", recorder.responseSizes)
	}
}

func TestMetrics_TracksInFlight(t *testing.T) {
	recorder := &mockMetricsRecorder{}
	var inFlightDuringRequest int

	handler := Metrics(recorder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inFlightDuringRequest = recorder.inFlight
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if inFlightDuringRequest != 1 {
		t.Errorf("Expected in-flight to be 1 during request, got %d", inFlightDuringRequest)
	}
	if recorder.inFlight != 0 {
		t.Errorf("Expected in-flight to be 0 after request, got %d", recorder.inFlight)
	}
}

func TestMetrics_NilRecorder(t *testing.T) {
	handler := Metrics(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestMetricsResponseWriter_WriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	wrapper := &metricsResponseWriter{
		ResponseWriter: rr,
		statusCode:     http.StatusOK,
	}

	wrapper.WriteHeader(http.StatusNotFound)

	if wrapper.statusCode != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, wrapper.statusCode)
	}
}

// --- RateLimiter Tests ---

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(nil)
	defer rl.Stop()

	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.config.RequestsPerSecond != 100 {
		t.Errorf("Expected default RequestsPerSecond 100, got %f", rl.config.RequestsPerSecond)
	}
}

func TestRateLimiter_AllowBurst(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         5,
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	// Should allow burst size requests
	for i := 0; i < 5; i++ {
		if !rl.Allow("client1") {
			t.Errorf("Request %d should be allowed within burst", i)
		}
	}

	// Next request should be denied
	if rl.Allow("client1") {
		t.Error("Request beyond burst should be denied")
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 1000, // Fast refill
		BurstSize:         1,
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	// Use up the token
	if !rl.Allow("client1") {
		t.Error("First request should be allowed")
	}
	if rl.Allow("client1") {
		t.Error("Second request should be denied")
	}

	// Wait for refill
	time.Sleep(10 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow("client1") {
		t.Error("Request should be allowed after refill")
	}
}

func TestRateLimiter_MaxClients(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         10,
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
		MaxClients:        2,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	// First two clients should be allowed
	if !rl.Allow("client1") {
		t.Error("client1 should be allowed")
	}
	if !rl.Allow("client2") {
		t.Error("client2 should be allowed")
	}

	// Third client should be denied
	if rl.Allow("client3") {
		t.Error("client3 should be denied (max clients reached)")
	}

	// Existing clients should still work
	if !rl.Allow("client1") {
		t.Error("client1 should still be allowed")
	}
}

func TestRateLimiter_GetStats(t *testing.T) {
	config := DefaultRateLimitConfig()
	rl := NewRateLimiter(config)
	defer rl.Stop()

	rl.Allow("client1")
	rl.Allow("client2")

	stats := rl.GetStats()
	if stats["active_clients"].(int) != 2 {
		t.Errorf("Expected 2 active clients, got %v", stats["active_clients"])
	}
}

func TestRateLimiter_GetConfig(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 50,
		BurstSize:         100,
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	returnedConfig := rl.GetConfig()
	if returnedConfig.RequestsPerSecond != 50 {
		t.Errorf("Expected RequestsPerSecond 50, got %f", returnedConfig.RequestsPerSecond)
	}
}

func TestRateLimit_Middleware(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         1,
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	getClientID := func(r *http.Request) string {
		return r.RemoteAddr
	}

	handler := RateLimit(rl, getClientID, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("First request: expected %d, got %d", http.StatusOK, rr1.Code)
	}

	// Second request should be rate limited
	req2 := httptest.NewRequest("GET", "/", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request: expected %d, got %d", http.StatusTooManyRequests, rr2.Code)
	}
	if rr2.Header().Get("Retry-After") != "1" {
		t.Error("Expected Retry-After header")
	}
}

func TestRateLimit_NilLimiter(t *testing.T) {
	handler := RateLimit(nil, func(r *http.Request) string { return "" }, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected %d with nil limiter, got %d", http.StatusOK, rr.Code)
	}
}

func TestRateLimit_CustomOnLimited(t *testing.T) {
	config := &RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         0, // Always deny
		CleanupInterval:   time.Hour,
		ClientExpiration:  time.Hour,
	}

	rl := NewRateLimiter(config)
	defer rl.Stop()

	customCalled := false
	onLimited := func(w http.ResponseWriter, r *http.Request, clientID string) {
		customCalled = true
	}

	handler := RateLimit(rl, func(r *http.Request) string { return "test" }, onLimited)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Handler should not be called when rate limited")
		}),
	)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !customCalled {
		t.Error("Custom onLimited handler should be called")
	}
}

// --- InputValidation Tests ---

func TestDefaultInputValidationConfig(t *testing.T) {
	config := DefaultInputValidationConfig()
	if config == nil {
		t.Fatal("DefaultInputValidationConfig returned nil")
	}
	if config.MaxBodySize != 10*1024*1024 {
		t.Errorf("Expected MaxBodySize 10MB, got %d", config.MaxBodySize)
	}
}

func TestInputValidation_SkipsGET(t *testing.T) {
	config := &InputValidationConfig{
		SkipPaths:   []string{},
		MaxBodySize: 1024,
		ValidateAll: false,
	}

	handler := InputValidation(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET should skip validation, got status %d", rr.Code)
	}
}

func TestInputValidation_SkipsPath(t *testing.T) {
	config := &InputValidationConfig{
		SkipPaths:   []string{"/auth/"},
		MaxBodySize: 1024,
		ValidateAll: true,
	}

	handler := InputValidation(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader([]byte("any body")))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Skipped path should pass, got status %d", rr.Code)
	}
}

func TestInputValidation_ValidInput(t *testing.T) {
	config := &InputValidationConfig{
		SkipPaths:   []string{},
		MaxBodySize: 1024,
		ValidateAll: false,
	}

	handler := InputValidation(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader(`{"name": "test"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Valid input should pass, got status %d", rr.Code)
	}
}

func TestInputValidation_PathTraversal(t *testing.T) {
	config := &InputValidationConfig{
		SkipPaths:   []string{},
		MaxBodySize: 1024,
		ValidateAll: false,
	}

	handler := InputValidation(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for path traversal attempt")
	}))

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader(`{"path": "../../../etc/passwd"}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Path traversal should be rejected, got status %d", rr.Code)
	}
}

func TestInputValidation_EmptyBody(t *testing.T) {
	config := &InputValidationConfig{
		SkipPaths:   []string{},
		MaxBodySize: 1024,
		ValidateAll: false,
	}

	handler := InputValidation(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader(""))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Empty body should pass, got status %d", rr.Code)
	}
}

func TestInputValidation_NilConfig(t *testing.T) {
	handler := InputValidation(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/data", strings.NewReader(`{"valid": "data"}`))
	rr := httptest.NewRecorder()

	// Should use default config and not panic
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Valid request with nil config should pass, got status %d", rr.Code)
	}
}
