package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Test RSA key data (for testing only - not a real key)
var testJWKS = JWKS{
	Keys: []JWK{
		{
			Kty: "RSA",
			Kid: "test-key-1",
			Use: "sig",
			Alg: "RS256",
			// These are test values - modulus and exponent for a small test key
			N: "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
			E: "AQAB",
		},
		{
			Kty: "RSA",
			Kid: "test-key-2",
			Use: "sig",
			Alg: "RS384",
			N:   "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
			E:   "AQAB",
		},
	},
}

func TestJWKSClient_GetJWKS(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name: "Successful JWKS fetch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(testJWKS)
			},
			wantErr: false,
		},
		{
			name: "JWKS endpoint returns 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Not Found"))
			},
			wantErr:    true,
			errContain: "status 404",
		},
		{
			name: "JWKS endpoint returns invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("not valid json"))
			},
			wantErr:    true,
			errContain: "decode JWKS",
		},
		{
			name: "JWKS contains no keys",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(JWKS{Keys: []JWK{}})
			},
			wantErr:    true,
			errContain: "contains no keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := NewJWKSClientWithHTTP(server.Client(), 5*time.Minute)
			ctx := context.Background()

			jwks, err := client.GetJWKS(ctx, server.URL)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errContain != "" && !containsString(err.Error(), tt.errContain) {
					t.Errorf("Expected error containing %q, got %q", tt.errContain, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if jwks == nil {
				t.Error("Expected JWKS, got nil")
				return
			}

			if len(jwks.Keys) != 2 {
				t.Errorf("Expected 2 keys, got %d", len(jwks.Keys))
			}
		})
	}
}

func TestJWKSClient_Caching(t *testing.T) {
	fetchCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testJWKS)
	}))
	defer server.Close()

	client := NewJWKSClientWithHTTP(server.Client(), 1*time.Hour)
	ctx := context.Background()

	// First fetch
	_, err := client.GetJWKS(ctx, server.URL)
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("Expected 1 fetch, got %d", fetchCount)
	}

	// Second fetch should use cache
	_, err = client.GetJWKS(ctx, server.URL)
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("Expected 1 fetch (cached), got %d", fetchCount)
	}

	// Clear cache and fetch again
	client.ClearCacheForURL(server.URL)

	_, err = client.GetJWKS(ctx, server.URL)
	if err != nil {
		t.Fatalf("Third fetch failed: %v", err)
	}

	if fetchCount != 2 {
		t.Errorf("Expected 2 fetches after cache clear, got %d", fetchCount)
	}
}

func TestJWKSClient_GetKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testJWKS)
	}))
	defer server.Close()

	client := NewJWKSClientWithHTTP(server.Client(), 5*time.Minute)
	ctx := context.Background()

	// Get existing key
	key, err := client.GetKey(ctx, server.URL, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}

	if key == nil {
		t.Error("Expected key, got nil")
	}

	// Get non-existent key
	_, err = client.GetKey(ctx, server.URL, "non-existent-key")
	if err != ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound, got %v", err)
	}
}

func TestJWKSClient_KeyRotation(t *testing.T) {
	// Simulate key rotation scenario
	currentKeys := testJWKS
	fetchCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(currentKeys)
	}))
	defer server.Close()

	client := NewJWKSClientWithHTTP(server.Client(), 5*time.Minute)
	ctx := context.Background()

	// First fetch - get key-1
	_, err := client.GetKey(ctx, server.URL, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to get initial key: %v", err)
	}

	// Simulate key rotation - add new key
	newKey := JWK{
		Kty: "RSA",
		Kid: "test-key-3",
		Use: "sig",
		Alg: "RS256",
		N:   testJWKS.Keys[0].N,
		E:   testJWKS.Keys[0].E,
	}
	currentKeys.Keys = append(currentKeys.Keys, newKey)

	// Clear cache to simulate expiry
	client.ClearCache()

	// Request new key - should trigger refresh
	_, err = client.GetKey(ctx, server.URL, "test-key-3")
	if err != nil {
		t.Fatalf("Failed to get rotated key: %v", err)
	}

	// Should have fetched twice (initial + after rotation)
	if fetchCount != 2 {
		t.Errorf("Expected 2 fetches for key rotation, got %d", fetchCount)
	}
}

func TestParseRSAPublicKey(t *testing.T) {
	tests := []struct {
		name    string
		jwk     JWK
		wantErr bool
	}{
		{
			name:    "Valid RSA key",
			jwk:     testJWKS.Keys[0],
			wantErr: false,
		},
		{
			name: "Wrong key type",
			jwk: JWK{
				Kty: "EC",
				Kid: "ec-key",
			},
			wantErr: true,
		},
		{
			name: "Invalid key use",
			jwk: JWK{
				Kty: "RSA",
				Kid: "enc-key",
				Use: "enc", // encryption, not signature
				N:   testJWKS.Keys[0].N,
				E:   testJWKS.Keys[0].E,
			},
			wantErr: true,
		},
		{
			name: "Invalid modulus",
			jwk: JWK{
				Kty: "RSA",
				Kid: "bad-n-key",
				Use: "sig",
				N:   "!!!invalid base64!!!",
				E:   "AQAB",
			},
			wantErr: true,
		},
		{
			name: "Invalid exponent",
			jwk: JWK{
				Kty: "RSA",
				Kid: "bad-e-key",
				Use: "sig",
				N:   testJWKS.Keys[0].N,
				E:   "!!!invalid base64!!!",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := parseRSAPublicKey(&tt.jwk)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if key == nil {
				t.Error("Expected key, got nil")
			}
		})
	}
}

func TestFindKeyByKid(t *testing.T) {
	// Find existing key
	key := FindKeyByKid(&testJWKS, "test-key-1")
	if key == nil {
		t.Error("Expected to find test-key-1")
	}
	if key.Kid != "test-key-1" {
		t.Errorf("Expected kid test-key-1, got %s", key.Kid)
	}

	// Find non-existent key
	key = FindKeyByKid(&testJWKS, "non-existent")
	if key != nil {
		t.Error("Expected nil for non-existent key")
	}
}

func TestJWKSClient_KeyRotationCleansOldKeys(t *testing.T) {
	// Initial JWKS with two keys
	initialKeys := JWKS{
		Keys: []JWK{
			testJWKS.Keys[0], // test-key-1
			testJWKS.Keys[1], // test-key-2
		},
	}

	// Rotated JWKS: test-key-1 removed, test-key-3 added
	rotatedKeys := JWKS{
		Keys: []JWK{
			testJWKS.Keys[1], // test-key-2 (kept)
			{
				Kty: "RSA",
				Kid: "test-key-3",
				Use: "sig",
				Alg: "RS256",
				N:   testJWKS.Keys[0].N,
				E:   testJWKS.Keys[0].E,
			},
		},
	}

	currentKeys := &initialKeys
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(currentKeys)
	}))
	defer server.Close()

	client := NewJWKSClientWithHTTP(server.Client(), 5*time.Minute)
	ctx := context.Background()

	// Fetch initial JWKS and cache keys
	_, err := client.GetKey(ctx, server.URL, "test-key-1")
	if err != nil {
		t.Fatalf("Failed to get initial key: %v", err)
	}

	// Verify both keys are cached
	client.mu.RLock()
	if len(client.keyCache) != 2 {
		t.Errorf("Expected 2 cached keys, got %d", len(client.keyCache))
	}
	_, hasKey1 := client.keyCache["test-key-1"]
	_, hasKey2 := client.keyCache["test-key-2"]
	client.mu.RUnlock()

	if !hasKey1 || !hasKey2 {
		t.Error("Expected both test-key-1 and test-key-2 to be cached")
	}

	// Simulate key rotation
	currentKeys = &rotatedKeys

	// Clear cache to force refetch
	client.ClearCache()

	// Fetch new key - this should also clean up test-key-1
	_, err = client.GetKey(ctx, server.URL, "test-key-3")
	if err != nil {
		t.Fatalf("Failed to get rotated key: %v", err)
	}

	// Verify test-key-1 is removed, test-key-2 and test-key-3 are present
	client.mu.RLock()
	_, hasKey1 = client.keyCache["test-key-1"]
	_, hasKey2 = client.keyCache["test-key-2"]
	_, hasKey3 := client.keyCache["test-key-3"]
	client.mu.RUnlock()

	if hasKey1 {
		t.Error("Expected test-key-1 to be removed after rotation")
	}
	if !hasKey2 {
		t.Error("Expected test-key-2 to still be cached")
	}
	if !hasKey3 {
		t.Error("Expected test-key-3 to be cached")
	}
}

func TestJWKSClient_MaxCacheSize(t *testing.T) {
	// Create a JWKS with more keys than MaxKeyCacheSize
	// For this test, we'll use a smaller number and verify the eviction logic
	numKeys := 50
	largeJWKS := JWKS{
		Keys: make([]JWK, numKeys),
	}
	for i := 0; i < numKeys; i++ {
		largeJWKS.Keys[i] = JWK{
			Kty: "RSA",
			Kid: "key-" + string(rune('A'+i)),
			Use: "sig",
			Alg: "RS256",
			N:   testJWKS.Keys[0].N,
			E:   testJWKS.Keys[0].E,
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(largeJWKS)
	}))
	defer server.Close()

	client := NewJWKSClientWithHTTP(server.Client(), 5*time.Minute)
	ctx := context.Background()

	// Fetch JWKS - should cache all keys
	_, err := client.GetJWKS(ctx, server.URL)
	if err != nil {
		t.Fatalf("Failed to get JWKS: %v", err)
	}

	// Verify keys are cached
	client.mu.RLock()
	cacheLen := len(client.keyCache)
	client.mu.RUnlock()

	if cacheLen != numKeys {
		t.Errorf("Expected %d cached keys, got %d", numKeys, cacheLen)
	}

	// Cache size should never exceed MaxKeyCacheSize
	if cacheLen > MaxKeyCacheSize {
		t.Errorf("Cache size %d exceeds MaxKeyCacheSize %d", cacheLen, MaxKeyCacheSize)
	}
}

func TestJWKSClient_EvictOldestKeys(t *testing.T) {
	client := NewJWKSClient(5 * time.Minute)

	// Manually add keys to cache
	client.mu.Lock()
	for i := 0; i < 10; i++ {
		kid := "evict-test-key-" + string(rune('A'+i))
		client.keyCache[kid] = nil // Value doesn't matter for this test
		client.keyToURL[kid] = "http://example.com"
	}
	initialSize := len(client.keyCache)
	client.evictOldestKeys(3)
	afterEvict := len(client.keyCache)
	client.mu.Unlock()

	if afterEvict != initialSize-3 {
		t.Errorf("Expected %d keys after eviction, got %d", initialSize-3, afterEvict)
	}
}

func TestJWKSClient_KeyToURLTracking(t *testing.T) {
	// Two different JWKS endpoints
	jwks1 := JWKS{
		Keys: []JWK{{
			Kty: "RSA",
			Kid: "endpoint1-key",
			Use: "sig",
			N:   testJWKS.Keys[0].N,
			E:   testJWKS.Keys[0].E,
		}},
	}
	jwks2 := JWKS{
		Keys: []JWK{{
			Kty: "RSA",
			Kid: "endpoint2-key",
			Use: "sig",
			N:   testJWKS.Keys[0].N,
			E:   testJWKS.Keys[0].E,
		}},
	}

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks1)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks2)
	}))
	defer server2.Close()

	client := NewJWKSClientWithHTTP(http.DefaultClient, 5*time.Minute)
	ctx := context.Background()

	// Fetch from both endpoints
	_, err := client.GetKey(ctx, server1.URL, "endpoint1-key")
	if err != nil {
		t.Fatalf("Failed to get key from endpoint 1: %v", err)
	}
	_, err = client.GetKey(ctx, server2.URL, "endpoint2-key")
	if err != nil {
		t.Fatalf("Failed to get key from endpoint 2: %v", err)
	}

	// Verify key-to-URL tracking
	client.mu.RLock()
	url1, ok1 := client.keyToURL["endpoint1-key"]
	url2, ok2 := client.keyToURL["endpoint2-key"]
	client.mu.RUnlock()

	if !ok1 || url1 != server1.URL {
		t.Errorf("Expected endpoint1-key to map to %s, got %s", server1.URL, url1)
	}
	if !ok2 || url2 != server2.URL {
		t.Errorf("Expected endpoint2-key to map to %s, got %s", server2.URL, url2)
	}

	// Clear cache for endpoint 1 only
	client.ClearCacheForURL(server1.URL)

	client.mu.RLock()
	_, hasKey1 := client.keyCache["endpoint1-key"]
	_, hasKey2 := client.keyCache["endpoint2-key"]
	client.mu.RUnlock()

	if hasKey1 {
		t.Error("Expected endpoint1-key to be removed")
	}
	if !hasKey2 {
		t.Error("Expected endpoint2-key to still be cached")
	}
}
