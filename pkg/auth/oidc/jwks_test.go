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
