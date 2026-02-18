package licensing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// Cache TTL (24 hours)
	CacheTTL = 24 * time.Hour

	// Fallback cache TTL (7 days - used when license server is down)
	FallbackCacheTTL = 7 * 24 * time.Hour

	// Revalidation interval (check license server every 24 hours in background)
	RevalidationInterval = 24 * time.Hour

	// Default license server URL
	DefaultLicenseServerURL = "https://license.graphdb.com"

	// DefaultVersion is used when build info is not available
	DefaultVersion = "dev"
)

// getVersion returns the module version from build info
func getVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return DefaultVersion
	}

	// Main module version
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Try to get version from vcs.revision
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			// Return short commit hash
			if len(setting.Value) > 7 {
				return setting.Value[:7]
			}
			return setting.Value
		}
	}

	return DefaultVersion
}

// Client validates licenses against the license server
type Client struct {
	serverURL  string
	httpClient *http.Client
	instanceID string

	// Cache
	mu            sync.RWMutex
	currentLicense *LicenseInfo
	fallbackLicense *LicenseInfo

	// Background validation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewClient creates a new license client
func NewClient(serverURL string) *Client {
	if serverURL == "" {
		serverURL = DefaultLicenseServerURL
	}

	instanceID := uuid.New().String()

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		instanceID: instanceID,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Validate validates a license key and caches the result
func (c *Client) Validate(licenseKey string) (*LicenseInfo, error) {
	// Check cache first
	c.mu.RLock()
	if c.currentLicense != nil && time.Now().Before(c.currentLicense.CachedUntil) {
		cached := c.currentLicense
		c.mu.RUnlock()
		log.Printf("[License] Using cached license (valid until %s)", cached.CachedUntil)
		return cached, nil
	}
	c.mu.RUnlock()

	// Validate with server
	license, err := c.validateWithServer(licenseKey)
	if err != nil {
		log.Printf("[License] Validation failed: %v", err)

		// Try fallback cache
		c.mu.RLock()
		if c.fallbackLicense != nil && time.Now().Before(c.fallbackLicense.CachedUntil) {
			fallback := c.fallbackLicense
			c.mu.RUnlock()
			log.Printf("[License] Using fallback cache (valid until %s)", fallback.CachedUntil)
			return fallback, nil
		}
		c.mu.RUnlock()

		// No cache available, fail open for community features
		log.Printf("[License] No valid cache, failing open to community tier")
		return &LicenseInfo{
			Valid:       true,
			Tier:        TierCommunity,
			Status:      StatusActive,
			ValidatedAt: time.Now(),
			CachedUntil: time.Now().Add(1 * time.Hour), // Short cache for fail-open
		}, nil
	}

	// Update cache
	c.mu.Lock()
	c.currentLicense = license

	// Create separate fallback cache with longer TTL
	fallback := &LicenseInfo{
		Valid:       license.Valid,
		Tier:        license.Tier,
		Status:      license.Status,
		ExpiresAt:   license.ExpiresAt,
		MaxNodes:    license.MaxNodes,
		ValidatedAt: license.ValidatedAt,
		CachedUntil: time.Now().Add(FallbackCacheTTL), // 7 days
	}
	c.fallbackLicense = fallback
	c.mu.Unlock()

	log.Printf("[License] Validated successfully: tier=%s, expires=%v", license.Tier, license.ExpiresAt)

	return license, nil
}

// validateWithServer validates license key with the license server
func (c *Client) validateWithServer(licenseKey string) (*LicenseInfo, error) {
	req := ValidateRequest{
		LicenseKey: licenseKey,
		InstanceID: c.instanceID,
		Version:    getVersion(),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.serverURL+"/validate", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	var validateResp ValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&validateResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if !validateResp.Valid {
		return nil, fmt.Errorf("invalid license: %s", validateResp.Error)
	}

	now := time.Now()
	license := &LicenseInfo{
		Valid:       true,
		Tier:        validateResp.Tier,
		Status:      validateResp.Status,
		ExpiresAt:   validateResp.ExpiresAt,
		MaxNodes:    validateResp.MaxNodes,
		ValidatedAt: now,
		CachedUntil: now.Add(CacheTTL),
	}

	return license, nil
}

// GetCurrent returns the current cached license (if any)
func (c *Client) GetCurrent() *LicenseInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.currentLicense != nil && time.Now().Before(c.currentLicense.CachedUntil) {
		return c.currentLicense
	}

	// Return community tier if no valid cache
	return &LicenseInfo{
		Valid:  true,
		Tier:   TierCommunity,
		Status: StatusActive,
	}
}

// StartBackgroundValidation starts background license re-validation
func (c *Client) StartBackgroundValidation(licenseKey string) {
	if licenseKey == "" {
		log.Printf("[License] No license key provided, running in community mode")
		return
	}

	go c.backgroundValidation(licenseKey)
}

// backgroundValidation periodically re-validates the license
func (c *Client) backgroundValidation(licenseKey string) {
	// Initial validation
	if _, err := c.Validate(licenseKey); err != nil {
		log.Printf("[License] Initial validation failed: %v", err)
	}

	// Revalidate every 24 hours
	ticker := time.NewTicker(RevalidationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("[License] Background validation stopped")
			return

		case <-ticker.C:
			log.Printf("[License] Revalidating license...")
			if _, err := c.Validate(licenseKey); err != nil {
				log.Printf("[License] Revalidation failed: %v", err)
			}
		}
	}
}

// Stop stops background validation
func (c *Client) Stop() {
	c.cancel()
}
