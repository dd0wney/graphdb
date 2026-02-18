package licensing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// LicenseType represents the license tier
type LicenseType string

const (
	LicenseTypeProfessional LicenseType = "professional"
	LicenseTypeEnterprise   LicenseType = "enterprise"
)

// License represents a GraphDB license
type License struct {
	ID           string      `json:"id"`
	Key          string      `json:"key"`
	Type         LicenseType `json:"type"`
	Email        string      `json:"email"`
	CustomerID   string      `json:"customer_id"`   // Stripe customer ID
	SubscriptionID string    `json:"subscription_id"` // Stripe subscription ID
	Status       string      `json:"status"`        // active, cancelled, expired
	CreatedAt    time.Time   `json:"created_at"`
	ExpiresAt    *time.Time  `json:"expires_at,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// GenerateLicenseKey creates a unique license key with checksum
// Format: CGDB-XXXX-XXXX-XXXX-XXXX-CC (where CC is checksum)
func GenerateLicenseKey(licenseType LicenseType, email string) (string, error) {
	// Generate random bytes
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	// Create deterministic component from email and type
	h := sha256.New()
	h.Write([]byte(email))
	h.Write([]byte(licenseType))
	emailHash := h.Sum(nil)

	// Combine random and deterministic parts
	combined := append(randomBytes, emailHash[:8]...)

	// Encode to base64 and format
	// Use standard encoding and replace + and / with alphanumeric characters
	// to avoid interference with our hyphen-separated format
	encoded := base64.RawStdEncoding.EncodeToString(combined)
	encoded = strings.ReplaceAll(encoded, "+", "X")
	encoded = strings.ReplaceAll(encoded, "/", "Y")

	// Format as: CGDB-XXXX-XXXX-XXXX-XXXX
	prefix := "CGDB"
	parts := []string{prefix}

	for i := 0; i < len(encoded); i += 4 {
		end := i + 4
		if end > len(encoded) {
			end = len(encoded)
		}
		parts = append(parts, encoded[i:end])
		if len(parts) == 5 { // 4 parts after prefix
			break
		}
	}

	// Ensure we have enough parts (defensive check)
	for len(parts) < 5 {
		parts = append(parts, "XXXX")
	}

	// Generate key without checksum first
	keyWithoutChecksum := fmt.Sprintf("%s-%s-%s-%s-%s", parts[0], parts[1], parts[2], parts[3], parts[4])

	// Calculate checksum and append
	checksum := calculateLicenseChecksum(keyWithoutChecksum)

	return fmt.Sprintf("%s-%s", keyWithoutChecksum, checksum), nil
}

// calculateLicenseChecksum computes a 2-character checksum for license key validation
func calculateLicenseChecksum(keyWithoutChecksum string) string {
	h := sha256.Sum256([]byte(keyWithoutChecksum))
	// Use first 2 hex characters of hash as checksum
	return fmt.Sprintf("%02X", h[0])
}

// ValidateLicenseKey checks if a license key has valid format and checksum
// Supports both old format (CGDB-XXXX-XXXX-XXXX-XXXX) and new format with checksum
func ValidateLicenseKey(key string) bool {
	// Basic format check
	if len(key) < 24 {
		return false
	}
	if !strings.HasPrefix(key, "CGDB-") {
		return false
	}

	// Count parts to determine format
	parts := strings.Split(key, "-")

	// Old format: 5 parts (CGDB-XXXX-XXXX-XXXX-XXXX)
	if len(parts) == 5 {
		return true // Legacy keys without checksum are still valid
	}

	// New format: 6 parts (CGDB-XXXX-XXXX-XXXX-XXXX-CC)
	if len(parts) == 6 {
		// Extract key without checksum and provided checksum
		keyWithoutChecksum := strings.Join(parts[:5], "-")
		providedChecksum := parts[5]

		// Calculate expected checksum
		expectedChecksum := calculateLicenseChecksum(keyWithoutChecksum)

		// Compare (case-insensitive)
		return strings.EqualFold(providedChecksum, expectedChecksum)
	}

	return false
}

// GenerateLicenseID creates a unique license ID.
// Returns an error if random generation fails.
func GenerateLicenseID() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate license ID: %w", err)
	}
	return "lic_" + hex.EncodeToString(randomBytes), nil
}

// IsExpired checks if a license has expired
func (l *License) IsExpired() bool {
	if l.ExpiresAt == nil {
		return false // No expiration
	}
	return time.Now().After(*l.ExpiresAt)
}

// IsActive checks if a license is currently active
func (l *License) IsActive() bool {
	return l.Status == "active" && !l.IsExpired()
}

// ValidationOptions controls which validations to perform
type ValidationOptions struct {
	CheckExpiration      bool
	CheckStatus          bool
	CheckHardwareBinding bool
}

// DefaultValidationOptions returns the standard validation options
func DefaultValidationOptions() ValidationOptions {
	return ValidationOptions{
		CheckExpiration:      true,
		CheckStatus:          true,
		CheckHardwareBinding: true,
	}
}

// ValidationError represents a license validation failure
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Validate performs comprehensive license validation
// Returns nil if license is valid, error otherwise
func (l *License) Validate() error {
	return l.ValidateWithOptions(DefaultValidationOptions())
}

// ValidateWithOptions performs license validation with custom options
func (l *License) ValidateWithOptions(opts ValidationOptions) error {
	// Check license key format
	if !ValidateLicenseKey(l.Key) {
		return &ValidationError{
			Code:    "INVALID_KEY_FORMAT",
			Message: "license key has invalid format",
		}
	}

	// Check status
	if opts.CheckStatus && l.Status != "active" {
		return &ValidationError{
			Code:    "INACTIVE_LICENSE",
			Message: fmt.Sprintf("license status is '%s', expected 'active'", l.Status),
		}
	}

	// Check expiration
	if opts.CheckExpiration && l.IsExpired() {
		return &ValidationError{
			Code:    "EXPIRED_LICENSE",
			Message: "license has expired",
		}
	}

	// Check hardware binding (if enabled for this license)
	if opts.CheckHardwareBinding {
		if valid, err := l.VerifyHardwareBinding(); err != nil {
			return &ValidationError{
				Code:    "HARDWARE_BINDING_ERROR",
				Message: fmt.Sprintf("failed to verify hardware binding: %v", err),
			}
		} else if !valid {
			return &ValidationError{
				Code:    "HARDWARE_MISMATCH",
				Message: "license is bound to different hardware",
			}
		}
	}

	return nil
}
