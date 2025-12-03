package licensing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClient_Validate_Success tests successful license validation
func TestClient_Validate_Success(t *testing.T) {
	// Create mock license server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/validate" {
			t.Errorf("Expected path /validate, got %s", r.URL.Path)
		}

		if r.Method != "POST" {
			t.Errorf("Expected POST method, got %s", r.Method)
		}

		// Send valid response
		expiresAt := time.Now().Add(365 * 24 * time.Hour)
		maxNodes := 10000
		resp := ValidateResponse{
			Valid:     true,
			Tier:      TierPro,
			Status:    StatusActive,
			ExpiresAt: &expiresAt,
			MaxNodes:  &maxNodes,
			Timestamp: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client pointing to mock server
	client := NewClient(server.URL)

	// Validate license
	license, err := client.Validate("test-license-key")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !license.Valid {
		t.Error("Expected valid license")
	}

	if license.Tier != TierPro {
		t.Errorf("Expected Pro tier, got %s", license.Tier)
	}

	if license.Status != StatusActive {
		t.Errorf("Expected active status, got %s", license.Status)
	}

	if license.ExpiresAt == nil {
		t.Error("Expected expiration date")
	}

	if license.MaxNodes == nil || *license.MaxNodes != 10000 {
		t.Error("Expected maxNodes = 10000")
	}
}

// TestClient_Validate_InvalidLicense tests validation of invalid license
func TestClient_Validate_InvalidLicense(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ValidateResponse{
			Valid:     false,
			Error:     "License is expired",
			Timestamp: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// When license is invalid, should fail-open to community tier
	license, err := client.Validate("expired-license-key")
	if err != nil {
		t.Fatalf("Expected fail-open, got error: %v", err)
	}

	if license == nil {
		t.Fatal("Expected community license on fail-open")
	}

	// Should fail-open to community tier
	if license.Tier != TierCommunity {
		t.Errorf("Expected fail-open to community tier, got %s", license.Tier)
	}

	if !license.Valid {
		t.Error("Expected valid community license on fail-open")
	}
}

// TestClient_Validate_Cache tests that validation uses cache
func TestClient_Validate_Cache(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		resp := ValidateResponse{
			Valid:     true,
			Tier:      TierEnterprise,
			Status:    StatusActive,
			Timestamp: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// First validation - should hit server
	license1, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("First Validate() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected 1 server call, got %d", callCount)
	}

	// Second validation - should use cache
	license2, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("Second Validate() error = %v", err)
	}

	if callCount != 1 {
		t.Errorf("Expected cache hit (still 1 call), got %d calls", callCount)
	}

	// Verify both licenses are identical
	if license1.Tier != license2.Tier {
		t.Error("Cached license differs from original")
	}
}

// TestClient_Validate_FailOpen tests fail-open behavior
func TestClient_Validate_FailOpen(t *testing.T) {
	// Server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// Validation should fail-open to community tier
	license, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("Expected fail-open, got error: %v", err)
	}

	if license.Tier != TierCommunity {
		t.Errorf("Expected fail-open to community tier, got %s", license.Tier)
	}

	if !license.Valid {
		t.Error("Expected valid community license on fail-open")
	}

	if license.Status != StatusActive {
		t.Error("Expected active status on fail-open")
	}
}

// TestClient_Validate_ServerUnreachable tests behavior when server is down
func TestClient_Validate_ServerUnreachable(t *testing.T) {
	// Use invalid URL (server doesn't exist)
	client := NewClient("http://localhost:9999")

	license, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("Expected fail-open, got error: %v", err)
	}

	// Should fail-open to community tier
	if license.Tier != TierCommunity {
		t.Errorf("Expected fail-open to community tier, got %s", license.Tier)
	}
}

// TestClient_Validate_FallbackCache tests fallback cache when server is down
func TestClient_Validate_FallbackCache(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount == 1 {
			// First call succeeds
			resp := ValidateResponse{
				Valid:     true,
				Tier:      TierEnterprise,
				Status:    StatusActive,
				Timestamp: time.Now(),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			// Subsequent calls fail
			http.Error(w, "Server down", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// First validation succeeds
	license1, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("First Validate() error = %v", err)
	}

	if license1.Tier != TierEnterprise {
		t.Errorf("Expected Enterprise tier, got %s", license1.Tier)
	}

	// Expire primary cache by manipulating time
	client.mu.Lock()
	client.currentLicense.CachedUntil = time.Now().Add(-1 * time.Hour)
	client.mu.Unlock()

	// Second validation should use fallback cache (within 7 days)
	license2, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("Second Validate() error = %v", err)
	}

	// Should still be Enterprise from fallback cache
	if license2.Tier != TierEnterprise {
		t.Errorf("Expected Enterprise from fallback, got %s", license2.Tier)
	}
}

// TestClient_GetCurrent tests current license retrieval
func TestClient_GetCurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ValidateResponse{
			Valid:     true,
			Tier:      TierPro,
			Status:    StatusActive,
			Timestamp: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// Before validation, should return community
	license := client.GetCurrent()
	if license.Tier != TierCommunity {
		t.Errorf("Expected community tier before validation, got %s", license.Tier)
	}

	// After validation, should return validated license
	_, err := client.Validate("test-key")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	license = client.GetCurrent()
	if license.Tier != TierPro {
		t.Errorf("Expected Pro tier after validation, got %s", license.Tier)
	}
}

// TestClient_NewClient tests client initialization
func TestClient_NewClient(t *testing.T) {
	t.Run("With custom URL", func(t *testing.T) {
		client := NewClient("https://custom.example.com")
		if client.serverURL != "https://custom.example.com" {
			t.Errorf("Expected custom URL, got %s", client.serverURL)
		}
	})

	t.Run("With empty URL uses default", func(t *testing.T) {
		client := NewClient("")
		if client.serverURL != DefaultLicenseServerURL {
			t.Errorf("Expected default URL, got %s", client.serverURL)
		}
	})

	t.Run("Client has instance ID", func(t *testing.T) {
		client := NewClient("")
		if client.instanceID == "" {
			t.Error("Expected instance ID to be set")
		}
	})
}

// TestClient_ConcurrentValidations tests thread-safety
func TestClient_ConcurrentValidations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ValidateResponse{
			Valid:     true,
			Tier:      TierPro,
			Status:    StatusActive,
			Timestamp: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// Launch concurrent validations
	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func() {
			defer func() { done <- true }()
			_, err := client.Validate("test-key")
			if err != nil {
				t.Errorf("Concurrent Validate() error = %v", err)
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}
}

// BenchmarkClient_Validate_CacheHit benchmarks cached validation
func BenchmarkClient_Validate_CacheHit(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ValidateResponse{
			Valid:     true,
			Tier:      TierPro,
			Status:    StatusActive,
			Timestamp: time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// Prime the cache
	client.Validate("test-key")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.Validate("test-key")
	}
}

// BenchmarkClient_GetCurrent benchmarks current license retrieval
func BenchmarkClient_GetCurrent(b *testing.B) {
	client := NewClient("")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = client.GetCurrent()
	}
}
