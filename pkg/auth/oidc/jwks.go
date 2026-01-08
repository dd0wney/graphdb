package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

var (
	ErrKeyNotFound   = errors.New("key not found in JWKS")
	ErrInvalidKeyUse = errors.New("key is not for signature verification")
	ErrUnsupportedKty = errors.New("unsupported key type")
)

// JWKSClient fetches and caches JSON Web Key Sets
type JWKSClient struct {
	httpClient *http.Client

	// Cache
	mu       sync.RWMutex
	cache    map[string]*CachedJWKS
	cacheTTL time.Duration

	// Parsed RSA keys cache (kid -> key)
	keyCache map[string]*rsa.PublicKey
}

// NewJWKSClient creates a new JWKS client with default settings
func NewJWKSClient(cacheTTL time.Duration) *JWKSClient {
	if cacheTTL == 0 {
		cacheTTL = DefaultJWKSCacheTTL
	}
	return &JWKSClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache:    make(map[string]*CachedJWKS),
		cacheTTL: cacheTTL,
		keyCache: make(map[string]*rsa.PublicKey),
	}
}

// NewJWKSClientWithHTTP creates a JWKS client with custom HTTP client
// Useful for testing with mock servers
func NewJWKSClientWithHTTP(client *http.Client, cacheTTL time.Duration) *JWKSClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if cacheTTL == 0 {
		cacheTTL = DefaultJWKSCacheTTL
	}
	return &JWKSClient{
		httpClient: client,
		cache:      make(map[string]*CachedJWKS),
		cacheTTL:   cacheTTL,
		keyCache:   make(map[string]*rsa.PublicKey),
	}
}

// GetJWKS fetches the JWKS from the given URL, using cache if available
func (c *JWKSClient) GetJWKS(ctx context.Context, jwksURL string) (*JWKS, error) {
	// Check cache first
	c.mu.RLock()
	cached, exists := c.cache[jwksURL]
	c.mu.RUnlock()

	if exists && time.Now().Before(cached.ExpiresAt) {
		return cached.JWKS, nil
	}

	// Fetch fresh JWKS
	jwks, err := c.fetchJWKS(ctx, jwksURL)
	if err != nil {
		// If we have stale cache, return it (graceful degradation)
		if exists {
			return cached.JWKS, nil
		}
		return nil, err
	}

	// Update cache and pre-parse keys
	now := time.Now()
	c.mu.Lock()
	c.cache[jwksURL] = &CachedJWKS{
		JWKS:      jwks,
		FetchedAt: now,
		ExpiresAt: now.Add(c.cacheTTL),
	}
	// Parse and cache RSA keys
	for _, key := range jwks.Keys {
		if key.Kty == "RSA" && key.Kid != "" {
			if rsaKey, err := parseRSAPublicKey(&key); err == nil {
				c.keyCache[key.Kid] = rsaKey
			}
		}
	}
	c.mu.Unlock()

	return jwks, nil
}

// GetKey retrieves a specific key by kid from the JWKS
// If the key is not found, it will refresh the JWKS once and try again
// (handles key rotation)
func (c *JWKSClient) GetKey(ctx context.Context, jwksURL, kid string) (*rsa.PublicKey, error) {
	// Check key cache first
	c.mu.RLock()
	key, exists := c.keyCache[kid]
	c.mu.RUnlock()

	if exists {
		return key, nil
	}

	// Key not found, fetch/refresh JWKS
	jwks, err := c.fetchJWKS(ctx, jwksURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}

	// Update cache
	now := time.Now()
	c.mu.Lock()
	c.cache[jwksURL] = &CachedJWKS{
		JWKS:      jwks,
		FetchedAt: now,
		ExpiresAt: now.Add(c.cacheTTL),
	}
	// Parse and cache all keys
	for _, jwk := range jwks.Keys {
		if jwk.Kty == "RSA" && jwk.Kid != "" {
			if rsaKey, err := parseRSAPublicKey(&jwk); err == nil {
				c.keyCache[jwk.Kid] = rsaKey
			}
		}
	}
	key, exists = c.keyCache[kid]
	c.mu.Unlock()

	if !exists {
		return nil, ErrKeyNotFound
	}

	return key, nil
}

// fetchJWKS makes the HTTP request to get the JWKS
func (c *JWKSClient) fetchJWKS(ctx context.Context, jwksURL string) (*JWKS, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("JWKS endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	// Limit response size to prevent DoS
	limitedReader := io.LimitReader(resp.Body, 512*1024) // 512KB max for JWKS

	var jwks JWKS
	if err := json.NewDecoder(limitedReader).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("JWKS contains no keys")
	}

	return &jwks, nil
}

// parseRSAPublicKey parses a JWK into an RSA public key
func parseRSAPublicKey(jwk *JWK) (*rsa.PublicKey, error) {
	if jwk.Kty != "RSA" {
		return nil, ErrUnsupportedKty
	}

	// Check key use (should be "sig" for signature verification)
	if jwk.Use != "" && jwk.Use != "sig" {
		return nil, ErrInvalidKeyUse
	}

	// Decode modulus (n)
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode modulus: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)

	// Decode exponent (e)
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode exponent: %w", err)
	}
	// Convert exponent bytes to int
	var e int
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

// ClearCache removes all cached JWKS and keys
func (c *JWKSClient) ClearCache() {
	c.mu.Lock()
	c.cache = make(map[string]*CachedJWKS)
	c.keyCache = make(map[string]*rsa.PublicKey)
	c.mu.Unlock()
}

// ClearCacheForURL removes the cached JWKS for a specific URL
func (c *JWKSClient) ClearCacheForURL(jwksURL string) {
	c.mu.Lock()
	// Remove JWKS cache
	if cached, exists := c.cache[jwksURL]; exists {
		// Also remove associated key cache entries
		for _, key := range cached.JWKS.Keys {
			delete(c.keyCache, key.Kid)
		}
		delete(c.cache, jwksURL)
	}
	c.mu.Unlock()
}

// FindKeyByKid searches for a key by its kid in a JWKS
func FindKeyByKid(jwks *JWKS, kid string) *JWK {
	for i := range jwks.Keys {
		if jwks.Keys[i].Kid == kid {
			return &jwks.Keys[i]
		}
	}
	return nil
}
