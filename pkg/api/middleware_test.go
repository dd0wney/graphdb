package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	s := &Server{}

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

	// Test with request under limit
	body := bytes.NewReader([]byte(strings.Repeat("a", 500)))
	req := httptest.NewRequest("POST", "/test", body)
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
	rr = httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d", rr.Code)
	}
}
