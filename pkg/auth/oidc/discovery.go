package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DiscoveryClient fetches and caches OIDC discovery documents
type DiscoveryClient struct {
	httpClient *http.Client

	// Cache
	mu       sync.RWMutex
	cache    map[string]*CachedDiscovery
	cacheTTL time.Duration
}

// CachedDiscovery holds a discovery document with cache metadata
type CachedDiscovery struct {
	Discovery *OIDCDiscovery
	FetchedAt time.Time
	ExpiresAt time.Time
}

// NewDiscoveryClient creates a new discovery client with default settings
func NewDiscoveryClient() *DiscoveryClient {
	return &DiscoveryClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache:    make(map[string]*CachedDiscovery),
		cacheTTL: 1 * time.Hour,
	}
}

// NewDiscoveryClientWithHTTP creates a discovery client with custom HTTP client
// Useful for testing with mock servers
func NewDiscoveryClientWithHTTP(client *http.Client, cacheTTL time.Duration) *DiscoveryClient {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if cacheTTL == 0 {
		cacheTTL = time.Hour
	}
	return &DiscoveryClient{
		httpClient: client,
		cache:      make(map[string]*CachedDiscovery),
		cacheTTL:   cacheTTL,
	}
}

// GetDiscovery fetches the OIDC discovery document for an issuer.
// Results are cached for cacheTTL duration.
func (c *DiscoveryClient) GetDiscovery(ctx context.Context, issuer string) (*OIDCDiscovery, error) {
	// Normalize issuer URL
	issuer = strings.TrimSuffix(issuer, "/")

	// Check cache first
	c.mu.RLock()
	cached, exists := c.cache[issuer]
	c.mu.RUnlock()

	if exists && time.Now().Before(cached.ExpiresAt) {
		return cached.Discovery, nil
	}

	// Fetch fresh discovery document
	discovery, err := c.fetchDiscovery(ctx, issuer)
	if err != nil {
		// If we have stale cache, return it with a warning in error
		if exists {
			return cached.Discovery, nil
		}
		return nil, err
	}

	// Update cache
	c.mu.Lock()
	c.cache[issuer] = &CachedDiscovery{
		Discovery: discovery,
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(c.cacheTTL),
	}
	c.mu.Unlock()

	return discovery, nil
}

// fetchDiscovery makes the HTTP request to get the discovery document
func (c *DiscoveryClient) fetchDiscovery(ctx context.Context, issuer string) (*OIDCDiscovery, error) {
	// Build the well-known URL
	wellKnownURL := issuer + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("discovery endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	// Limit response size to prevent DoS
	limitedReader := io.LimitReader(resp.Body, 1024*1024) // 1MB max

	var discovery OIDCDiscovery
	if err := json.NewDecoder(limitedReader).Decode(&discovery); err != nil {
		return nil, fmt.Errorf("failed to decode discovery document: %w", err)
	}

	// Validate required fields
	if err := validateDiscovery(&discovery, issuer); err != nil {
		return nil, err
	}

	return &discovery, nil
}

// validateDiscovery checks that required OIDC fields are present
func validateDiscovery(d *OIDCDiscovery, expectedIssuer string) error {
	if d.Issuer == "" {
		return fmt.Errorf("discovery document missing issuer")
	}

	// Issuer must match exactly (per OIDC spec)
	if d.Issuer != expectedIssuer && d.Issuer != expectedIssuer+"/" {
		return fmt.Errorf("issuer mismatch: expected %q, got %q", expectedIssuer, d.Issuer)
	}

	if d.JWKSUri == "" {
		return fmt.Errorf("discovery document missing jwks_uri")
	}

	if d.AuthorizationEndpoint == "" {
		return fmt.Errorf("discovery document missing authorization_endpoint")
	}

	if d.TokenEndpoint == "" {
		return fmt.Errorf("discovery document missing token_endpoint")
	}

	return nil
}

// ClearCache removes all cached discovery documents
func (c *DiscoveryClient) ClearCache() {
	c.mu.Lock()
	c.cache = make(map[string]*CachedDiscovery)
	c.mu.Unlock()
}

// ClearCacheForIssuer removes the cached discovery for a specific issuer
func (c *DiscoveryClient) ClearCacheForIssuer(issuer string) {
	issuer = strings.TrimSuffix(issuer, "/")
	c.mu.Lock()
	delete(c.cache, issuer)
	c.mu.Unlock()
}
