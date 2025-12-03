package licensing

import "time"

// LicenseTier represents the GraphDB license tier
type LicenseTier string

const (
	TierCommunity  LicenseTier = "community"
	TierPro        LicenseTier = "pro"
	TierEnterprise LicenseTier = "enterprise"
)

// LicenseStatus represents the status of a license
type LicenseStatus string

const (
	StatusActive    LicenseStatus = "active"
	StatusSuspended LicenseStatus = "suspended"
	StatusCancelled LicenseStatus = "cancelled"
	StatusExpired   LicenseStatus = "expired"
)

// ValidateRequest is sent to the license server
type ValidateRequest struct {
	LicenseKey string `json:"licenseKey"`
	InstanceID string `json:"instanceId,omitempty"`
	Version    string `json:"version,omitempty"`
}

// ValidateResponse is received from the license server
type ValidateResponse struct {
	Valid     bool           `json:"valid"`
	Tier      LicenseTier    `json:"tier,omitempty"`
	Status    LicenseStatus  `json:"status,omitempty"`
	Error     string         `json:"error,omitempty"`
	ExpiresAt *time.Time     `json:"expiresAt,omitempty"`
	MaxNodes  *int           `json:"maxNodes,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// LicenseInfo represents a validated license with caching info
type LicenseInfo struct {
	Valid       bool
	Tier        LicenseTier
	Status      LicenseStatus
	ExpiresAt   *time.Time
	MaxNodes    *int
	ValidatedAt time.Time
	CachedUntil time.Time
}

// IsValid returns whether the license is valid
func (l *LicenseInfo) IsValid() bool {
	if !l.Valid {
		return false
	}

	// Check expiration
	if l.ExpiresAt != nil && l.ExpiresAt.Before(time.Now()) {
		return false
	}

	// Check status
	if l.Status != StatusActive {
		return false
	}

	return true
}

// IsPro returns whether the license is Pro or higher
func (l *LicenseInfo) IsPro() bool {
	return l.IsValid() && (l.Tier == TierPro || l.Tier == TierEnterprise)
}

// IsEnterprise returns whether the license is Enterprise
func (l *LicenseInfo) IsEnterprise() bool {
	return l.IsValid() && l.Tier == TierEnterprise
}

// HasFeature checks if a license tier supports a feature
func (l *LicenseInfo) HasFeature(feature Feature) bool {
	if !l.IsValid() {
		// Community features are always available
		return feature.RequiredTier == TierCommunity
	}

	// Check if license tier meets or exceeds required tier
	switch l.Tier {
	case TierEnterprise:
		return true // Enterprise has all features
	case TierPro:
		return feature.RequiredTier == TierCommunity || feature.RequiredTier == TierPro
	case TierCommunity:
		return feature.RequiredTier == TierCommunity
	default:
		return false
	}
}
