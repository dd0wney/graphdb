package licensing

import (
	"fmt"
	"log"
	"sync"
)

var (
	// Global license manager (singleton)
	globalManager *Manager
	globalOnce    sync.Once
)

// Manager manages license validation and feature access
type Manager struct {
	client  *Client
	license *LicenseInfo
	mu      sync.RWMutex
}

// InitGlobal initializes the global license manager
func InitGlobal(licenseKey, serverURL string) {
	globalOnce.Do(func() {
		client := NewClient(serverURL)
		globalManager = &Manager{
			client: client,
		}

		// Start background validation
		if licenseKey != "" {
			client.StartBackgroundValidation(licenseKey)
			globalManager.updateLicense()
		} else {
			log.Printf("[License] No license key provided - running in Community mode")
			globalManager.license = &LicenseInfo{
				Valid:  true,
				Tier:   TierCommunity,
				Status: StatusActive,
			}
		}
	})
}

// Global returns the global license manager
func Global() *Manager {
	if globalManager == nil {
		// Initialize with community tier if not initialized
		InitGlobal("", "")
	}
	return globalManager
}

// updateLicense updates the cached license from the client
func (m *Manager) updateLicense() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.license = m.client.GetCurrent()
}

// GetLicense returns the current license
func (m *Manager) GetLicense() *LicenseInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.license != nil {
		return m.license
	}

	// Default to community
	return &LicenseInfo{
		Valid:  true,
		Tier:   TierCommunity,
		Status: StatusActive,
	}
}

// GetTier returns the current license tier
func (m *Manager) GetTier() LicenseTier {
	return m.GetLicense().Tier
}

// IsValid returns whether the current license is valid
func (m *Manager) IsValid() bool {
	return m.GetLicense().IsValid()
}

// IsPro returns whether the license is Pro or higher
func (m *Manager) IsPro() bool {
	return m.GetLicense().IsPro()
}

// IsEnterprise returns whether the license is Enterprise
func (m *Manager) IsEnterprise() bool {
	return m.GetLicense().IsEnterprise()
}

// HasFeature checks if the current license supports a feature
func (m *Manager) HasFeature(feature Feature) bool {
	return m.GetLicense().HasFeature(feature)
}

// RequireFeature panics if the current license doesn't support a feature
// Use this for critical features that should never be accessed without proper licensing
func (m *Manager) RequireFeature(feature Feature) {
	if !m.HasFeature(feature) {
		log.Fatalf("[License] Feature %s requires %s tier, but current tier is %s",
			feature.Name, feature.RequiredTier, m.GetTier())
	}
}

// CheckFeature returns an error if the current license doesn't support a feature
// Use this for graceful feature gating in API handlers
func (m *Manager) CheckFeature(feature Feature) error {
	if !m.HasFeature(feature) {
		return &FeatureNotAvailableError{
			Feature:      feature,
			CurrentTier:  m.GetTier(),
			RequiredTier: feature.RequiredTier,
		}
	}
	return nil
}

// FeatureNotAvailableError is returned when a feature is not available in the current tier
type FeatureNotAvailableError struct {
	Feature      Feature
	CurrentTier  LicenseTier
	RequiredTier LicenseTier
}

func (e *FeatureNotAvailableError) Error() string {
	return fmt.Sprintf("Feature '%s' requires %s tier (current tier: %s). Upgrade at https://graphdb.com/pricing",
		e.Feature.Name, e.RequiredTier, e.CurrentTier)
}

// Stop stops the license manager
func (m *Manager) Stop() {
	if m.client != nil {
		m.client.Stop()
	}
}
