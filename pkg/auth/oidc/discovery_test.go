package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscoveryClient_GetDiscovery(t *testing.T) {
	// Create a mock OIDC discovery document
	mockDiscovery := OIDCDiscovery{
		Issuer:                "https://mock.idp.test",
		AuthorizationEndpoint: "https://mock.idp.test/authorize",
		TokenEndpoint:         "https://mock.idp.test/token",
		UserinfoEndpoint:      "https://mock.idp.test/userinfo",
		JWKSUri:               "https://mock.idp.test/.well-known/jwks.json",
		ScopesSupported:       []string{"openid", "profile", "email"},
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name: "Successful discovery fetch",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					t.Errorf("Expected path /.well-known/openid-configuration, got %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(mockDiscovery)
			},
			wantErr: false,
		},
		{
			name: "Discovery endpoint returns 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Not Found"))
			},
			wantErr:    true,
			errContain: "status 404",
		},
		{
			name: "Discovery endpoint returns invalid JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("not valid json"))
			},
			wantErr:    true,
			errContain: "decode discovery document",
		},
		{
			name: "Discovery missing required fields",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				// Return issuer that matches server URL but missing other required fields
				json.NewEncoder(w).Encode(map[string]string{
					"issuer": "http://" + r.Host,
					// Missing jwks_uri, authorization_endpoint, token_endpoint
				})
			},
			wantErr:    true,
			errContain: "missing jwks_uri",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			// Update mock discovery to use server URL
			localDiscovery := mockDiscovery
			localDiscovery.Issuer = server.URL

			// Create test handler that returns correct issuer
			if tt.name == "Successful discovery fetch" {
				server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(localDiscovery)
				})
			}

			client := NewDiscoveryClientWithHTTP(server.Client(), 5*time.Minute)
			ctx := context.Background()

			discovery, err := client.GetDiscovery(ctx, server.URL)

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

			if discovery == nil {
				t.Error("Expected discovery document, got nil")
				return
			}

			if discovery.Issuer != server.URL {
				t.Errorf("Expected issuer %q, got %q", server.URL, discovery.Issuer)
			}
		})
	}
}

func TestDiscoveryClient_Caching(t *testing.T) {
	fetchCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		discovery := OIDCDiscovery{
			Issuer:                "placeholder",
			AuthorizationEndpoint: "placeholder/authorize",
			TokenEndpoint:         "placeholder/token",
			JWKSUri:               "placeholder/.well-known/jwks.json",
		}
		// Set correct issuer dynamically
		discovery.Issuer = "http://" + r.Host
		discovery.AuthorizationEndpoint = "http://" + r.Host + "/authorize"
		discovery.TokenEndpoint = "http://" + r.Host + "/token"
		discovery.JWKSUri = "http://" + r.Host + "/.well-known/jwks.json"

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(discovery)
	}))
	defer server.Close()

	client := NewDiscoveryClientWithHTTP(server.Client(), 1*time.Hour)
	ctx := context.Background()

	// First fetch
	_, err := client.GetDiscovery(ctx, server.URL)
	if err != nil {
		t.Fatalf("First fetch failed: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("Expected 1 fetch, got %d", fetchCount)
	}

	// Second fetch should use cache
	_, err = client.GetDiscovery(ctx, server.URL)
	if err != nil {
		t.Fatalf("Second fetch failed: %v", err)
	}

	if fetchCount != 1 {
		t.Errorf("Expected 1 fetch (cached), got %d", fetchCount)
	}

	// Clear cache and fetch again
	client.ClearCacheForIssuer(server.URL)

	_, err = client.GetDiscovery(ctx, server.URL)
	if err != nil {
		t.Fatalf("Third fetch failed: %v", err)
	}

	if fetchCount != 2 {
		t.Errorf("Expected 2 fetches after cache clear, got %d", fetchCount)
	}
}

func TestValidateDiscovery(t *testing.T) {
	tests := []struct {
		name        string
		discovery   OIDCDiscovery
		issuer      string
		wantErr     bool
		errContains string
	}{
		{
			name: "Valid discovery",
			discovery: OIDCDiscovery{
				Issuer:                "https://idp.example.com",
				AuthorizationEndpoint: "https://idp.example.com/authorize",
				TokenEndpoint:         "https://idp.example.com/token",
				JWKSUri:               "https://idp.example.com/.well-known/jwks.json",
			},
			issuer:  "https://idp.example.com",
			wantErr: false,
		},
		{
			name:        "Missing issuer",
			discovery:   OIDCDiscovery{},
			issuer:      "https://idp.example.com",
			wantErr:     true,
			errContains: "missing issuer",
		},
		{
			name: "Issuer mismatch",
			discovery: OIDCDiscovery{
				Issuer: "https://different.example.com",
			},
			issuer:      "https://idp.example.com",
			wantErr:     true,
			errContains: "issuer mismatch",
		},
		{
			name: "Missing JWKS URI",
			discovery: OIDCDiscovery{
				Issuer: "https://idp.example.com",
			},
			issuer:      "https://idp.example.com",
			wantErr:     true,
			errContains: "missing jwks_uri",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDiscovery(&tt.discovery, tt.issuer)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
