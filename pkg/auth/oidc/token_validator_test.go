package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// testKeyPair holds an RSA key pair for testing
type testKeyPair struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	kid        string
}

// generateTestKeyPair creates an RSA key pair for testing
func generateTestKeyPair(kid string) (*testKeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &testKeyPair{
		privateKey: privateKey,
		publicKey:  &privateKey.PublicKey,
		kid:        kid,
	}, nil
}

// toJWK converts the public key to a JWK
func (kp *testKeyPair) toJWK() JWK {
	return JWK{
		Kty: "RSA",
		Kid: kp.kid,
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(kp.publicKey.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(kp.publicKey.E)).Bytes()),
	}
}

// createSignedToken creates a signed JWT for testing
func createSignedToken(kp *testKeyPair, claims map[string]any) string {
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

	// Sign with RSA-SHA256
	hash := sha256.Sum256([]byte(signingInput))
	signature, _ := rsa.SignPKCS1v15(rand.Reader, kp.privateKey, crypto.SHA256, hash[:])
	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)

	return signingInput + "." + signatureB64
}

func TestOIDCTokenValidator_ValidateToken(t *testing.T) {
	// Generate test key pair
	keyPair, err := generateTestKeyPair("test-key-1")
	if err != nil {
		t.Fatalf("Failed to generate test key pair: %v", err)
	}

	// Create mock OIDC provider
	var serverURL string
	mux := http.NewServeMux()
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
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		jwks := JWKS{Keys: []JWK{keyPair.toJWK()}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	// Create validator config
	config := &Config{
		Enabled:      true,
		Issuer:       serverURL,
		ClientID:     "test-client-id",
		DefaultRole:  "viewer",
		JWKSCacheTTL: time.Hour,
		RoleMappings: []RoleMapping{
			{ClaimName: "groups", ClaimValues: []string{"admins"}, GraphDBRole: "admin"},
		},
	}

	validator := NewOIDCTokenValidatorWithClients(
		config,
		NewDiscoveryClientWithHTTP(server.Client(), 5*time.Minute),
		NewJWKSClientWithHTTP(server.Client(), 5*time.Minute),
	)

	now := time.Now().Unix()

	tests := []struct {
		name        string
		claims      map[string]any
		wantErr     bool
		errContains string
		wantRole    string
	}{
		{
			name: "Valid token with default role",
			claims: map[string]any{
				"iss":                serverURL,
				"sub":                "user123",
				"aud":                "test-client-id",
				"exp":                now + 3600,
				"iat":                now,
				"email":              "user@example.com",
				"preferred_username": "testuser",
			},
			wantErr:  false,
			wantRole: "viewer",
		},
		{
			name: "Valid token with admin role from groups",
			claims: map[string]any{
				"iss":    serverURL,
				"sub":    "admin123",
				"aud":    "test-client-id",
				"exp":    now + 3600,
				"iat":    now,
				"groups": []string{"admins", "users"},
			},
			wantErr:  false,
			wantRole: "admin",
		},
		{
			name: "Expired token",
			claims: map[string]any{
				"iss": serverURL,
				"sub": "user123",
				"aud": "test-client-id",
				"exp": now - 3600, // Expired
				"iat": now - 7200,
			},
			wantErr:     true,
			errContains: "expired",
		},
		{
			name: "Wrong issuer",
			claims: map[string]any{
				"iss": "https://wrong-issuer.com",
				"sub": "user123",
				"aud": "test-client-id",
				"exp": now + 3600,
				"iat": now,
			},
			wantErr:     true,
			errContains: "issuer",
		},
		{
			name: "Wrong audience",
			claims: map[string]any{
				"iss": serverURL,
				"sub": "user123",
				"aud": "wrong-client-id",
				"exp": now + 3600,
				"iat": now,
			},
			wantErr:     true,
			errContains: "audience",
		},
		{
			name: "Missing subject",
			claims: map[string]any{
				"iss": serverURL,
				"aud": "test-client-id",
				"exp": now + 3600,
				"iat": now,
			},
			wantErr:     true,
			errContains: "subject",
		},
		{
			name: "Array audience with match",
			claims: map[string]any{
				"iss": serverURL,
				"sub": "user123",
				"aud": []string{"other-client", "test-client-id"},
				"exp": now + 3600,
				"iat": now,
			},
			wantErr:  false,
			wantRole: "viewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := createSignedToken(keyPair, tt.claims)
			ctx := context.Background()

			claims, err := validator.ValidateToken(ctx, token)

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
				return
			}

			if claims.Role != tt.wantRole {
				t.Errorf("Expected role %q, got %q", tt.wantRole, claims.Role)
			}
		})
	}
}

func TestOIDCTokenValidator_MalformedTokens(t *testing.T) {
	config := &Config{
		Enabled:      true,
		Issuer:       "https://idp.example.com",
		ClientID:     "test-client-id",
		DefaultRole:  "viewer",
		JWKSCacheTTL: time.Hour,
	}

	validator := NewOIDCTokenValidator(config)
	ctx := context.Background()

	malformedTokens := []struct {
		name  string
		token string
	}{
		{"Empty token", ""},
		{"Single part", "header"},
		{"Two parts", "header.payload"},
		{"Four parts", "header.payload.signature.extra"},
		{"Invalid header base64", "!!!.cGF5bG9hZA.c2lnbmF0dXJl"},
		{"Invalid payload base64", "aGVhZGVy.!!!.c2lnbmF0dXJl"},
		{"Invalid signature base64", "aGVhZGVy.cGF5bG9hZA.!!!"},
	}

	for _, tt := range malformedTokens {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.ValidateToken(ctx, tt.token)
			if err == nil {
				t.Error("Expected error for malformed token, got nil")
			}
		})
	}
}

func TestOIDCTokenValidator_UnsupportedAlgorithm(t *testing.T) {
	config := &Config{
		Enabled:      true,
		Issuer:       "https://idp.example.com",
		ClientID:     "test-client-id",
		DefaultRole:  "viewer",
		JWKSCacheTTL: time.Hour,
	}

	validator := NewOIDCTokenValidator(config)
	ctx := context.Background()

	// Create a token with HS256 (not supported)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user","iss":"https://idp.example.com","aud":"test","exp":9999999999}`))
	token := header + "." + payload + ".fake_signature"

	_, err := validator.ValidateToken(ctx, token)
	if err == nil {
		t.Error("Expected error for unsupported algorithm, got nil")
	}
	if !containsString(err.Error(), "unsupported") {
		t.Errorf("Expected error about unsupported algorithm, got: %v", err)
	}
}

func TestOIDCTokenValidator_ExtractUsername(t *testing.T) {
	config := &Config{
		Enabled:      true,
		Issuer:       "https://idp.example.com",
		ClientID:     "test-client-id",
		DefaultRole:  "viewer",
		JWKSCacheTTL: time.Hour,
	}

	validator := NewOIDCTokenValidator(config)

	tests := []struct {
		name             string
		claims           *IDTokenClaims
		expectedUsername string
	}{
		{
			name: "Prefers preferred_username",
			claims: &IDTokenClaims{
				Subject:           "sub123",
				PreferredUsername: "preferred",
				Email:             "email@example.com",
				Name:              "Full Name",
			},
			expectedUsername: "preferred",
		},
		{
			name: "Falls back to email",
			claims: &IDTokenClaims{
				Subject: "sub123",
				Email:   "email@example.com",
				Name:    "Full Name",
			},
			expectedUsername: "email@example.com",
		},
		{
			name: "Falls back to name",
			claims: &IDTokenClaims{
				Subject: "sub123",
				Name:    "Full Name",
			},
			expectedUsername: "Full Name",
		},
		{
			name: "Falls back to subject",
			claims: &IDTokenClaims{
				Subject: "sub123",
			},
			expectedUsername: "sub123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			username := validator.extractUsername(tt.claims)
			if username != tt.expectedUsername {
				t.Errorf("Expected username %q, got %q", tt.expectedUsername, username)
			}
		})
	}
}

func TestIsSupportedAlgorithm(t *testing.T) {
	supported := []string{"RS256", "RS384", "RS512"}
	unsupported := []string{"HS256", "HS384", "HS512", "ES256", "PS256", "none", ""}

	for _, alg := range supported {
		if !isSupportedAlgorithm(alg) {
			t.Errorf("Expected %s to be supported", alg)
		}
	}

	for _, alg := range unsupported {
		if isSupportedAlgorithm(alg) {
			t.Errorf("Expected %s to be unsupported", alg)
		}
	}
}
